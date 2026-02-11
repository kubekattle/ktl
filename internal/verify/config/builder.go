package config

import (
	"fmt"
	"strings"
)

type Params struct {
	Kind string

	ChartPath   string
	Release     string
	Namespace   string
	Manifest    string
	ValuesFiles []string
	SetValues   []string

	UseCluster  bool
	IncludeCRDs bool

	Mode        string
	FailOn      string
	Format      string
	Report      string
	KubeContext string
}

func BuildFromParams(params Params) (Config, error) {
	kind := strings.ToLower(strings.TrimSpace(params.Kind))

	inferKind := func() string {
		if strings.TrimSpace(params.ChartPath) != "" {
			return "chart"
		}
		if strings.TrimSpace(params.Manifest) != "" {
			return "manifest"
		}
		if strings.TrimSpace(params.Namespace) != "" {
			return "namespace"
		}
		return ""
	}
	if kind == "" {
		kind = inferKind()
	}

	cfg := Config{Version: "v1"}
	switch kind {
	case "":
		return Config{}, nil
	case "chart":
		chartPath := strings.TrimSpace(params.ChartPath)
		release := strings.TrimSpace(params.Release)
		namespace := strings.TrimSpace(params.Namespace)
		if namespace == "" {
			namespace = "default"
		}
		if chartPath == "" {
			return Config{}, fmt.Errorf("--chart is required (kind=chart)")
		}
		if release == "" {
			return Config{}, fmt.Errorf("--release is required (kind=chart)")
		}
		mode := strings.TrimSpace(params.Mode)
		if mode == "" {
			mode = "block"
		}
		cfg.Target = Target{
			Kind: "chart",
			Chart: Chart{
				Chart:       chartPath,
				Release:     release,
				Namespace:   namespace,
				ValuesFiles: splitCSV(params.ValuesFiles),
				SetValues:   splitCSV(params.SetValues),
				UseCluster:  ptrBool(params.UseCluster),
				IncludeCRDs: ptrBool(params.IncludeCRDs),
			},
		}
		cfg.Verify = Rules{
			Mode:   mode,
			FailOn: strings.TrimSpace(params.FailOn),
		}
		cfg.Output = Output{
			Format: strings.TrimSpace(params.Format),
			Report: strings.TrimSpace(params.Report),
		}
		cfg.Kube = Kube{
			Context: strings.TrimSpace(params.KubeContext),
		}
	case "namespace":
		namespace := strings.TrimSpace(params.Namespace)
		if namespace == "" {
			return Config{}, fmt.Errorf("--namespace is required (kind=namespace)")
		}
		mode := strings.TrimSpace(params.Mode)
		if mode == "" {
			mode = "warn"
		}
		cfg.Target = Target{
			Kind:      "namespace",
			Namespace: namespace,
		}
		cfg.Verify = Rules{
			Mode:   mode,
			FailOn: strings.TrimSpace(params.FailOn),
		}
		cfg.Output = Output{
			Format: strings.TrimSpace(params.Format),
			Report: strings.TrimSpace(params.Report),
		}
		cfg.Kube = Kube{
			Context: strings.TrimSpace(params.KubeContext),
		}
	case "manifest":
		manifest := strings.TrimSpace(params.Manifest)
		if manifest == "" {
			return Config{}, fmt.Errorf("--manifest is required (kind=manifest)")
		}
		mode := strings.TrimSpace(params.Mode)
		if mode == "" {
			mode = "block"
		}
		cfg.Target = Target{
			Kind:     "manifest",
			Manifest: manifest,
		}
		cfg.Verify = Rules{
			Mode:   mode,
			FailOn: strings.TrimSpace(params.FailOn),
		}
		cfg.Output = Output{
			Format: strings.TrimSpace(params.Format),
			Report: strings.TrimSpace(params.Report),
		}
	default:
		return Config{}, fmt.Errorf("--kind must be one of: chart, namespace, manifest")
	}

	if strings.TrimSpace(cfg.Output.Report) == "" {
		cfg.Output.Report = "-"
	}
	if err := cfg.Validate("."); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func splitCSV(vals []string) []string {
	var out []string
	for _, v := range vals {
		if strings.TrimSpace(v) == "" {
			continue
		}
		for _, part := range strings.Split(v, ",") {
			if strings.TrimSpace(part) != "" {
				out = append(out, strings.TrimSpace(part))
			}
		}
	}
	return out
}

func ptrBool(v bool) *bool {
	return &v
}
