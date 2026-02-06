package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/secretstore"
	"github.com/example/ktl/internal/verify"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

type Config struct {
	Version  string  `yaml:"version,omitempty"`
	Target   Target  `yaml:"target"`
	Verify   Rules   `yaml:"verify,omitempty"`
	Output   Output  `yaml:"output,omitempty"`
	Kube     Kube    `yaml:"kube,omitempty"`
	LogLevel *string `yaml:"-"` // injected by caller when needed
}

type Target struct {
	Kind      string `yaml:"kind"` // namespace|chart|manifest
	Namespace string `yaml:"namespace,omitempty"`
	Manifest  string `yaml:"manifest,omitempty"`
	Chart     Chart  `yaml:"chart,omitempty"`
}

type Chart struct {
	Chart       string   `yaml:"chart,omitempty"`
	Release     string   `yaml:"release,omitempty"`
	Namespace   string   `yaml:"namespace,omitempty"`
	ValuesFiles []string `yaml:"values,omitempty"`
	SetValues   []string `yaml:"set,omitempty"`

	UseCluster  *bool `yaml:"useCluster,omitempty"`
	IncludeCRDs *bool `yaml:"includeCRDs,omitempty"`
}

type Rules struct {
	Mode          string                `yaml:"mode,omitempty"`   // warn|block|off
	FailOn        string                `yaml:"failOn,omitempty"` // info|low|medium|high|critical
	RulesDir      string                `yaml:"rulesDir,omitempty"`
	RulesPath     []string              `yaml:"rulesPath,omitempty"`
	Selectors     verify.SelectorSet    `yaml:"selectors,omitempty"`
	RuleSelectors []verify.RuleSelector `yaml:"ruleSelectors,omitempty"`

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

type Output struct {
	Format string `yaml:"format,omitempty"` // table|json|sarif|html|md
	Report string `yaml:"report,omitempty"` // path or "-" (stdout)
}

type Kube struct {
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
	Context    string `yaml:"context,omitempty"`

	KubeconfigSet bool `yaml:"-"`
	ContextSet    bool `yaml:"-"`
}

func Load(path string) (*Config, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", fmt.Errorf("verify config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, "", err
	}
	var rawMap map[string]any
	_ = yaml.Unmarshal(raw, &rawMap)
	if kubeSection, ok := rawMap["kube"].(map[string]any); ok {
		if _, ok := kubeSection["kubeconfig"]; ok {
			cfg.Kube.KubeconfigSet = true
		}
		if _, ok := kubeSection["context"]; ok {
			cfg.Kube.ContextSet = true
		}
	}
	baseDir := filepath.Dir(path)
	if baseDir == "" {
		baseDir = "."
	}
	baseDir, _ = filepath.Abs(baseDir)
	cfg.resolvePaths(baseDir)
	return &cfg, baseDir, nil
}

func (c *Config) ResolvePaths(baseDir string) {
	if c == nil {
		return
	}
	c.Target.Manifest = resolveRelPath(baseDir, c.Target.Manifest)
	c.Verify.RulesDir = resolveRelPath(baseDir, c.Verify.RulesDir)
	for i := range c.Verify.RulesPath {
		c.Verify.RulesPath[i] = resolveRelPath(baseDir, c.Verify.RulesPath[i])
	}
	c.Verify.Policy.Ref = resolveRelMaybeURL(baseDir, c.Verify.Policy.Ref)
	c.Verify.Baseline.Read = resolveRelPath(baseDir, c.Verify.Baseline.Read)
	c.Verify.Baseline.Write = resolveRelPath(baseDir, c.Verify.Baseline.Write)
	c.Verify.Exposure.Output = resolveRelPath(baseDir, c.Verify.Exposure.Output)
	c.Output.Report = resolveRelPath(baseDir, c.Output.Report)
	c.Kube.Kubeconfig = resolveRelPath(baseDir, c.Kube.Kubeconfig)
}

func (c *Config) resolvePaths(baseDir string) { c.ResolvePaths(baseDir) }

func (c *Config) Validate(baseDir string) error {
	if c == nil {
		return fmt.Errorf("verify config is required")
	}
	if c.Kube.KubeconfigSet && strings.TrimSpace(c.Kube.Kubeconfig) == "" {
		return fmt.Errorf("kube.kubeconfig is set but empty; set a path or remove it")
	}
	if c.Kube.ContextSet && strings.TrimSpace(c.Kube.Context) == "" {
		return fmt.Errorf("kube.context is set but empty; set a context or remove it")
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

func (c *Config) TargetLabel() string {
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

func (c *Config) StartPhase() string {
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

func (c *Config) LoadObjects(ctx context.Context, baseDir string, kubeconfig string, kubeContext string, console *verify.Console) ([]map[string]any, string, *kube.APIRequestStats, error) {
	switch strings.ToLower(strings.TrimSpace(c.Target.Kind)) {
	case "manifest":
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "decode"})
		}
		raw, err := os.ReadFile(strings.TrimSpace(c.Target.Manifest))
		if err != nil {
			return nil, "", nil, err
		}
		objs, err := verify.DecodeK8SYAML(string(raw))
		return objs, "", nil, err
	case "namespace":
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "collect"})
		}
		client, err := kube.New(ctx, strings.TrimSpace(kubeconfig), strings.TrimSpace(kubeContext))
		if err != nil {
			return nil, "", nil, err
		}
		objs, err := collectNamespacedObjects(ctx, client, strings.TrimSpace(c.Target.Namespace))
		return objs, "", client.APIStats, err
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

		apiStats := kube.NewAPIRequestStats()
		if flags, ok := settings.RESTClientGetter().(*genericclioptions.ConfigFlags); ok && flags != nil {
			wrap := flags.WrapConfigFn
			flags.WrapConfigFn = func(cfg *rest.Config) *rest.Config {
				if wrap != nil {
					cfg = wrap(cfg)
				}
				kube.AttachAPITelemetry(cfg, apiStats)
				return cfg
			}
		}

		actionCfg := new(action.Configuration)
		if err := actionCfg.Init(settings.RESTClientGetter(), strings.TrimSpace(c.Target.Chart.Namespace), os.Getenv("HELM_DRIVER"), func(format string, args ...interface{}) {
			// keep quiet
			_ = c.LogLevel
		}); err != nil {
			return nil, "", apiStats, err
		}

		useCluster := true
		if c.Target.Chart.UseCluster != nil {
			useCluster = *c.Target.Chart.UseCluster
		}
		includeCRDs := true
		if c.Target.Chart.IncludeCRDs != nil {
			includeCRDs = *c.Target.Chart.IncludeCRDs
		}

		secretsCfg, secretsBaseDir, err := secretstore.LoadConfigFromApp(ctx, c.Target.Chart.Chart, "")
		if err != nil {
			return nil, "", apiStats, err
		}
		secretResolver, err := secretstore.NewResolver(secretsCfg, secretstore.ResolverOptions{
			BaseDir: secretsBaseDir,
			Mode:    secretstore.ResolveModeValue,
		})
		if err != nil {
			return nil, "", apiStats, err
		}

		result, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
			Chart:       strings.TrimSpace(c.Target.Chart.Chart),
			ReleaseName: strings.TrimSpace(c.Target.Chart.Release),
			Namespace:   strings.TrimSpace(c.Target.Chart.Namespace),
			ValuesFiles: c.Target.Chart.ValuesFiles,
			SetValues:   c.Target.Chart.SetValues,
			UseCluster:  useCluster,
			IncludeCRDs: includeCRDs,
			Secrets:     &deploy.SecretOptions{Resolver: secretResolver},
		})
		if err != nil {
			return nil, "", apiStats, err
		}
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "decode"})
		}
		objs, err := verify.DecodeK8SYAML(result.Manifest)
		return objs, result.Manifest, apiStats, err
	default:
		return nil, "", nil, fmt.Errorf("unsupported target.kind %q", c.Target.Kind)
	}
}

func (c *Config) AppendInputs(rep *verify.Report, renderedManifest string) {
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

func ResolveRelPath(baseDir string, p string) string {
	return resolveRelPath(baseDir, p)
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

func RepModeFormat(v string) verify.OutputFormat {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "table":
		return verify.OutputTable
	case "json":
		return verify.OutputJSON
	case "sarif":
		return verify.OutputSARIF
	case "html":
		return verify.OutputHTML
	case "md", "markdown":
		return verify.OutputMD
	default:
		return verify.OutputTable
	}
}

func OpenOutput(defaultWriter io.Writer, path string) (io.Writer, io.Closer, error) {
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

// collectNamespacedObjects is kept here to avoid import cycles and mirrors the original implementation.
func collectNamespacedObjects(ctx context.Context, client *kube.Client, namespace string) ([]map[string]any, error) {
	var out []map[string]any
	add := func(obj any, err error) error {
		if err != nil {
			return err
		}
		m, convErr := toMap(obj)
		if convErr != nil {
			return convErr
		}
		out = append(out, m)
		return nil
	}

	if err := add(client.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})); err != nil {
		return nil, err
	}

	list := func(err error, objs ...any) error {
		if err != nil {
			return err
		}
		for _, o := range objs {
			if err := add(o, nil); err != nil {
				return err
			}
		}
		return nil
	}

	if err := list(func() error {
		podList, err := client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range podList.Items {
			if err := add(&podList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		dsList, err := client.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range dsList.Items {
			if err := add(&dsList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		asList, err := client.Clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range asList.Items {
			if err := add(&asList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		ssList, err := client.Clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range ssList.Items {
			if err := add(&ssList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		rsList, err := client.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range rsList.Items {
			if err := add(&rsList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		cronList, err := client.Clientset.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range cronList.Items {
			if err := add(&cronList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		jobList, err := client.Clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range jobList.Items {
			if err := add(&jobList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		hpaList, err := client.Clientset.AutoscalingV1().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range hpaList.Items {
			if err := add(&hpaList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		svcList, err := client.Clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range svcList.Items {
			if err := add(&svcList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		cmList, err := client.Clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range cmList.Items {
			if err := add(&cmList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		secList, err := client.Clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range secList.Items {
			if err := add(&secList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		pdbList, err := client.Clientset.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range pdbList.Items {
			if err := add(&pdbList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	if err := list(func() error {
		ingList, err := client.Clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i := range ingList.Items {
			if err := add(&ingList.Items[i], nil); err != nil {
				return err
			}
		}
		return nil
	}()); err != nil {
		return nil, err
	}

	return out, nil
}

func toMap(obj any) (map[string]any, error) {
	if m, ok := obj.(map[string]any); ok {
		return m, nil
	}
	if runtimeObj, ok := obj.(runtime.Object); ok {
		return runtime.DefaultUnstructuredConverter.ToUnstructured(runtimeObj)
	}
	return nil, fmt.Errorf("unsupported object type %T", obj)
}
