package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/secretstore"
	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"sigs.k8s.io/yaml"
)

type secretDiscoverRef struct {
	Provider  string   `json:"provider"`
	Path      string   `json:"path"`
	Key       string   `json:"key,omitempty"`
	Reference string   `json:"reference"`
	Owners    []string `json:"owners,omitempty"`
	Count     int      `json:"count"`
}

type secretDiscoverProvider struct {
	Name string `json:"name"`
	Used bool   `json:"used"`
}

type secretDiscoverOrphan struct {
	Reference string   `json:"reference"`
	Message   string   `json:"message"`
	Owners    []string `json:"owners,omitempty"`
	Count     int      `json:"count"`
}

type secretDiscoverOutput struct {
	Scope     string                   `json:"scope"`
	Refs      []secretDiscoverRef      `json:"refs"`
	Providers []secretDiscoverProvider `json:"providers,omitempty"`
	Orphans   []secretDiscoverOrphan   `json:"orphans,omitempty"`
}

type secretRefKey struct {
	Provider string
	Path     string
	Key      string
}

type secretRefAggregate struct {
	Reference string
	Owners    map[string]struct{}
}

type secretOrphanAggregate struct {
	Reference string
	Message   string
	Owners    map[string]struct{}
}

type secretDiscoverAgg struct {
	defaultProvider string
	providers       map[string]struct{}
	refs            map[secretRefKey]*secretRefAggregate
	orphans         map[string]*secretOrphanAggregate
}

func newSecretDiscoverAgg(defaultProvider string, providerNames []string) *secretDiscoverAgg {
	providers := map[string]struct{}{}
	for _, name := range providerNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		providers[name] = struct{}{}
	}
	return &secretDiscoverAgg{
		defaultProvider: strings.TrimSpace(defaultProvider),
		providers:       providers,
		refs:            map[secretRefKey]*secretRefAggregate{},
		orphans:         map[string]*secretOrphanAggregate{},
	}
}

func (a *secretDiscoverAgg) addRawRef(raw string, owner string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	ref, ok, err := secretstore.ParseRef(raw, a.defaultProvider)
	if !ok {
		return
	}
	if err != nil {
		a.addOrphan(raw, err.Error(), owner)
		return
	}
	provider := strings.TrimSpace(ref.Provider)
	path, key := splitRefKey(ref.Path)
	if provider == "" {
		a.addOrphan(raw, "secret reference is missing provider", owner)
		return
	}
	if len(a.providers) > 0 {
		if _, ok := a.providers[provider]; !ok {
			a.addOrphan(raw, fmt.Sprintf("secret provider %q is not configured", provider), owner)
			return
		}
	}
	reference := buildRefString(provider, path, key)
	keyStruct := secretRefKey{Provider: provider, Path: path, Key: key}
	agg := a.refs[keyStruct]
	if agg == nil {
		agg = &secretRefAggregate{Reference: reference, Owners: map[string]struct{}{}}
		a.refs[keyStruct] = agg
	}
	if owner != "" {
		agg.Owners[owner] = struct{}{}
	}
}

func (a *secretDiscoverAgg) addOrphan(reference string, message string, owner string) {
	reference = strings.TrimSpace(reference)
	message = strings.TrimSpace(message)
	if reference == "" {
		reference = "secret://"
	}
	key := reference + "|" + message
	agg := a.orphans[key]
	if agg == nil {
		agg = &secretOrphanAggregate{
			Reference: reference,
			Message:   message,
			Owners:    map[string]struct{}{},
		}
		a.orphans[key] = agg
	}
	if owner != "" {
		agg.Owners[owner] = struct{}{}
	}
}

func (a *secretDiscoverAgg) refsList() []secretDiscoverRef {
	out := make([]secretDiscoverRef, 0, len(a.refs))
	for key, agg := range a.refs {
		owners := sortedKeysFromSet(agg.Owners)
		out = append(out, secretDiscoverRef{
			Provider:  key.Provider,
			Path:      key.Path,
			Key:       key.Key,
			Reference: agg.Reference,
			Owners:    owners,
			Count:     len(owners),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func (a *secretDiscoverAgg) orphanList() []secretDiscoverOrphan {
	out := make([]secretDiscoverOrphan, 0, len(a.orphans))
	for _, agg := range a.orphans {
		owners := sortedKeysFromSet(agg.Owners)
		out = append(out, secretDiscoverOrphan{
			Reference: agg.Reference,
			Message:   agg.Message,
			Owners:    owners,
			Count:     len(owners),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reference != out[j].Reference {
			return out[i].Reference < out[j].Reference
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func newSecretsDiscoverCommand() *cobra.Command {
	var scope string
	var root string
	var configPath string
	var chartRef string
	var chartVersion string
	var chartRepo string
	var valuesFiles []string
	var setValues []string
	var setStringValues []string
	var setFileValues []string
	var output string
	var includeUnused bool
	var includeOrphaned bool
	var secretProvider string
	var secretConfig string

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan charts and values for secret:// references",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			scope = strings.ToLower(strings.TrimSpace(scope))
			if scope == "" {
				scope = "repo"
			}
			switch scope {
			case "repo", "chart", "stack":
			default:
				return fmt.Errorf("unsupported --scope %q (expected repo, chart, or stack)", scope)
			}
			if scope == "chart" && strings.TrimSpace(chartRef) == "" {
				return fmt.Errorf("--chart is required for scope=chart")
			}
			if scope == "stack" && strings.TrimSpace(chartRef) != "" {
				return fmt.Errorf("--chart is only valid for scope=chart")
			}
			if scope != "stack" && strings.TrimSpace(configPath) != "" {
				return fmt.Errorf("--config is only valid for scope=stack")
			}

			defaultProvider, providerNames, configErr := loadDiscoverSecretConfig(ctx, scope, root, configPath, chartRef, secretConfig, secretProvider)
			if configErr != nil {
				return configErr
			}
			agg := newSecretDiscoverAgg(defaultProvider, providerNames)

			switch scope {
			case "repo":
				repoRoot, err := resolveDiscoverRepoRoot(root)
				if err != nil {
					return err
				}
				if err := discoverRepoSecrets(ctx, cmd.ErrOrStderr(), agg, repoRoot); err != nil {
					return err
				}
			case "chart":
				if err := discoverChartSecrets(ctx, cmd.ErrOrStderr(), agg, chartRef, chartVersion, chartRepo, valuesFiles, setValues, setStringValues, setFileValues); err != nil {
					return err
				}
			case "stack":
				stackRoot, err := resolveStackRoot(configPath, root)
				if err != nil {
					return err
				}
				if err := discoverStackSecrets(ctx, cmd.ErrOrStderr(), agg, stackRoot); err != nil {
					return err
				}
			}

			refs := agg.refsList()
			orphans := agg.orphanList()
			providers := buildProviderUsage(providerNames, refs)

			out := secretDiscoverOutput{
				Scope: scope,
				Refs:  refs,
			}
			if includeUnused {
				out.Providers = providers
			}
			if includeOrphaned {
				out.Orphans = orphans
			}

			format := strings.ToLower(strings.TrimSpace(output))
			if format == "" {
				format = "table"
			}
			switch format {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			case "table":
				return renderSecretDiscoverTable(cmd.OutOrStdout(), out, includeUnused, includeOrphaned)
			default:
				return fmt.Errorf("unsupported --output %q (expected table or json)", output)
			}
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "repo", "Discovery scope: repo, chart, or stack")
	cmd.Flags().StringVar(&root, "root", ".", "Repo root (scope=repo) or stack root when --config is unset")
	cmd.Flags().StringVar(&configPath, "config", "", "Stack root directory or stack.yaml (scope=stack)")
	cmd.Flags().StringVar(&chartRef, "chart", "", "Chart path or reference (scope=chart)")
	cmd.Flags().StringVar(&chartVersion, "chart-version", "", "Chart version (scope=chart)")
	cmd.Flags().StringVar(&chartRepo, "chart-repo", "", "Chart repository URL (scope=chart)")
	cmd.Flags().StringSliceVar(&valuesFiles, "values", nil, "Values file(s) to include (scope=chart)")
	cmd.Flags().StringSliceVar(&setValues, "set", nil, "Set values on the command line (scope=chart)")
	cmd.Flags().StringSliceVar(&setStringValues, "set-string", nil, "Set string values on the command line (scope=chart)")
	cmd.Flags().StringSliceVar(&setFileValues, "set-file", nil, "Set values from files on the command line (scope=chart)")
	cmd.Flags().StringVar(&secretProvider, "secret-provider", "", "Default secret provider name for secret:// references")
	cmd.Flags().StringVar(&secretConfig, "secret-config", "", "Secrets provider config file (defaults to ~/.ktl/config.yaml and repo .ktl.yaml)")
	cmd.Flags().StringVar(&output, "output", "table", "Output format: table or json")
	cmd.Flags().BoolVar(&includeUnused, "include-unused", false, "Include configured but unused secret providers")
	cmd.Flags().BoolVar(&includeOrphaned, "include-orphaned", false, "Include invalid or unconfigured secret references")
	return cmd
}

func loadDiscoverSecretConfig(ctx context.Context, scope string, root string, configPath string, chartRef string, secretConfig string, secretProvider string) (string, []string, error) {
	if strings.TrimSpace(secretProvider) != "" {
		secretProvider = strings.TrimSpace(secretProvider)
	}
	var base string
	switch scope {
	case "chart":
		base = strings.TrimSpace(chartRef)
	case "stack":
		base = strings.TrimSpace(configPath)
		if base == "" {
			base = strings.TrimSpace(root)
		}
	default:
		base = strings.TrimSpace(root)
	}
	if base == "" {
		base = "."
	}
	cfg, _, err := secretstore.LoadConfigFromApp(ctx, base, secretConfig)
	if err != nil {
		return "", nil, err
	}
	defaultProvider := secretProvider
	if defaultProvider == "" {
		defaultProvider = cfg.DefaultProvider
	}
	providers := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		providers = append(providers, name)
	}
	sort.Strings(providers)
	return defaultProvider, providers, nil
}

func resolveDiscoverRepoRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	repoRoot := appconfig.FindRepoRoot(absRoot)
	if repoRoot == "" {
		repoRoot = absRoot
	}
	return repoRoot, nil
}

func resolveStackRoot(configPath string, root string) (string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = strings.TrimSpace(root)
	}
	if configPath == "" {
		configPath = "."
	}
	info, err := os.Stat(configPath)
	if err == nil && !info.IsDir() {
		return filepath.Dir(configPath), nil
	}
	return configPath, nil
}

func discoverRepoSecrets(ctx context.Context, errOut io.Writer, agg *secretDiscoverAgg, repoRoot string) error {
	chartDirs, err := findChartDirs(repoRoot, 6)
	if err != nil {
		return err
	}
	for _, chartDir := range chartDirs {
		rel := relPath(repoRoot, chartDir)
		if err := discoverChartFromPath(ctx, errOut, agg, chartDir, rel); err != nil {
			return err
		}
	}
	return nil
}

func discoverChartSecrets(ctx context.Context, errOut io.Writer, agg *secretDiscoverAgg, chartRef string, chartVersion string, chartRepo string, valuesFiles []string, setValues []string, setStringValues []string, setFileValues []string) error {
	chartRef = strings.TrimSpace(chartRef)
	if chartRef == "" {
		return fmt.Errorf("chart reference is required")
	}
	chartRequested, chartPath, err := loadChart(ctx, chartRef, chartVersion, chartRepo)
	if err != nil {
		return err
	}
	ownerPrefix := chartPath
	if ownerPrefix == "" {
		ownerPrefix = chartRef
	}
	valuesOwner := ownerPrefix
	if ownerPrefix != "" {
		valuesOwner = filepath.Join(ownerPrefix, "values.yaml")
	}
	scanChartValues(agg, chartRequested, valuesOwner, ownerPrefix)
	scanChartTemplates(agg, chartRequested, ownerPrefix)

	for _, vf := range valuesFiles {
		if err := scanValuesFile(agg, vf, vf); err != nil {
			if errOut != nil {
				fmt.Fprintf(errOut, "warning: %v\n", err)
			}
		}
	}
	scanSetValues(agg, setValues, "--set")
	scanSetValues(agg, setStringValues, "--set-string")
	scanSetFileValues(agg, setFileValues, "--set-file")
	return nil
}

func discoverStackSecrets(ctx context.Context, errOut io.Writer, agg *secretDiscoverAgg, stackRoot string) error {
	u, err := stack.Discover(stackRoot)
	if err != nil {
		return err
	}
	p, err := stack.Compile(u, stack.CompileOptions{Profile: u.DefaultProfile})
	if err != nil {
		return err
	}
	cache := map[string]*chart.Chart{}
	for _, node := range p.Nodes {
		owner := node.ID
		for _, vf := range node.Values {
			if err := scanValuesFile(agg, vf, owner); err != nil {
				if errOut != nil {
					fmt.Fprintf(errOut, "warning: %v\n", err)
				}
			}
		}
		scanSetValuesFromMap(agg, node.Set, owner)

		chartKey := node.Chart + "@" + node.ChartVersion
		chartRequested := cache[chartKey]
		if chartRequested == nil {
			chartRequested, _, err = loadChart(ctx, node.Chart, node.ChartVersion, "")
			if err != nil {
				if errOut != nil {
					fmt.Fprintf(errOut, "warning: %v\n", err)
				}
				continue
			}
			cache[chartKey] = chartRequested
		}
		scanChartValues(agg, chartRequested, owner, owner)
		scanChartTemplates(agg, chartRequested, owner)
	}
	return nil
}

func discoverChartFromPath(ctx context.Context, errOut io.Writer, agg *secretDiscoverAgg, chartDir string, ownerPrefix string) error {
	chartRequested, err := loader.Load(chartDir)
	if err != nil {
		return err
	}
	if err := scanValuesFile(agg, filepath.Join(chartDir, "values.yaml"), filepath.Join(ownerPrefix, "values.yaml")); err != nil && errOut != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(errOut, "warning: %v\n", err)
		}
	}
	valuesDir := filepath.Join(chartDir, "values")
	if entries, err := os.ReadDir(valuesDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}
			fullPath := filepath.Join(valuesDir, name)
			owner := filepath.Join(ownerPrefix, "values", name)
			if err := scanValuesFile(agg, fullPath, owner); err != nil && errOut != nil {
				fmt.Fprintf(errOut, "warning: %v\n", err)
			}
		}
	}
	valuesOwner := filepath.Join(ownerPrefix, "values.yaml")
	scanChartValues(agg, chartRequested, valuesOwner, valuesOwner)
	scanChartTemplates(agg, chartRequested, ownerPrefix)
	return nil
}

func loadChart(ctx context.Context, chartRef string, version string, repoURL string) (*chart.Chart, string, error) {
	_ = ctx
	settings := cli.New()
	chartOpts := action.ChartPathOptions{RepoURL: strings.TrimSpace(repoURL), Version: strings.TrimSpace(version)}
	chartPath, err := chartOpts.LocateChart(chartRef, settings)
	if err != nil {
		return nil, "", fmt.Errorf("locate chart %q: %w", chartRef, err)
	}
	chartRequested, err := loader.Load(chartPath)
	if err != nil {
		return nil, "", fmt.Errorf("load chart %q: %w", chartRef, err)
	}
	return chartRequested, chartPath, nil
}

func scanValuesFile(agg *secretDiscoverAgg, path string, owner string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var data map[string]interface{}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse values file %s: %w", path, err)
	}
	for _, ref := range secretstore.FindRefs(data) {
		agg.addRawRef(ref, owner)
	}
	return nil
}

func scanChartValues(agg *secretDiscoverAgg, chartRequested *chart.Chart, owner string, fallbackOwner string) {
	if chartRequested == nil {
		return
	}
	if owner == "" {
		owner = fallbackOwner
	}
	for _, ref := range secretstore.FindRefs(chartRequested.Values) {
		agg.addRawRef(ref, owner)
	}
	for _, dep := range chartRequested.Dependencies() {
		scanChartValues(agg, dep, owner, fallbackOwner)
	}
}

func scanChartTemplates(agg *secretDiscoverAgg, chartRequested *chart.Chart, ownerPrefix string) {
	if chartRequested == nil {
		return
	}
	templateSources := map[string]string{}
	collectTemplates(chartRequested, "", templateSources)
	for name, contents := range templateSources {
		owner := ownerPrefix
		if owner != "" {
			owner = filepath.Join(ownerPrefix, name)
		}
		for _, ref := range scanSecretRefsInText(contents) {
			agg.addRawRef(ref, owner)
		}
	}
}

func collectTemplates(ch *chart.Chart, prefix string, out map[string]string) {
	if ch == nil {
		return
	}
	base := ch.Name()
	if base == "" {
		base = "chart"
	}
	if prefix != "" {
		base = filepath.ToSlash(filepath.Join(prefix, base))
	}
	for _, tpl := range ch.Templates {
		name := strings.TrimSpace(tpl.Name)
		if name == "" {
			continue
		}
		out[filepath.ToSlash(filepath.Join(base, name))] = string(tpl.Data)
	}
	for _, dep := range ch.Dependencies() {
		collectTemplates(dep, base, out)
	}
}

func scanSetValues(agg *secretDiscoverAgg, values []string, owner string) {
	for _, entry := range values {
		for _, ref := range scanSecretRefsInText(entry) {
			agg.addRawRef(ref, owner)
		}
	}
}

func scanSetFileValues(agg *secretDiscoverAgg, values []string, owner string) {
	for _, entry := range values {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		path := strings.TrimSpace(parts[1])
		if path == "" {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, ref := range scanSecretRefsInText(string(raw)) {
			agg.addRawRef(ref, owner)
		}
	}
}

func scanSetValuesFromMap(agg *secretDiscoverAgg, values map[string]string, owner string) {
	if len(values) == 0 {
		return
	}
	setList := make([]string, 0, len(values))
	for key, val := range values {
		if strings.TrimSpace(val) == "" {
			continue
		}
		setList = append(setList, fmt.Sprintf("%s=%s", key, val))
	}
	scanSetValues(agg, setList, owner)
}

func scanSecretRefsInText(text string) []string {
	var refs []string
	if strings.Index(text, "secret://") == -1 {
		return refs
	}
	i := 0
	for {
		pos := strings.Index(text[i:], "secret://")
		if pos == -1 {
			break
		}
		start := i + pos
		end := start + len("secret://")
		for end < len(text) && !isSecretRefDelimiter(text[end]) {
			end++
		}
		ref := strings.TrimRight(text[start:end], ".,;")
		if ref != "" {
			refs = append(refs, ref)
		}
		i = end
	}
	return refs
}

func isSecretRefDelimiter(ch byte) bool {
	switch ch {
	case ' ', '\n', '\t', '\r', '"', '\'', '`', ')', ']', '}', '<', '>', '|', ',':
		return true
	default:
		return false
	}
}

func splitRefKey(path string) (string, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	parts := strings.SplitN(path, "#", 2)
	base := strings.Trim(strings.TrimSpace(parts[0]), "/")
	key := ""
	if len(parts) > 1 {
		key = strings.TrimSpace(parts[1])
	}
	return base, key
}

func buildRefString(provider string, path string, key string) string {
	ref := "secret://" + strings.TrimSpace(provider)
	if path != "" {
		ref = ref + "/" + strings.Trim(strings.TrimSpace(path), "/")
	}
	if key != "" {
		ref = ref + "#" + strings.TrimSpace(key)
	}
	return ref
}

func buildProviderUsage(providerNames []string, refs []secretDiscoverRef) []secretDiscoverProvider {
	used := map[string]struct{}{}
	for _, ref := range refs {
		if ref.Provider != "" {
			used[ref.Provider] = struct{}{}
		}
	}
	out := make([]secretDiscoverProvider, 0, len(providerNames))
	for _, name := range providerNames {
		_, ok := used[name]
		out = append(out, secretDiscoverProvider{Name: name, Used: ok})
	}
	return out
}

func renderSecretDiscoverTable(out io.Writer, result secretDiscoverOutput, includeUnused bool, includeOrphaned bool) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	fmt.Fprintln(tw, "PROVIDER\tPATH\tKEY\tCOUNT\tOWNERS")
	for _, ref := range result.Refs {
		owners := strings.Join(ref.Owners, ", ")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", ref.Provider, ref.Path, ref.Key, ref.Count, owners)
	}

	if includeOrphaned && len(result.Orphans) > 0 {
		fmt.Fprintln(tw)
		fmt.Fprintln(tw, "ORPHANED_REFERENCE\tCOUNT\tOWNERS\tMESSAGE")
		for _, orphan := range result.Orphans {
			owners := strings.Join(orphan.Owners, ", ")
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", orphan.Reference, orphan.Count, owners, orphan.Message)
		}
	}

	if includeUnused && len(result.Providers) > 0 {
		fmt.Fprintln(tw)
		fmt.Fprintln(tw, "PROVIDER\tUSED")
		for _, provider := range result.Providers {
			fmt.Fprintf(tw, "%s\t%v\n", provider.Name, provider.Used)
		}
	}
	return nil
}

func sortedKeysFromSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Values merging is retained here for potential future use.
func mergeValues(settings *cli.EnvSettings, valueFiles, setVals, setStringVals, setFileVals []string) (map[string]interface{}, error) {
	valOpts := &values.Options{
		ValueFiles:   valueFiles,
		Values:       setVals,
		StringValues: setStringVals,
		FileValues:   setFileVals,
	}
	providers := getter.All(settings)
	return valOpts.MergeValues(providers)
}
