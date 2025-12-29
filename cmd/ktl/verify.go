package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/ui"
	"github.com/example/ktl/internal/verify"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"sigs.k8s.io/yaml"
)

type verifyConfig struct {
	Version string            `yaml:"version,omitempty"`
	Target  verifyTarget      `yaml:"target"`
	Verify  verifyConfigRules `yaml:"verify,omitempty"`
	Output  verifyConfigOut   `yaml:"output,omitempty"`
	Kube    verifyConfigKube  `yaml:"kube,omitempty"`
}

type verifyTarget struct {
	Kind      string            `yaml:"kind"` // namespace|chart|manifest
	Namespace string            `yaml:"namespace,omitempty"`
	Manifest  string            `yaml:"manifest,omitempty"`
	Chart     verifyTargetChart `yaml:"chart,omitempty"`
}

type verifyTargetChart struct {
	Chart       string   `yaml:"chart,omitempty"`
	Release     string   `yaml:"release,omitempty"`
	Namespace   string   `yaml:"namespace,omitempty"`
	ValuesFiles []string `yaml:"values,omitempty"`
	SetValues   []string `yaml:"set,omitempty"`

	UseCluster  *bool `yaml:"useCluster,omitempty"`
	IncludeCRDs *bool `yaml:"includeCRDs,omitempty"`
}

type verifyConfigRules struct {
	Mode     string `yaml:"mode,omitempty"`   // warn|block|off
	FailOn   string `yaml:"failOn,omitempty"` // info|low|medium|high|critical
	RulesDir string `yaml:"rulesDir,omitempty"`

	Policy struct {
		Ref  string `yaml:"ref,omitempty"`
		Mode string `yaml:"mode,omitempty"` // warn|enforce
	} `yaml:"policy,omitempty"`

	Baseline struct {
		Read        string `yaml:"read,omitempty"`
		Write       string `yaml:"write,omitempty"`
		ExitOnDelta bool   `yaml:"exitOnDelta,omitempty"`
	} `yaml:"baseline,omitempty"`

	Exposure struct {
		Enabled bool   `yaml:"enabled,omitempty"`
		Output  string `yaml:"output,omitempty"`
	} `yaml:"exposure,omitempty"`

	FixPlan bool `yaml:"fixPlan,omitempty"`
}

type verifyConfigOut struct {
	Format string `yaml:"format,omitempty"` // table|json|sarif
	Report string `yaml:"report,omitempty"` // path or "-" (stdout)
}

type verifyConfigKube struct {
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
	Context    string `yaml:"context,omitempty"`
}

func newVerifyCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <config.yaml>",
		Short: "Verify Kubernetes configuration using a YAML config file",
		Long: strings.TrimSpace(`
Verify renders and/or collects Kubernetes objects and evaluates them against the built-in verify rules.

The config file is a small schema; set target.kind to choose what you are verifying:
  - namespace: read live objects from the cluster
  - chart: render a Helm chart (optionally with cluster lookups enabled)
  - manifest: verify an already-rendered manifest YAML file
`),
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			cfgPath := strings.TrimSpace(args[0])
			if cfgPath == "" {
				return fmt.Errorf("verify config path is required")
			}
			cfg, baseDir, err := loadVerifyConfig(cfgPath)
			if err != nil {
				return err
			}
			if err := cfg.validate(baseDir); err != nil {
				return err
			}

			if strings.TrimSpace(cfg.Kube.Kubeconfig) != "" && strings.TrimSpace(deref(kubeconfigPath)) != "" {
				return fmt.Errorf("kubeconfig is set in both the YAML config and --kubeconfig; pick one")
			}
			if strings.TrimSpace(cfg.Kube.Context) != "" && strings.TrimSpace(deref(kubeContext)) != "" {
				return fmt.Errorf("context is set in both the YAML config and --context; pick one")
			}

			effectiveKubeconfig := strings.TrimSpace(cfg.Kube.Kubeconfig)
			if effectiveKubeconfig == "" {
				effectiveKubeconfig = strings.TrimSpace(deref(kubeconfigPath))
			}
			effectiveContext := strings.TrimSpace(cfg.Kube.Context)
			if effectiveContext == "" {
				effectiveContext = strings.TrimSpace(deref(kubeContext))
			}

			targetLabel := cfg.targetLabel()
			modeValue := verify.Mode(strings.ToLower(strings.TrimSpace(cfg.Verify.Mode)))
			failOnValue := verify.Severity(strings.ToLower(strings.TrimSpace(cfg.Verify.FailOn)))

			errOut := cmd.ErrOrStderr()
			var console *verify.Console
			finishConsole := func() {
				if console == nil {
					return
				}
				console.Done()
				console = nil
			}
			if isTerminalWriter(errOut) {
				width, _ := ui.TerminalWidth(errOut)
				noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
				console = verify.NewConsole(errOut, verify.ConsoleMeta{
					Target:     targetLabel,
					Mode:       modeValue,
					FailOn:     failOnValue,
					PolicyRef:  strings.TrimSpace(cfg.Verify.Policy.Ref),
					PolicyMode: strings.TrimSpace(cfg.Verify.Policy.Mode),
				}, verify.ConsoleOptions{
					Enabled: true,
					Width:   width,
					Color:   !noColor,
					Now:     func() time.Time { return time.Now().UTC() },
				})
				phase := cfg.startPhase()
				if phase != "" {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Target: targetLabel, Phase: phase})
				}
			}

			var emit verify.Emitter
			if console != nil {
				emit = func(ev verify.Event) error {
					console.Observe(ev)
					return nil
				}
			}

			objs, renderedManifest, err := cfg.loadObjects(ctx, baseDir, effectiveKubeconfig, effectiveContext, logLevel, console)
			if err != nil {
				finishConsole()
				return err
			}

			rulesDir := strings.TrimSpace(cfg.Verify.RulesDir)
			if rulesDir == "" {
				rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
			}
			opts := verify.Options{
				Mode:     modeValue,
				FailOn:   failOnValue,
				Format:   verify.OutputFormat(strings.ToLower(strings.TrimSpace(cfg.Output.Format))),
				RulesDir: rulesDir,
			}
			if console != nil {
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "evaluate"})
			}
			rep, err := verify.VerifyObjectsWithEmitter(ctx, targetLabel, objs, opts, emit)
			if err != nil {
				finishConsole()
				return err
			}
			cfg.appendInputs(rep, renderedManifest)

			if strings.TrimSpace(cfg.Verify.Policy.Ref) != "" {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "policy", PolicyRef: strings.TrimSpace(cfg.Verify.Policy.Ref), PolicyMode: strings.TrimSpace(cfg.Verify.Policy.Mode)})
				}
				pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: cfg.Verify.Policy.Ref, Mode: cfg.Verify.Policy.Mode}, objs)
				if err != nil {
					finishConsole()
					return err
				}
				policyFindings := verify.PolicyReportToFindings(pol)
				rep.Findings = append(rep.Findings, policyFindings...)
				if console != nil {
					for i := range policyFindings {
						f := policyFindings[i]
						console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
					}
				}
				if strings.EqualFold(strings.TrimSpace(cfg.Verify.Policy.Mode), "enforce") && pol != nil && pol.DenyCount > 0 {
					rep.Blocked = true
					rep.Passed = false
				}
			}

			if strings.TrimSpace(cfg.Verify.Baseline.Read) != "" {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "baseline"})
				}
				base, err := verify.LoadReport(cfg.Verify.Baseline.Read)
				if err != nil {
					finishConsole()
					return err
				}
				delta := verify.ComputeDelta(rep, base)
				if cfg.Verify.Baseline.ExitOnDelta && len(delta.NewOrChanged) > 0 {
					rep.Blocked = true
					rep.Passed = false
				}
				rep.Findings = delta.NewOrChanged
				rep.Summary = verify.Summary{Total: len(rep.Findings), BySev: map[verify.Severity]int{}}
				for _, f := range rep.Findings {
					rep.Summary.BySev[f.Severity]++
				}
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventReset, When: time.Now().UTC()})
					for i := range rep.Findings {
						f := rep.Findings[i]
						console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
					}
					s := rep.Summary
					console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
				}
			}

			if cfg.Verify.Exposure.Enabled {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "exposure"})
				}
				ex := verify.AnalyzeExposure(objs)
				rep.Exposure = &ex
				if strings.TrimSpace(cfg.Verify.Exposure.Output) != "" {
					if w, c, err := openOutput(cmd.ErrOrStderr(), cfg.Verify.Exposure.Output); err == nil {
						_ = verify.WriteExposureJSON(w, &ex)
						if c != nil {
							_ = c.Close()
						}
					}
				}
			}

			if strings.TrimSpace(cfg.Verify.Baseline.Write) != "" {
				if w, c, err := openOutput(cmd.ErrOrStderr(), cfg.Verify.Baseline.Write); err == nil {
					_ = verify.WriteReport(w, rep, verify.OutputJSON)
					if c != nil {
						_ = c.Close()
					}
				}
			}

			if console != nil {
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "write"})
				s := rep.Summary
				console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
				console.Observe(verify.Event{Type: verify.EventDone, When: time.Now().UTC(), Passed: rep.Passed, Blocked: rep.Blocked})
			}
			finishConsole()

			out, closer, err := openOutput(cmd.OutOrStdout(), cfg.Output.Report)
			if err != nil {
				return err
			}
			defer func() {
				if closer != nil {
					_ = closer.Close()
				}
			}()
			if err := verify.WriteReport(out, rep, repModeFormat(cfg.Output.Format)); err != nil {
				return err
			}
			if cfg.Verify.FixPlan && (strings.TrimSpace(cfg.Output.Report) == "" || strings.TrimSpace(cfg.Output.Report) == "-") && repModeFormat(cfg.Output.Format) == verify.OutputTable {
				plan := verify.BuildFixPlan(rep.Findings)
				if text := verify.RenderFixPlanText(plan); text != "" {
					fmt.Fprint(cmd.OutOrStdout(), text)
				}
			}
			if rep.Blocked {
				return fmt.Errorf("verify blocked (fail-on=%s)", cfg.Verify.FailOn)
			}
			return nil
		},
	}

	decorateCommandHelp(cmd, "Verify Flags")
	cmd.Example = strings.TrimSpace(`
  # Run the bundled verify showcase (includes a CRITICAL rule)
  ktl verify testdata/verify/showcase/verify.yaml

  # Config reference (all options; remove what you don't use)
  cat > verify.yaml <<'YAML'
  version: v1

  target:
    kind: chart # namespace|chart|manifest
    # kind=namespace
    namespace: default
    # kind=manifest
    manifest: ./rendered.yaml
    # kind=chart
    chart:
      chart: ./chart
      release: foo
      namespace: default
      values:
        - values.yaml
      set:
        - image.tag=dev
      useCluster: true
      includeCRDs: false

  kube:
    kubeconfig: ~/.kube/config
    context: ""

  verify:
    mode: warn # warn|block|off
    failOn: high # info|low|medium|high|critical
    rulesDir: "" # default: built-in rules
    policy:
      ref: "" # local path or URL
      mode: warn # warn|enforce
    baseline:
      read: ""  # read report JSON
      write: "" # write report JSON
      exitOnDelta: false
    exposure:
      enabled: false
      output: "" # write exposure JSON
    fixPlan: false

  output:
    format: table # table|json|sarif
    report: "-"   # path or "-"
  YAML

  ktl verify verify.yaml

  # Verify a live namespace
  cat > verify-namespace.yaml <<'YAML'
  version: v1
  target:
    kind: namespace
    namespace: default
  verify:
    mode: warn
    failOn: high
  output:
    format: table
    report: "-"
  YAML

  ktl verify verify-namespace.yaml

  # Verify a chart render
  cat > verify-chart.yaml <<'YAML'
  version: v1
  target:
    kind: chart
    chart:
      chart: ./chart
      release: foo
      namespace: default
  verify:
    mode: block
    failOn: high
  output:
    format: table
    report: "-"
  YAML

  ktl verify verify-chart.yaml
`)
	return cmd
}

func loadVerifyConfig(path string) (*verifyConfig, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", fmt.Errorf("verify config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var cfg verifyConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, "", err
	}
	baseDir := filepath.Dir(path)
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, _ = filepath.Abs(baseDir)
	cfg.resolvePaths(baseDir)
	return &cfg, baseDir, nil
}

func (c *verifyConfig) resolvePaths(baseDir string) {
	if c == nil {
		return
	}
	c.Target.Manifest = resolveRelPath(baseDir, c.Target.Manifest)
	c.Verify.RulesDir = resolveRelPath(baseDir, c.Verify.RulesDir)
	c.Verify.Policy.Ref = resolveRelMaybeURL(baseDir, c.Verify.Policy.Ref)
	c.Verify.Baseline.Read = resolveRelPath(baseDir, c.Verify.Baseline.Read)
	c.Verify.Baseline.Write = resolveRelPath(baseDir, c.Verify.Baseline.Write)
	c.Verify.Exposure.Output = resolveRelPath(baseDir, c.Verify.Exposure.Output)
	c.Output.Report = resolveRelPath(baseDir, c.Output.Report)
	c.Kube.Kubeconfig = resolveRelPath(baseDir, c.Kube.Kubeconfig)
}

func (c *verifyConfig) validate(baseDir string) error {
	if c == nil {
		return fmt.Errorf("verify config is required")
	}
	kind := strings.ToLower(strings.TrimSpace(c.Target.Kind))
	switch kind {
	case "namespace":
		if strings.TrimSpace(c.Target.Namespace) == "" {
			return fmt.Errorf("target.namespace is required for kind=namespace")
		}
	case "manifest":
		if strings.TrimSpace(c.Target.Manifest) == "" {
			return fmt.Errorf("target.manifest is required for kind=manifest")
		}
	case "chart":
		if strings.TrimSpace(c.Target.Chart.Chart) == "" || strings.TrimSpace(c.Target.Chart.Release) == "" {
			return fmt.Errorf("target.chart.chart and target.chart.release are required for kind=chart")
		}
	default:
		return fmt.Errorf("target.kind must be one of: namespace, manifest, chart")
	}

	if strings.TrimSpace(c.Verify.Mode) == "" {
		c.Verify.Mode = "warn"
	}
	if strings.TrimSpace(c.Verify.FailOn) == "" {
		c.Verify.FailOn = "high"
	}
	if strings.TrimSpace(c.Output.Format) == "" {
		c.Output.Format = "table"
	}
	if strings.TrimSpace(c.Verify.Policy.Mode) == "" {
		c.Verify.Policy.Mode = "warn"
	}
	_ = baseDir
	return nil
}

func (c *verifyConfig) targetLabel() string {
	if c == nil {
		return "verify"
	}
	switch strings.ToLower(strings.TrimSpace(c.Target.Kind)) {
	case "namespace":
		return fmt.Sprintf("namespace %s", strings.TrimSpace(c.Target.Namespace))
	case "manifest":
		name := strings.TrimSpace(filepath.Base(strings.TrimSpace(c.Target.Manifest)))
		if name == "" {
			name = "manifest"
		}
		return fmt.Sprintf("manifest %s", name)
	case "chart":
		ns := strings.TrimSpace(c.Target.Chart.Namespace)
		return fmt.Sprintf("chart %s (release=%s ns=%s)", strings.TrimSpace(c.Target.Chart.Chart), strings.TrimSpace(c.Target.Chart.Release), ns)
	default:
		return "verify"
	}
}

func (c *verifyConfig) startPhase() string {
	switch strings.ToLower(strings.TrimSpace(c.Target.Kind)) {
	case "namespace":
		return "collect"
	case "manifest":
		return "decode"
	case "chart":
		return "render"
	default:
		return ""
	}
}

func (c *verifyConfig) loadObjects(ctx context.Context, baseDir string, kubeconfig string, kubeContext string, logLevel *string, console *verify.Console) ([]map[string]any, string, error) {
	switch strings.ToLower(strings.TrimSpace(c.Target.Kind)) {
	case "manifest":
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "decode"})
		}
		raw, err := os.ReadFile(strings.TrimSpace(c.Target.Manifest))
		if err != nil {
			return nil, "", err
		}
		objs, err := verify.DecodeK8SYAML(string(raw))
		return objs, "", err
	case "namespace":
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "collect"})
		}
		client, err := kube.New(ctx, strings.TrimSpace(kubeconfig), strings.TrimSpace(kubeContext))
		if err != nil {
			return nil, "", err
		}
		objs, err := collectNamespacedObjects(ctx, client, strings.TrimSpace(c.Target.Namespace))
		return objs, "", err
	case "chart":
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "render"})
		}
		settings := cli.New()
		settings.KubeConfig = strings.TrimSpace(kubeconfig)
		if settings.KubeConfig == "" {
			settings.KubeConfig = os.Getenv("KUBECONFIG")
		}
		if v := strings.TrimSpace(kubeContext); v != "" {
			settings.KubeContext = v
		}

		actionCfg := new(action.Configuration)
		if err := actionCfg.Init(settings.RESTClientGetter(), strings.TrimSpace(c.Target.Chart.Namespace), os.Getenv("HELM_DRIVER"), func(format string, args ...interface{}) {
			_ = logLevel
		}); err != nil {
			return nil, "", err
		}

		useCluster := true
		if c.Target.Chart.UseCluster != nil {
			useCluster = *c.Target.Chart.UseCluster
		}
		includeCRDs := true
		if c.Target.Chart.IncludeCRDs != nil {
			includeCRDs = *c.Target.Chart.IncludeCRDs
		}

		result, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
			Chart:       strings.TrimSpace(c.Target.Chart.Chart),
			ReleaseName: strings.TrimSpace(c.Target.Chart.Release),
			Namespace:   strings.TrimSpace(c.Target.Chart.Namespace),
			ValuesFiles: c.Target.Chart.ValuesFiles,
			SetValues:   c.Target.Chart.SetValues,
			UseCluster:  useCluster,
			IncludeCRDs: includeCRDs,
		})
		if err != nil {
			return nil, "", err
		}
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "decode"})
		}
		objs, err := verify.DecodeK8SYAML(result.Manifest)
		return objs, result.Manifest, err
	default:
		return nil, "", fmt.Errorf("unsupported target.kind %q", c.Target.Kind)
	}
}

func (c *verifyConfig) appendInputs(rep *verify.Report, renderedManifest string) {
	if c == nil || rep == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(c.Target.Kind)) {
	case "namespace":
		rep.Inputs = append(rep.Inputs, verify.Input{
			Kind:            "namespace",
			Namespace:       strings.TrimSpace(c.Target.Namespace),
			CollectedAtHint: "live",
		})
	case "manifest":
		rep.Inputs = append(rep.Inputs, verify.Input{
			Kind: "manifest",
		})
	case "chart":
		rep.Inputs = append(rep.Inputs, verify.Input{
			Kind:           "chart",
			Chart:          strings.TrimSpace(c.Target.Chart.Chart),
			Release:        strings.TrimSpace(c.Target.Chart.Release),
			Namespace:      strings.TrimSpace(c.Target.Chart.Namespace),
			RenderedSHA256: verify.ManifestDigestSHA256(renderedManifest),
		})
	}
}

func resolveRelPath(baseDir string, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "-" {
		return p
	}
	p = strings.TrimSpace(os.ExpandEnv(p))
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			switch p {
			case "~":
				p = home
			default:
				p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~/"), `~\`))
			}
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	if baseDir == "" {
		return p
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}

func resolveRelMaybeURL(baseDir string, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ref), "http://") || strings.HasPrefix(strings.ToLower(ref), "https://") {
		return ref
	}
	return resolveRelPath(baseDir, ref)
}

func repModeFormat(v string) verify.OutputFormat {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "table":
		return verify.OutputTable
	case "json":
		return verify.OutputJSON
	case "sarif":
		return verify.OutputSARIF
	default:
		return verify.OutputTable
	}
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func openOutput(defaultWriter io.Writer, path string) (io.Writer, io.Closer, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return defaultWriter, nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f, nil
}
