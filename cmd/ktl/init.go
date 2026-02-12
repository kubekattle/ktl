// File: cmd/ktl/init.go
// Brief: CLI command wiring and implementation for 'init'.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kubekattle/ktl/internal/appconfig"
	"github.com/mitchellh/go-homedir"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/tools/clientcmd"
)

type kubeContextInfo struct {
	Kubeconfig string
	Context    string
	Namespace  string
	Cluster    string
	Server     string
}

type initOptions struct {
	force           bool
	merge           bool
	dryRun          bool
	interactive     bool
	plan            bool
	planOutput      string
	applyPlan       string
	layout          bool
	values          bool
	stack           bool
	gitignore       bool
	gitignoreConfig bool
	showDiff        bool
	validate        bool
	output          string
	preset          string
	template        string
	secretsFile     string
	secretsProvider string
}

type scaffoldResult struct {
	Created []string
	Skipped []string
}

type gitignoreResult struct {
	Path    string
	Added   []string
	Skipped []string
}

type projectLayout struct {
	ChartPath       string
	ChartCandidates []string
	StackPath       string
	HelmfilePath    string
	KustomizePath   string
	DockerfilePath  string
	ValuesDir       string
	ValuesDevPath   string
	ValuesProdPath  string
}

func newInitCommand(kubeconfig *string, kubeContext *string, profile *string) *cobra.Command {
	opts := initOptions{
		validate:        true,
		output:          "text",
		secretsFile:     "./secrets.dev.yaml",
		secretsProvider: "local",
	}

	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a repo with ktl defaults",
		Long: strings.TrimSpace(`
Creates a repo-local .ktl.yaml with sane defaults and prints a short
getting-started checklist.`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.applyPlan) != "" {
				if opts.plan {
					return fmt.Errorf("--apply-plan cannot be combined with --plan")
				}
				return runInitApplyPlan(cmd, opts.applyPlan, opts.dryRun)
			}
			if opts.force && opts.merge {
				return fmt.Errorf("use either --force or --merge (not both)")
			}
			if opts.showDiff && !opts.merge {
				return fmt.Errorf("--show-diff requires --merge")
			}
			if opts.interactive && !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("--interactive requires a TTY")
			}
			opts.output = strings.ToLower(strings.TrimSpace(opts.output))
			if opts.output == "" {
				opts.output = "text"
			}
			if opts.output != "text" && opts.output != "json" {
				return fmt.Errorf("unsupported --output %q (expected text or json)", opts.output)
			}

			repoRoot, cfgPath, err := resolveInitPaths(args)
			if err != nil {
				return err
			}

			project := detectProjectLayout(repoRoot)
			existing, err := readExistingConfig(cfgPath)
			if err != nil {
				return err
			}
			if err := ensureInitTarget(cfgPath, opts.force, opts.merge, opts.dryRun, existing.Exists); err != nil {
				return err
			}

			profileValue := "dev"
			if profile != nil && strings.TrimSpace(*profile) != "" {
				profileValue = strings.TrimSpace(*profile)
			}

			if strings.TrimSpace(opts.preset) != "" {
				if err := applyPresetDefaults(cmd, &opts, project, &profileValue); err != nil {
					return err
				}
			}

			if opts.interactive {
				if err := runInitWizard(cmd, &opts, project, &profileValue); err != nil {
					return err
				}
			}

			if opts.layout && !flagChanged(cmd, "values") {
				opts.values = true
			}
			if opts.stack && !flagChanged(cmd, "layout") && project.ChartPath == "" {
				opts.layout = true
			}
			if opts.stack && opts.layout && !flagChanged(cmd, "values") {
				opts.values = true
			}

			planning := opts.plan
			if planning {
				opts.dryRun = true
			}

			sandboxConfig, sandboxNote := detectSandboxConfig(repoRoot)

			cfg, err := buildInitConfig(profileValue, opts.secretsProvider, opts.secretsFile, opts.preset, sandboxConfig)
			if err != nil {
				return err
			}

			if opts.merge && existing.Exists {
				if !isSecretsEmpty(existing.Config.Secrets) {
					cfg.Secrets = appconfig.SecretsConfig{}
				}
			}

			baseMap, err := configToMap(cfg)
			if err != nil {
				return err
			}

			templateMap, templateSource, err := loadInitTemplate(opts.template, repoRoot)
			if err != nil {
				return err
			}
			if templateMap != nil && opts.merge && existing.Exists && !isSecretsEmpty(existing.Config.Secrets) {
				delete(templateMap, "secrets")
			}
			if templateMap != nil {
				baseMap = mergeMapsPreferOverlay(baseMap, templateMap)
			}

			payload, finalMap, err := renderInitConfig(baseMap, existing.Map, opts.merge && existing.Exists)
			if err != nil {
				return err
			}
			if !opts.merge || !existing.Exists {
				header := []byte("# ktl repo config\n")
				payload = append(header, payload...)
			}
			if len(payload) > 0 && payload[len(payload)-1] != '\n' {
				payload = append(payload, '\n')
			}

			mode := "created"
			switch {
			case opts.dryRun && !planning:
				mode = "preview"
			case existing.Exists && opts.merge:
				mode = "merged"
			case existing.Exists && opts.force:
				mode = "overwritten"
			}

			diffText := ""
			if opts.showDiff && existing.Exists {
				diffText = renderUnifiedDiff(existing.Raw, string(payload), cfgPath)
			}

			if opts.dryRun {
				if planning || strings.EqualFold(opts.output, "json") {
					// Suppress YAML output; JSON payload or plan output will include it.
				} else {
					if _, err := cmd.OutOrStdout().Write(payload); err != nil {
						return err
					}
				}
			} else if err := os.WriteFile(cfgPath, payload, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", cfgPath, err)
			}

			strictContext := flagChanged(cmd, "kubeconfig") || flagChanged(cmd, "context")
			info, note, detectErr := detectKubeContext(kubeconfig, kubeContext, strictContext)
			if detectErr != nil {
				return detectErr
			}

			layout := scaffoldResult{}
			if opts.layout {
				layout, err = scaffoldLayout(repoRoot, opts.force, opts.dryRun)
				if err != nil {
					return err
				}
			}

			values := scaffoldResult{}
			if opts.values {
				values, err = scaffoldValues(repoRoot, opts.secretsProvider, opts.force, opts.dryRun)
				if err != nil {
					return err
				}
			}

			stack := scaffoldResult{}
			if opts.stack {
				stack, err = scaffoldStack(repoRoot, opts.force, opts.dryRun, project, opts.values)
				if err != nil {
					return err
				}
			}

			gitignore := gitignoreResult{}
			if opts.gitignore || opts.gitignoreConfig {
				var entries []string
				var skippedEntries []string
				if opts.gitignore {
					if entry, skipped := normalizeGitignoreEntry(repoRoot, opts.secretsFile); entry != "" {
						entries = append(entries, entry)
					} else if skipped != "" {
						skippedEntries = append(skippedEntries, skipped)
					}
				}
				if opts.gitignoreConfig {
					entries = append(entries, ".ktl.yaml")
				}
				if len(entries) > 0 {
					gitignore, err = updateGitignore(repoRoot, entries, opts.dryRun)
					if err != nil {
						return err
					}
				}
				if len(skippedEntries) > 0 {
					gitignore.Skipped = append(gitignore.Skipped, skippedEntries...)
				}
			}

			if !opts.dryRun {
				project = detectProjectLayout(repoRoot)
			}

			validation := []string{}
			if opts.validate {
				validation = validateConfigMap(finalMap)
			}

			namespace := "default"
			if info != nil && strings.TrimSpace(info.Namespace) != "" {
				namespace = strings.TrimSpace(info.Namespace)
			}
			nextSteps := buildNextSteps(project, opts.layout, namespace)
			notes := buildInitNotes(project, sandboxNote, templateSource)

			summary := initSummary{
				ConfigPath:    cfgPath,
				RepoRoot:      repoRoot,
				Mode:          mode,
				KubeInfo:      info,
				KubeNote:      note,
				SecretsPath:   opts.secretsFile,
				Layout:        layout,
				Values:        values,
				Stack:         stack,
				Gitignore:     gitignore,
				Project:       project,
				LayoutEnabled: opts.layout,
				Validation:    validation,
				Diff:          diffText,
				SandboxConfig: sandboxConfig,
				SandboxNote:   sandboxNote,
				NextSteps:     nextSteps,
				Notes:         notes,
			}

			if planning {
				plan := buildInitPlan(initPlanOptions{
					Mode:            mode,
					RepoRoot:        repoRoot,
					ConfigPath:      cfgPath,
					ConfigYAML:      string(payload),
					Diff:            diffText,
					Template:        opts.template,
					Preset:          opts.preset,
					KubeContext:     info,
					KubeNote:        note,
					Project:         project,
					Layout:          layout,
					Values:          values,
					Stack:           stack,
					Gitignore:       gitignore,
					NextSteps:       nextSteps,
					Notes:           notes,
					SandboxConfig:   sandboxConfig,
					SandboxNote:     sandboxNote,
					ValuesEnabled:   opts.values,
					SecretsProvider: opts.secretsProvider,
				})
				return writeInitPlan(cmd, plan, opts.planOutput)
			}

			if strings.EqualFold(opts.output, "json") {
				return writeInitJSON(cmd.OutOrStdout(), initOutput{
					Mode:          summary.Mode,
					ConfigPath:    summary.ConfigPath,
					RepoRoot:      summary.RepoRoot,
					ConfigYAML:    string(payload),
					Diff:          summary.Diff,
					KubeContext:   summary.KubeInfo,
					KubeNote:      summary.KubeNote,
					Layout:        summary.Layout,
					Values:        summary.Values,
					Stack:         summary.Stack,
					Gitignore:     summary.Gitignore,
					Project:       summary.Project,
					Validation:    summary.Validation,
					NextSteps:     summary.NextSteps,
					SandboxConfig: summary.SandboxConfig,
					SandboxNote:   summary.SandboxNote,
					Notes:         summary.Notes,
				})
			}

			summaryOut := cmd.OutOrStdout()
			if opts.dryRun {
				summaryOut = cmd.ErrOrStderr()
			}
			printInitSummary(summaryOut, summary)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.force, "force", false, "Overwrite an existing .ktl.yaml")
	cmd.Flags().BoolVar(&opts.merge, "merge", false, "Merge defaults into an existing .ktl.yaml")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Print the .ktl.yaml that would be written")
	cmd.Flags().BoolVar(&opts.interactive, "interactive", false, "Run an interactive setup wizard")
	cmd.Flags().BoolVar(&opts.plan, "plan", false, "Output a replayable init plan as JSON (no changes applied)")
	cmd.Flags().StringVar(&opts.planOutput, "plan-output", "", "Write plan JSON to a file instead of stdout")
	cmd.Flags().StringVar(&opts.applyPlan, "apply-plan", "", "Apply a previously generated init plan (path or - for stdin)")
	cmd.Flags().BoolVar(&opts.layout, "layout", false, "Create chart/ and values/ directories")
	cmd.Flags().BoolVar(&opts.values, "values", false, "Create values/dev.yaml and values/prod.yaml")
	cmd.Flags().BoolVar(&opts.stack, "stack", false, "Create a minimal stack.yaml in the repo root")
	cmd.Flags().BoolVar(&opts.gitignore, "gitignore", false, "Add the secrets file to .gitignore")
	cmd.Flags().BoolVar(&opts.gitignoreConfig, "gitignore-config", false, "Also add .ktl.yaml to .gitignore")
	cmd.Flags().BoolVar(&opts.showDiff, "show-diff", false, "Show a diff when merging into an existing .ktl.yaml")
	cmd.Flags().BoolVar(&opts.validate, "validate", opts.validate, "Validate the generated config for common mistakes")
	cmd.Flags().StringVar(&opts.output, "output", opts.output, "Output format: text or json")
	cmd.Flags().StringVar(&opts.preset, "preset", "", "Opinionated init preset: dev, ci, prod")
	cmd.Flags().StringVar(&opts.template, "template", "", "Apply a config template (name, path, or URL). Built-ins: platform, secure")
	cmd.Flags().StringVar(&opts.secretsFile, "secrets-file", opts.secretsFile, "Local secrets file path to reference in .ktl.yaml")
	cmd.Flags().StringVar(&opts.secretsProvider, "secrets-provider", opts.secretsProvider, "Secrets provider to scaffold (local, vault, aws, k8s)")
	decorateCommandHelp(cmd, "Onboarding")
	return cmd
}

type initSummary struct {
	ConfigPath    string
	RepoRoot      string
	Mode          string
	KubeInfo      *kubeContextInfo
	KubeNote      string
	SecretsPath   string
	Layout        scaffoldResult
	Values        scaffoldResult
	Stack         scaffoldResult
	Gitignore     gitignoreResult
	Project       projectLayout
	LayoutEnabled bool
	Validation    []string
	Diff          string
	SandboxConfig string
	SandboxNote   string
	NextSteps     []string
	Notes         []string
}

type initOutput struct {
	Mode          string           `json:"mode"`
	ConfigPath    string           `json:"configPath"`
	RepoRoot      string           `json:"repoRoot"`
	ConfigYAML    string           `json:"configYaml,omitempty"`
	Diff          string           `json:"diff,omitempty"`
	KubeContext   *kubeContextInfo `json:"kubeContext,omitempty"`
	KubeNote      string           `json:"kubeNote,omitempty"`
	Layout        scaffoldResult   `json:"layout,omitempty"`
	Values        scaffoldResult   `json:"values,omitempty"`
	Stack         scaffoldResult   `json:"stack,omitempty"`
	Gitignore     gitignoreResult  `json:"gitignore,omitempty"`
	Project       projectLayout    `json:"project,omitempty"`
	Validation    []string         `json:"validation,omitempty"`
	NextSteps     []string         `json:"nextSteps,omitempty"`
	SandboxConfig string           `json:"sandboxConfig,omitempty"`
	SandboxNote   string           `json:"sandboxNote,omitempty"`
	Notes         []string         `json:"notes,omitempty"`
}

type existingConfig struct {
	Config appconfig.Config
	Map    map[string]any
	Raw    string
	Exists bool
}

type initTemplate struct {
	Name    string
	Content string
	Source  string
}

func resolveInitPaths(args []string) (string, string, error) {
	target := "."
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		target = strings.TrimSpace(args[0])
	}
	info, err := os.Stat(target)
	if err == nil && !info.IsDir() {
		target = filepath.Dir(target)
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", "", fmt.Errorf("resolve path %q: %w", target, err)
	}
	repoRoot := appconfig.FindRepoRoot(absTarget)
	if repoRoot == "" {
		repoRoot = absTarget
	}
	cfgPath := filepath.Join(repoRoot, ".ktl.yaml")
	return repoRoot, cfgPath, nil
}

func ensureInitTarget(cfgPath string, force bool, merge bool, dryRun bool, exists bool) error {
	if strings.TrimSpace(cfgPath) == "" {
		return errors.New("unable to resolve .ktl.yaml path")
	}
	if exists && !force && !merge && !dryRun {
		return fmt.Errorf("%s already exists (use --merge or --force)", cfgPath)
	}
	return nil
}

func applyPresetDefaults(cmd *cobra.Command, opts *initOptions, project projectLayout, profileValue *string) error {
	if opts == nil {
		return nil
	}
	preset := strings.ToLower(strings.TrimSpace(opts.preset))
	if preset == "" {
		return nil
	}
	switch preset {
	case "dev", "ci", "prod":
	default:
		return fmt.Errorf("unsupported --preset %q (expected dev, ci, prod)", preset)
	}

	setBool := func(flag string, dest *bool, value bool) {
		if dest == nil || flagChanged(cmd, flag) {
			return
		}
		*dest = value
	}
	setString := func(flag string, dest *string, value string) {
		if dest == nil || flagChanged(cmd, flag) {
			return
		}
		*dest = value
	}
	setProfile := func(value string) {
		if profileValue == nil || rootFlagChanged(cmd, "profile") {
			return
		}
		*profileValue = value
	}

	switch preset {
	case "dev":
		setProfile("dev")
		setString("secrets-provider", &opts.secretsProvider, "local")
		setBool("layout", &opts.layout, project.ChartPath == "" && project.StackPath == "")
		setBool("values", &opts.values, true)
		setBool("gitignore", &opts.gitignore, true)
		setBool("gitignore-config", &opts.gitignoreConfig, false)
	case "ci":
		setProfile("ci")
		setString("secrets-provider", &opts.secretsProvider, "local")
		setBool("layout", &opts.layout, false)
		setBool("values", &opts.values, false)
		setBool("gitignore", &opts.gitignore, false)
		setBool("gitignore-config", &opts.gitignoreConfig, false)
	case "prod":
		setProfile("secure")
		setString("secrets-provider", &opts.secretsProvider, "vault")
		setBool("layout", &opts.layout, project.ChartPath == "")
		setBool("values", &opts.values, true)
		setBool("gitignore", &opts.gitignore, false)
		setBool("gitignore-config", &opts.gitignoreConfig, false)
	}
	return nil
}

func runInitWizard(cmd *cobra.Command, opts *initOptions, project projectLayout, profileValue *string) error {
	if cmd == nil || opts == nil {
		return nil
	}
	reader := bufio.NewReader(os.Stdin)
	out := cmd.OutOrStdout()

	pProfile := "dev"
	if profileValue != nil && strings.TrimSpace(*profileValue) != "" {
		pProfile = strings.TrimSpace(*profileValue)
	}
	profileChoice, err := promptChoice(reader, out, "Profile (dev|ci|secure|remote)", []string{"dev", "ci", "secure", "remote"}, pProfile)
	if err != nil {
		return err
	}
	if profileValue != nil {
		*profileValue = profileChoice
	}

	providerChoice, err := promptChoice(reader, out, "Secrets provider (local|vault|aws|k8s)", []string{"local", "vault", "aws", "k8s"}, opts.secretsProvider)
	if err != nil {
		return err
	}
	opts.secretsProvider = providerChoice

	secretsPrompt := "Secrets file path (for local provider)"
	secretsPath, err := promptString(reader, out, secretsPrompt, opts.secretsFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(secretsPath) != "" {
		opts.secretsFile = secretsPath
	}

	defaultLayout := opts.layout
	if !flagChanged(cmd, "layout") && project.ChartPath == "" && project.StackPath == "" {
		defaultLayout = true
	}
	layoutChoice, err := promptBool(reader, out, "Create chart/ and values/ directories", defaultLayout)
	if err != nil {
		return err
	}
	opts.layout = layoutChoice

	defaultValues := opts.values
	if !flagChanged(cmd, "values") && layoutChoice {
		defaultValues = true
	}
	valuesChoice, err := promptBool(reader, out, "Create values/dev.yaml and values/prod.yaml", defaultValues)
	if err != nil {
		return err
	}
	opts.values = valuesChoice

	defaultStack := opts.stack
	if !flagChanged(cmd, "stack") && project.StackPath == "" {
		defaultStack = false
	}
	stackChoice, err := promptBool(reader, out, "Create a minimal stack.yaml", defaultStack)
	if err != nil {
		return err
	}
	opts.stack = stackChoice

	defaultGitignore := opts.gitignore
	if !flagChanged(cmd, "gitignore") && strings.EqualFold(opts.secretsProvider, "local") {
		defaultGitignore = true
	}
	gitignoreChoice, err := promptBool(reader, out, "Add secrets file to .gitignore", defaultGitignore)
	if err != nil {
		return err
	}
	opts.gitignore = gitignoreChoice

	gitignoreCfgChoice, err := promptBool(reader, out, "Add .ktl.yaml to .gitignore", opts.gitignoreConfig)
	if err != nil {
		return err
	}
	opts.gitignoreConfig = gitignoreCfgChoice

	return nil
}

func promptString(reader *bufio.Reader, w io.Writer, label string, def string) (string, error) {
	if reader == nil {
		return "", errors.New("missing reader")
	}
	if strings.TrimSpace(def) != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

func promptChoice(reader *bufio.Reader, w io.Writer, label string, choices []string, def string) (string, error) {
	normalized := make(map[string]string, len(choices))
	for _, choice := range choices {
		c := strings.ToLower(strings.TrimSpace(choice))
		if c != "" {
			normalized[c] = c
		}
	}
	def = strings.ToLower(strings.TrimSpace(def))
	for attempts := 0; attempts < 3; attempts++ {
		resp, err := promptString(reader, w, label, def)
		if err != nil {
			return "", err
		}
		val := strings.ToLower(strings.TrimSpace(resp))
		if val == "" && def != "" {
			val = def
		}
		if _, ok := normalized[val]; ok {
			return val, nil
		}
		if len(choices) > 0 {
			fmt.Fprintf(w, "Please choose one of: %s\n", strings.Join(choices, ", "))
		}
	}
	return "", errors.New("invalid selection")
}

func promptBool(reader *bufio.Reader, w io.Writer, label string, def bool) (bool, error) {
	defStr := "no"
	if def {
		defStr = "yes"
	}
	for attempts := 0; attempts < 3; attempts++ {
		resp, err := promptString(reader, w, label+" (yes/no)", defStr)
		if err != nil {
			return false, err
		}
		val := strings.ToLower(strings.TrimSpace(resp))
		switch val {
		case "y", "yes", "true", "1":
			return true, nil
		case "n", "no", "false", "0":
			return false, nil
		case "":
			return def, nil
		}
		fmt.Fprintln(w, "Please answer yes or no.")
	}
	return def, nil
}

func buildInitConfig(profileValue string, secretsProvider string, secretsPath string, preset string, sandboxConfig string) (appconfig.Config, error) {
	profileValue = strings.TrimSpace(profileValue)
	if profileValue == "" {
		profileValue = "dev"
	}
	secretsProvider = strings.ToLower(strings.TrimSpace(secretsProvider))
	if secretsProvider == "" {
		secretsProvider = "local"
	}
	if secretsPath == "" {
		secretsPath = "./secrets.dev.yaml"
	}

	cfg := appconfig.Config{
		Build: appconfig.BuildConfig{
			Profile: profileValue,
		},
	}
	if profileValue == "ci" {
		cfg.Build.Push = boolPtr(true)
	}
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "prod":
		cfg.Build.Hermetic = boolPtr(true)
		cfg.Build.Sandbox = boolPtr(true)
		cfg.Build.AttestDir = "dist/attest"
		cfg.Build.PolicyMode = "enforce"
		cfg.Build.SecretsMode = "enforce"
	case "ci":
		if cfg.Build.Push == nil {
			cfg.Build.Push = boolPtr(true)
		}
	}
	if strings.TrimSpace(sandboxConfig) != "" && strings.TrimSpace(cfg.Build.SandboxConfig) == "" {
		cfg.Build.SandboxConfig = sandboxConfig
	}

	switch secretsProvider {
	case "local":
		cfg.Secrets = appconfig.SecretsConfig{
			DefaultProvider: "local",
			Providers: map[string]appconfig.SecretProvider{
				"local": {
					Type: "file",
					Path: secretsPath,
				},
			},
		}
	case "vault":
		cfg.Secrets = appconfig.SecretsConfig{
			DefaultProvider: "vault",
			Providers: map[string]appconfig.SecretProvider{
				"vault": {
					Type:       "vault",
					Address:    "https://vault.example.com",
					AuthMethod: "token",
					Mount:      "secret",
					KVVersion:  2,
				},
			},
		}
	case "aws":
		cfg.Secrets = appconfig.SecretsConfig{
			DefaultProvider: "vault",
			Providers: map[string]appconfig.SecretProvider{
				"vault": {
					Type:           "vault",
					Address:        "https://vault.example.com",
					AuthMethod:     "aws",
					AuthMount:      "aws",
					AWSRole:        "ktl",
					AWSRegion:      "us-east-1",
					Mount:          "secret",
					KVVersion:      2,
					AWSHeaderValue: "vault.example.com",
				},
			},
		}
	case "k8s", "kubernetes":
		cfg.Secrets = appconfig.SecretsConfig{
			DefaultProvider: "vault",
			Providers: map[string]appconfig.SecretProvider{
				"vault": {
					Type:                "vault",
					Address:             "https://vault.example.com",
					AuthMethod:          "kubernetes",
					AuthMount:           "kubernetes",
					KubernetesRole:      "ktl",
					KubernetesTokenPath: "/var/run/secrets/kubernetes.io/serviceaccount/token",
					Mount:               "secret",
					KVVersion:           2,
				},
			},
		}
	default:
		return appconfig.Config{}, fmt.Errorf("unsupported --secrets-provider %q (expected local, vault, aws, k8s)", secretsProvider)
	}

	return cfg, nil
}

func boolPtr(val bool) *bool {
	return &val
}

func detectKubeContext(kubeconfig *string, kubeContext *string, strict bool) (*kubeContextInfo, string, error) {
	kubeconfigPath := ""
	if kubeconfig != nil {
		kubeconfigPath = strings.TrimSpace(*kubeconfig)
	}
	contextName := ""
	if kubeContext != nil {
		contextName = strings.TrimSpace(*kubeContext)
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		expanded, err := homedir.Expand(kubeconfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("expand kubeconfig path: %w", err)
		}
		rules.Precedence = []string{filepath.Clean(expanded)}
	}

	cfg, err := rules.Load()
	if err != nil {
		if strict {
			return nil, "", fmt.Errorf("load kubeconfig: %w", err)
		}
		path := describeKubeconfigPath(rules, kubeconfigPath)
		if path != "" {
			return nil, fmt.Sprintf("No kubecontext detected in %s. Run `kubectl config get-contexts` or pass --kubeconfig/--context.", path), nil
		}
		return nil, "No kubecontext detected. Run `kubectl config get-contexts` or pass --kubeconfig/--context.", nil
	}
	if len(cfg.Contexts) == 0 {
		if strict {
			return nil, "", errors.New("kubeconfig has no contexts")
		}
		return nil, "No kubecontext detected. Run `kubectl config get-contexts` or pass --kubeconfig/--context.", nil
	}
	if contextName == "" {
		contextName = strings.TrimSpace(cfg.CurrentContext)
	}
	if contextName == "" {
		if strict {
			return nil, "", errors.New("kubeconfig has no current context; set --context")
		}
		return nil, "No kubecontext selected. Run `kubectl config use-context <name>` or pass --context.", nil
	}
	ctx := cfg.Contexts[contextName]
	if ctx == nil {
		if strict {
			return nil, "", fmt.Errorf("kubeconfig does not contain context %q", contextName)
		}
		return nil, fmt.Sprintf("Kubecontext %q not found. Run `kubectl config get-contexts` or pass --context.", contextName), nil
	}

	namespace := strings.TrimSpace(ctx.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	clusterName := strings.TrimSpace(ctx.Cluster)
	server := ""
	if clusterName != "" {
		if cluster := cfg.Clusters[clusterName]; cluster != nil {
			server = strings.TrimSpace(cluster.Server)
		}
	}

	return &kubeContextInfo{
		Kubeconfig: describeKubeconfigPath(rules, kubeconfigPath),
		Context:    contextName,
		Namespace:  namespace,
		Cluster:    clusterName,
		Server:     server,
	}, "", nil
}

func describeKubeconfigPath(rules *clientcmd.ClientConfigLoadingRules, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		if expanded, err := homedir.Expand(explicit); err == nil {
			return filepath.Clean(expanded)
		}
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("KUBECONFIG")); env != "" {
		parts := strings.Split(env, string(os.PathListSeparator))
		for i, part := range parts {
			if expanded, err := homedir.Expand(strings.TrimSpace(part)); err == nil {
				parts[i] = filepath.Clean(expanded)
			}
		}
		return strings.Join(parts, string(os.PathListSeparator))
	}
	if rules != nil {
		if def := strings.TrimSpace(rules.GetDefaultFilename()); def != "" {
			if expanded, err := homedir.Expand(def); err == nil {
				return filepath.Clean(expanded)
			}
			return def
		}
	}
	return ""
}

func readExistingConfig(cfgPath string) (existingConfig, error) {
	if strings.TrimSpace(cfgPath) == "" {
		return existingConfig{}, errors.New("missing config path")
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return existingConfig{}, nil
		}
		return existingConfig{}, fmt.Errorf("read %s: %w", cfgPath, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return existingConfig{
			Config: appconfig.Config{},
			Map:    map[string]any{},
			Raw:    "",
			Exists: true,
		}, nil
	}
	var cfg appconfig.Config
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return existingConfig{}, fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	var mapped map[string]any
	if err := yaml.Unmarshal([]byte(trimmed), &mapped); err != nil {
		return existingConfig{}, fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	if mapped == nil {
		mapped = map[string]any{}
	}
	return existingConfig{
		Config: cfg,
		Map:    mapped,
		Raw:    trimmed,
		Exists: true,
	}, nil
}

func renderInitConfig(base map[string]any, existing map[string]any, merge bool) ([]byte, map[string]any, error) {
	if base == nil {
		base = map[string]any{}
	}
	if !merge {
		out, err := yaml.Marshal(base)
		if err != nil {
			return nil, nil, fmt.Errorf("render config yaml: %w", err)
		}
		return out, base, nil
	}
	merged := mergeMapsPreferExisting(base, existing)
	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, nil, fmt.Errorf("render config yaml: %w", err)
	}
	return out, merged, nil
}

func configToMap(cfg appconfig.Config) (map[string]any, error) {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var mapped map[string]any
	if err := yaml.Unmarshal(raw, &mapped); err != nil {
		return nil, err
	}
	if mapped == nil {
		mapped = map[string]any{}
	}
	return mapped, nil
}

func mergeMapsPreferExisting(base map[string]any, existing map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range existing {
		if cur, ok := out[k]; ok {
			curMap, curOK := cur.(map[string]any)
			nextMap, nextOK := v.(map[string]any)
			if curOK && nextOK {
				out[k] = mergeMapsPreferExisting(curMap, nextMap)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func mergeMapsPreferOverlay(base map[string]any, overlay map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		if cur, ok := out[k]; ok {
			curMap, curOK := cur.(map[string]any)
			nextMap, nextOK := v.(map[string]any)
			if curOK && nextOK {
				out[k] = mergeMapsPreferOverlay(curMap, nextMap)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func isSecretsEmpty(cfg appconfig.SecretsConfig) bool {
	return strings.TrimSpace(cfg.DefaultProvider) == "" && len(cfg.Providers) == 0
}

func scaffoldLayout(repoRoot string, force bool, dryRun bool) (scaffoldResult, error) {
	res := scaffoldResult{}
	if strings.TrimSpace(repoRoot) == "" {
		return res, errors.New("missing repo root")
	}
	chartDir := filepath.Join(repoRoot, "chart")
	valuesDir := filepath.Join(repoRoot, "values")

	ensureDir := func(path string) error {
		if fi, err := os.Stat(path); err == nil && fi.IsDir() {
			res.Skipped = append(res.Skipped, relPath(repoRoot, path))
			return nil
		} else if err == nil {
			return fmt.Errorf("%s exists and is not a directory", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		res.Created = append(res.Created, relPath(repoRoot, path))
		if dryRun {
			return nil
		}
		return os.MkdirAll(path, 0o755)
	}

	if err := ensureDir(chartDir); err != nil {
		return res, err
	}
	if err := ensureDir(valuesDir); err != nil {
		return res, err
	}
	return res, nil
}

func scaffoldValues(repoRoot string, secretsProvider string, force bool, dryRun bool) (scaffoldResult, error) {
	res := scaffoldResult{}
	if strings.TrimSpace(repoRoot) == "" {
		return res, errors.New("missing repo root")
	}
	valuesDir := filepath.Join(repoRoot, "values")
	dirCreated := false
	if fi, err := os.Stat(valuesDir); err == nil && !fi.IsDir() {
		return res, fmt.Errorf("%s exists and is not a directory", valuesDir)
	} else if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return res, err
		}
		dirCreated = true
		if !dryRun {
			if err := os.MkdirAll(valuesDir, 0o755); err != nil {
				return res, err
			}
		}
	}
	if dirCreated {
		res.Created = append(res.Created, relPath(repoRoot, valuesDir))
	}

	files := []struct {
		name string
		env  string
	}{
		{name: filepath.Join(valuesDir, "dev.yaml"), env: "dev"},
		{name: filepath.Join(valuesDir, "prod.yaml"), env: "prod"},
	}

	for _, f := range files {
		if fi, err := os.Stat(f.name); err == nil {
			if fi.IsDir() {
				return res, fmt.Errorf("%s exists and is a directory", f.name)
			}
			if !force {
				res.Skipped = append(res.Skipped, relPath(repoRoot, f.name))
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return res, err
		}
		res.Created = append(res.Created, relPath(repoRoot, f.name))
		if dryRun {
			continue
		}
		content := buildValuesTemplate(secretsProvider, f.env)
		if err := os.WriteFile(f.name, []byte(content), 0o644); err != nil {
			return res, fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return res, nil
}

func scaffoldStack(repoRoot string, force bool, dryRun bool, project projectLayout, valuesEnabled bool) (scaffoldResult, error) {
	res := scaffoldResult{}
	if strings.TrimSpace(repoRoot) == "" {
		return res, errors.New("missing repo root")
	}
	stackPath := filepath.Join(repoRoot, "stack.yaml")
	if fi, err := os.Stat(stackPath); err == nil {
		if fi.IsDir() {
			return res, fmt.Errorf("%s exists and is a directory", stackPath)
		}
		if !force {
			res.Skipped = append(res.Skipped, relPath(repoRoot, stackPath))
			return res, nil
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, err
	}

	content := renderStackTemplate(repoRoot, project, valuesEnabled)

	res.Created = append(res.Created, relPath(repoRoot, stackPath))
	if dryRun {
		return res, nil
	}
	if err := os.WriteFile(stackPath, []byte(content), 0o644); err != nil {
		return res, fmt.Errorf("write %s: %w", stackPath, err)
	}
	return res, nil
}

func renderStackTemplate(repoRoot string, project projectLayout, valuesEnabled bool) string {
	chartPath := "./chart"
	if project.ChartPath != "" {
		chartPath = project.ChartPath
	}
	releaseName := filepath.Base(repoRoot)
	if releaseName == "." || releaseName == "/" || releaseName == "" {
		releaseName = "app"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "name: %s\n\n", releaseName)
	b.WriteString("cli:\n")
	b.WriteString("  output: table\n")
	b.WriteString("  inferDeps: true\n\n")
	b.WriteString("releases:\n")
	fmt.Fprintf(&b, "  - name: %s\n", releaseName)
	fmt.Fprintf(&b, "    chart: %s\n", chartPath)
	if valuesEnabled {
		b.WriteString("    values:\n")
		b.WriteString("      - ./values/dev.yaml\n")
	}
	return b.String()
}

func buildValuesTemplate(secretsProvider string, env string) string {
	secretsProvider = strings.ToLower(strings.TrimSpace(secretsProvider))
	if secretsProvider == "" {
		secretsProvider = "local"
	}
	refProvider := secretsProvider
	if refProvider == "aws" || refProvider == "k8s" || refProvider == "kubernetes" {
		refProvider = "vault"
	}
	if strings.TrimSpace(env) == "" {
		env = "dev"
	}
	return fmt.Sprintf(`# values for %s

# Example secret reference:
# db:
#   password: secret://%s/app/db#password
`, env, refProvider)
}

func normalizeGitignoreEntry(repoRoot string, secretsPath string) (string, string) {
	path := strings.TrimSpace(secretsPath)
	if path == "" {
		return "", ""
	}
	if strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", path
		}
		path = rel
	}
	return path, ""
}

func gitignoreKey(entry string) string {
	entry = strings.TrimSpace(entry)
	if strings.HasPrefix(entry, "./") {
		entry = strings.TrimPrefix(entry, "./")
	}
	return entry
}

func updateGitignore(repoRoot string, entries []string, dryRun bool) (gitignoreResult, error) {
	res := gitignoreResult{}
	if strings.TrimSpace(repoRoot) == "" {
		return res, errors.New("missing repo root")
	}
	path := filepath.Join(repoRoot, ".gitignore")
	res.Path = path

	existing := map[string]struct{}{}
	var raw []byte
	if data, err := os.ReadFile(path); err == nil {
		raw = data
		for _, line := range strings.Split(string(data), "\n") {
			key := gitignoreKey(line)
			if key == "" {
				continue
			}
			existing[key] = struct{}{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("read %s: %w", path, err)
	}

	for _, entry := range entries {
		entry = gitignoreKey(entry)
		if entry == "" {
			continue
		}
		if _, ok := existing[entry]; ok {
			res.Skipped = append(res.Skipped, entry)
			continue
		}
		res.Added = append(res.Added, entry)
	}

	if len(res.Added) == 0 || dryRun {
		return res, nil
	}

	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		return res, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return res, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return res, err
		}
	}
	if _, ok := existing["# ktl"]; !ok {
		if _, err := f.WriteString("# ktl\n"); err != nil {
			return res, err
		}
	}
	for _, entry := range res.Added {
		if _, err := f.WriteString(entry + "\n"); err != nil {
			return res, err
		}
	}
	return res, nil
}

func detectProjectLayout(repoRoot string) projectLayout {
	out := projectLayout{}
	if repoRoot == "" {
		return out
	}

	chartPath, candidates := findChartPath(repoRoot)
	out.ChartPath = chartPath
	out.ChartCandidates = candidates

	if fileExists(filepath.Join(repoRoot, "stack.yaml")) || fileExists(filepath.Join(repoRoot, "stack.yml")) {
		out.StackPath = "."
	}
	if fileExists(filepath.Join(repoRoot, "helmfile.yaml")) {
		out.HelmfilePath = "./helmfile.yaml"
	} else if fileExists(filepath.Join(repoRoot, "helmfile.yml")) {
		out.HelmfilePath = "./helmfile.yml"
	}
	if fileExists(filepath.Join(repoRoot, "kustomization.yaml")) {
		out.KustomizePath = "./kustomization.yaml"
	} else if fileExists(filepath.Join(repoRoot, "kustomization.yml")) {
		out.KustomizePath = "./kustomization.yml"
	}
	if fileExists(filepath.Join(repoRoot, "Dockerfile")) {
		out.DockerfilePath = "./Dockerfile"
	}

	valuesDir, valuesDev, valuesProd := detectValuesPaths(repoRoot, out.ChartPath)
	out.ValuesDir = valuesDir
	out.ValuesDevPath = valuesDev
	out.ValuesProdPath = valuesProd

	return out
}

func detectSandboxConfig(repoRoot string) (string, string) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", ""
	}
	primary := []string{
		filepath.Join(repoRoot, "sandbox", "linux-ci.cfg"),
		filepath.Join(repoRoot, "sandbox", "linux-strict.cfg"),
	}
	for _, candidate := range primary {
		if fileExists(candidate) {
			return relPath(repoRoot, candidate), ""
		}
	}
	sandboxDir := filepath.Join(repoRoot, "sandbox")
	entries, err := os.ReadDir(sandboxDir)
	if err == nil {
		var cfgs []string
		for _, entry := range entries {
			if entry == nil || entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasSuffix(name, ".cfg") {
				cfgs = append(cfgs, filepath.Join(sandboxDir, name))
			}
		}
		sort.Strings(cfgs)
		if len(cfgs) > 0 {
			return relPath(repoRoot, cfgs[0]), ""
		}
	}
	note := "No sandbox policy found. Add sandbox/linux-ci.cfg or sandbox/linux-strict.cfg to enable sandbox defaults."
	return "", note
}

func validateConfigMap(cfg map[string]any) []string {
	if cfg == nil {
		return nil
	}
	var warns []string
	allowedTop := map[string]struct{}{
		"build":   {},
		"secrets": {},
	}
	for key, val := range cfg {
		switch key {
		case "build":
			buildMap, ok := val.(map[string]any)
			if !ok {
				warns = append(warns, "build should be a map/object")
				continue
			}
			warns = append(warns, validateBuildMap(buildMap)...)
		case "secrets":
			secMap, ok := val.(map[string]any)
			if !ok {
				warns = append(warns, "secrets should be a map/object")
				continue
			}
			warns = append(warns, validateSecretsMap(secMap)...)
		default:
			if _, ok := allowedTop[key]; !ok {
				warns = append(warns, fmt.Sprintf("unknown top-level key %q", key))
			}
		}
	}
	sort.Strings(warns)
	return uniqueStrings(warns)
}

func validateBuildMap(build map[string]any) []string {
	var warns []string
	allowed := map[string]string{
		"profile":       "string",
		"cacheDir":      "string",
		"attestDir":     "string",
		"policy":        "string",
		"policyMode":    "string",
		"secretsMode":   "string",
		"secretsConfig": "string",
		"hermetic":      "bool",
		"sandbox":       "bool",
		"sandboxConfig": "string",
		"push":          "bool",
		"load":          "bool",
		"remoteBuild":   "string",
	}
	for key, val := range build {
		expected, ok := allowed[key]
		if !ok {
			warns = append(warns, fmt.Sprintf("unknown build key %q", key))
			continue
		}
		if !valueMatchesType(val, expected) {
			warns = append(warns, fmt.Sprintf("build.%s should be %s", key, expected))
		}
		if key == "policyMode" || key == "secretsMode" {
			if s, ok := val.(string); ok && s != "" && s != "warn" && s != "enforce" {
				warns = append(warns, fmt.Sprintf("build.%s should be warn or enforce", key))
			}
		}
	}
	return warns
}

func validateSecretsMap(secrets map[string]any) []string {
	var warns []string
	allowed := map[string]string{
		"defaultProvider": "string",
		"providers":       "map",
	}
	for key, val := range secrets {
		expected, ok := allowed[key]
		if !ok {
			warns = append(warns, fmt.Sprintf("unknown secrets key %q", key))
			continue
		}
		if !valueMatchesType(val, expected) {
			warns = append(warns, fmt.Sprintf("secrets.%s should be %s", key, expected))
		}
	}

	providersRaw, ok := secrets["providers"]
	if !ok {
		return warns
	}
	providers, ok := providersRaw.(map[string]any)
	if !ok {
		return warns
	}
	for name, val := range providers {
		pmap, ok := val.(map[string]any)
		if !ok {
			warns = append(warns, fmt.Sprintf("secrets.providers.%s should be a map/object", name))
			continue
		}
		warns = append(warns, validateProviderMap(name, pmap)...)
	}
	return warns
}

func validateProviderMap(name string, provider map[string]any) []string {
	var warns []string
	allowed := map[string]string{
		"type":                "string",
		"path":                "string",
		"address":             "string",
		"token":               "string",
		"namespace":           "string",
		"mount":               "string",
		"kvVersion":           "number",
		"key":                 "string",
		"authMethod":          "string",
		"authMount":           "string",
		"roleId":              "string",
		"secretId":            "string",
		"kubernetesRole":      "string",
		"kubernetesToken":     "string",
		"kubernetesTokenPath": "string",
		"awsRole":             "string",
		"awsRegion":           "string",
		"awsHeaderValue":      "string",
	}
	for key, val := range provider {
		expected, ok := allowed[key]
		if !ok {
			warns = append(warns, fmt.Sprintf("unknown secrets.providers.%s key %q", name, key))
			continue
		}
		if !valueMatchesType(val, expected) {
			warns = append(warns, fmt.Sprintf("secrets.providers.%s.%s should be %s", name, key, expected))
		}
		if key == "type" {
			if s, ok := val.(string); ok && s != "" {
				switch s {
				case "file", "vault":
				default:
					warns = append(warns, fmt.Sprintf("secrets.providers.%s.type should be file or vault", name))
				}
			}
		}
	}
	return warns
}

func valueMatchesType(val any, expected string) bool {
	switch expected {
	case "string":
		_, ok := val.(string)
		return ok
	case "bool":
		_, ok := val.(bool)
		return ok
	case "number":
		switch val.(type) {
		case int, int64, int32, float64, float32, uint, uint64, uint32:
			return true
		default:
			return false
		}
	case "map":
		_, ok := val.(map[string]any)
		return ok
	default:
		return true
	}
}

func buildNextSteps(project projectLayout, layoutEnabled bool, namespace string) []string {
	if strings.TrimSpace(namespace) == "" {
		namespace = "default"
	}
	var steps []string
	valuesArg := ""
	if project.ValuesDevPath != "" {
		valuesArg = " -f " + project.ValuesDevPath
	} else if project.ValuesProdPath != "" {
		valuesArg = " -f " + project.ValuesProdPath
	}
	if project.ChartPath != "" {
		steps = append(steps, fmt.Sprintf("ktl apply plan --chart %s --release demo -n %s%s", project.ChartPath, namespace, valuesArg))
		steps = append(steps, fmt.Sprintf("ktl apply --chart %s --release demo -n %s%s --ui", project.ChartPath, namespace, valuesArg))
		if project.ValuesDevPath != "" && project.ValuesProdPath != "" && project.ValuesDevPath != project.ValuesProdPath {
			steps = append(steps, fmt.Sprintf("ktl apply plan --chart %s --release demo -n %s -f %s", project.ChartPath, namespace, project.ValuesProdPath))
		}
	} else if layoutEnabled {
		steps = append(steps, "Place your Helm chart under ./chart (or run `helm create chart`)")
	}

	if project.StackPath != "" {
		steps = append(steps, fmt.Sprintf("ktl stack plan --config %s", project.StackPath))
	}
	if len(steps) == 0 {
		steps = append(steps, fmt.Sprintf("ktl apply plan --chart ./chart --release demo -n %s", namespace))
	}
	steps = append(steps, "ktl help --ui")
	return steps
}

func buildInitNotes(project projectLayout, sandboxNote string, templateSource string) []string {
	var notes []string
	if templateSource != "" {
		notes = append(notes, fmt.Sprintf("Applied init template: %s.", templateSource))
	}
	if len(project.ChartCandidates) > 1 {
		notes = append(notes, fmt.Sprintf("Multiple charts detected (%s). Using %s.", strings.Join(project.ChartCandidates, ", "), project.ChartPath))
	}
	if project.ValuesDevPath != "" || project.ValuesProdPath != "" {
		var values []string
		if project.ValuesDevPath != "" {
			values = append(values, project.ValuesDevPath)
		}
		if project.ValuesProdPath != "" {
			values = append(values, project.ValuesProdPath)
		}
		notes = append(notes, fmt.Sprintf("Detected values files: %s.", strings.Join(values, ", ")))
	} else if project.ValuesDir != "" {
		notes = append(notes, fmt.Sprintf("values/ found at %s, but dev/prod files are missing.", project.ValuesDir))
	}
	if project.HelmfilePath != "" {
		notes = append(notes, fmt.Sprintf("Helmfile detected at %s (ktl init does not import Helmfile automatically).", project.HelmfilePath))
	}
	if project.KustomizePath != "" {
		notes = append(notes, fmt.Sprintf("Kustomize detected at %s. ktl focuses on Helm workflows.", project.KustomizePath))
	}
	if project.DockerfilePath != "" {
		notes = append(notes, fmt.Sprintf("Dockerfile detected at %s. Try ktl build --context . --tag <image>.", project.DockerfilePath))
	}
	if sandboxNote != "" {
		notes = append(notes, sandboxNote)
	}
	return notes
}

func renderUnifiedDiff(before string, after string, path string) string {
	before = strings.TrimRight(before, "\n")
	after = strings.TrimRight(after, "\n")
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(before + "\n"),
		B:        difflib.SplitLines(after + "\n"),
		FromFile: path + " (before)",
		ToFile:   path + " (after)",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return ""
	}
	return text
}

func writeInitJSON(w io.Writer, out initOutput) error {
	if w == nil {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func findChartPath(repoRoot string) (string, []string) {
	if repoRoot == "" {
		return "", nil
	}
	candidates := []string{}
	primary := []string{
		filepath.Join(repoRoot, "Chart.yaml"),
		filepath.Join(repoRoot, "chart", "Chart.yaml"),
		filepath.Join(repoRoot, "charts", "Chart.yaml"),
	}
	for _, candidate := range primary {
		if fileExists(candidate) {
			chartDir := filepath.Dir(candidate)
			rel := relPath(repoRoot, chartDir)
			if rel == "./" {
				rel = "."
			}
			return rel, []string{rel}
		}
	}

	_ = filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipInitDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), "Chart.yaml") {
			dir := filepath.Dir(path)
			rel := relPath(repoRoot, dir)
			if rel == "./" {
				rel = "."
			}
			candidates = append(candidates, rel)
		}
		return nil
	})
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return chartScore(candidates[i]) < chartScore(candidates[j]) || (chartScore(candidates[i]) == chartScore(candidates[j]) && candidates[i] < candidates[j])
	})
	unique := uniqueStrings(candidates)
	return unique[0], unique
}

func chartScore(path string) int {
	if path == "." || path == "./" {
		return 0
	}
	score := strings.Count(path, string(os.PathSeparator)) * 10
	base := filepath.Base(path)
	switch base {
	case "chart", "charts":
		score -= 5
	}
	if strings.Contains(path, "examples") {
		score += 5
	}
	return score
}

func shouldSkipInitDir(name string) bool {
	switch name {
	case ".git", ".github", ".idea", ".vscode", "node_modules", "vendor", "dist", "bin", "testdata", "tmp", ".ktl":
		return true
	default:
		return false
	}
}

func detectValuesPaths(repoRoot string, chartPath string) (string, string, string) {
	if repoRoot == "" {
		return "", "", ""
	}
	rootValues := filepath.Join(repoRoot, "values")
	valuesDir := ""
	if chartPath != "" {
		chartAbs := filepath.Join(repoRoot, chartPath)
		parent := filepath.Dir(chartAbs)
		if chartPath == "." || chartPath == "./" {
			parent = repoRoot
		}
		parentValues := filepath.Join(parent, "values")
		if dirExists(parentValues) {
			valuesDir = parentValues
		}
	}
	if valuesDir == "" && dirExists(rootValues) {
		valuesDir = rootValues
	}
	if valuesDir == "" {
		return "", "", ""
	}
	dev := filepath.Join(valuesDir, "dev.yaml")
	prod := filepath.Join(valuesDir, "prod.yaml")
	devRel := ""
	prodRel := ""
	if fileExists(dev) {
		devRel = relPath(repoRoot, dev)
	}
	if fileExists(prod) {
		prodRel = relPath(repoRoot, prod)
	}
	return relPath(repoRoot, valuesDir), devRel, prodRel
}

func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func relPath(base string, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	if rel == "." {
		return "./"
	}
	if !strings.HasPrefix(rel, "./") {
		return "./" + rel
	}
	return rel
}

func printInitSummary(w io.Writer, summary initSummary) {
	if w == nil {
		return
	}
	switch summary.Mode {
	case "preview":
		fmt.Fprintf(w, "Dry run: would write %s\n", summary.ConfigPath)
	case "merged":
		fmt.Fprintf(w, "Merged ktl config at %s\n", summary.ConfigPath)
	case "overwritten":
		fmt.Fprintf(w, "Overwrote ktl config at %s\n", summary.ConfigPath)
	default:
		fmt.Fprintf(w, "Initialized ktl config at %s\n", summary.ConfigPath)
	}
	if summary.RepoRoot != "" {
		fmt.Fprintf(w, "Repo root: %s\n", summary.RepoRoot)
	}

	if len(summary.Layout.Created) > 0 || len(summary.Layout.Skipped) > 0 {
		fmt.Fprintln(w, "\nScaffolded layout:")
		for _, item := range summary.Layout.Created {
			fmt.Fprintf(w, "- %s (created)\n", item)
		}
		for _, item := range summary.Layout.Skipped {
			fmt.Fprintf(w, "- %s (already existed)\n", item)
		}
	}

	if len(summary.Values.Created) > 0 || len(summary.Values.Skipped) > 0 {
		fmt.Fprintln(w, "\nScaffolded values:")
		for _, item := range summary.Values.Created {
			fmt.Fprintf(w, "- %s (created)\n", item)
		}
		for _, item := range summary.Values.Skipped {
			fmt.Fprintf(w, "- %s (already existed)\n", item)
		}
	}

	if len(summary.Stack.Created) > 0 || len(summary.Stack.Skipped) > 0 {
		fmt.Fprintln(w, "\nScaffolded stack:")
		for _, item := range summary.Stack.Created {
			fmt.Fprintf(w, "- %s (created)\n", item)
		}
		for _, item := range summary.Stack.Skipped {
			fmt.Fprintf(w, "- %s (already existed)\n", item)
		}
	}

	if len(summary.Gitignore.Added) > 0 || len(summary.Gitignore.Skipped) > 0 {
		fmt.Fprintln(w, "\nUpdated .gitignore:")
		if summary.Gitignore.Path != "" {
			fmt.Fprintf(w, "- Path: %s\n", summary.Gitignore.Path)
		}
		for _, entry := range summary.Gitignore.Added {
			fmt.Fprintf(w, "- Added: %s\n", entry)
		}
		for _, entry := range summary.Gitignore.Skipped {
			fmt.Fprintf(w, "- Skipped: %s\n", entry)
		}
	}

	if summary.SandboxConfig != "" {
		fmt.Fprintln(w, "\nSandbox defaults:")
		fmt.Fprintf(w, "- sandboxConfig: %s\n", summary.SandboxConfig)
	}

	if summary.KubeInfo != nil {
		fmt.Fprintln(w, "\nDetected Kubernetes context:")
		if summary.KubeInfo.Kubeconfig != "" {
			fmt.Fprintf(w, "- Kubeconfig: %s\n", summary.KubeInfo.Kubeconfig)
		}
		fmt.Fprintf(w, "- Context: %s\n", summary.KubeInfo.Context)
		fmt.Fprintf(w, "- Namespace: %s\n", summary.KubeInfo.Namespace)
		if summary.KubeInfo.Server != "" {
			fmt.Fprintf(w, "- Cluster: %s (%s)\n", summary.KubeInfo.Cluster, summary.KubeInfo.Server)
		} else if summary.KubeInfo.Cluster != "" {
			fmt.Fprintf(w, "- Cluster: %s\n", summary.KubeInfo.Cluster)
		}
	} else if summary.KubeNote != "" {
		fmt.Fprintln(w, "\nKubecontext:")
		fmt.Fprintf(w, "%s\n", summary.KubeNote)
	}

	secretsPath := strings.TrimSpace(summary.SecretsPath)
	if secretsPath == "" {
		secretsPath = "./secrets.dev.yaml"
	}
	if !strings.HasPrefix(secretsPath, "./") && !strings.HasPrefix(secretsPath, "/") {
		secretsPath = "./" + secretsPath
	}

	fmt.Fprintln(w, "\nSuggested layout:")
	fmt.Fprintln(w, "- chart/ Helm chart root")
	fmt.Fprintln(w, "- values/ Values per environment")
	fmt.Fprintf(w, "- %s Local secrets (add to .gitignore)\n", secretsPath)

	if len(summary.Validation) > 0 {
		fmt.Fprintln(w, "\nConfig validation warnings:")
		for _, warn := range summary.Validation {
			fmt.Fprintf(w, "- %s\n", warn)
		}
	}

	if summary.Diff != "" {
		fmt.Fprintln(w, "\nConfig diff:")
		fmt.Fprint(w, summary.Diff)
		if !strings.HasSuffix(summary.Diff, "\n") {
			fmt.Fprintln(w)
		}
	}

	if len(summary.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, note := range summary.Notes {
			fmt.Fprintf(w, "- %s\n", note)
		}
	}

	if len(summary.NextSteps) > 0 {
		fmt.Fprintln(w, "\nNext steps:")
		for _, step := range summary.NextSteps {
			fmt.Fprintf(w, "- %s\n", step)
		}
	}
}
