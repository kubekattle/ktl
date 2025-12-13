// template.go renders Helm manifests and change summaries for deploy plan/install operations.
package deploy

import (
	"context"
	"fmt"
	"path"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

// TemplateOptions controls rendering behavior for helm template equivalents.
type TemplateOptions struct {
	Chart           string
	Version         string
	RepoURL         string
	ReleaseName     string
	Namespace       string
	ValuesFiles     []string
	SetValues       []string
	SetStringValues []string
	SetFileValues   []string
	IncludeCRDs     bool
}

// TemplateResult holds rendered manifests and optional notes.
type TemplateResult struct {
	Manifest     string
	Notes        string
	ChartVersion string
	Templates    map[string]string
}

// RenderTemplate renders the provided chart without applying it to the cluster.
func RenderTemplate(ctx context.Context, actionCfg *action.Configuration, settings *cli.EnvSettings, opts TemplateOptions) (*TemplateResult, error) {
	if opts.Chart == "" {
		return nil, fmt.Errorf("chart reference is required")
	}
	if opts.ReleaseName == "" {
		return nil, fmt.Errorf("release name is required")
	}
	namespace := opts.Namespace
	if namespace == "" {
		namespace = settings.Namespace()
	}
	if namespace == "" {
		namespace = "default"
	}

	chartOpts := action.ChartPathOptions{RepoURL: opts.RepoURL, Version: opts.Version}
	chartPath, err := chartOpts.LocateChart(opts.Chart, settings)
	if err != nil {
		return nil, fmt.Errorf("locate chart: %w", err)
	}
	chartRequested, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	if err := ensureInstallable(chartRequested); err != nil {
		return nil, fmt.Errorf("chart not installable: %w", err)
	}

	vals, err := buildValues(settings, opts.ValuesFiles, opts.SetValues, opts.SetStringValues, opts.SetFileValues)
	if err != nil {
		return nil, err
	}

	installer := action.NewInstall(actionCfg)
	installer.DryRun = true
	installer.ReleaseName = opts.ReleaseName
	installer.Namespace = namespace
	installer.Replace = true
	installer.ClientOnly = true
	installer.IncludeCRDs = opts.IncludeCRDs

	rel, err := installer.RunWithContext(ctx, chartRequested, vals)
	if err != nil {
		return nil, fmt.Errorf("helm template: %w", err)
	}

	templateSources := make(map[string]string)
	collectTemplates(chartRequested, "", templateSources)

	return &TemplateResult{
		Manifest:     rel.Manifest,
		Notes:        rel.Info.Notes,
		ChartVersion: chartRequested.Metadata.Version,
		Templates:    templateSources,
	}, nil
}

func collectTemplates(ch *chart.Chart, prefix string, out map[string]string) {
	if ch == nil {
		return
	}
	base := ch.Metadata.Name
	if base == "" {
		base = "chart"
	}
	if prefix != "" {
		base = path.Join(prefix, base)
	}
	for _, tpl := range ch.Templates {
		name := tpl.Name
		if name == "" {
			continue
		}
		out[path.Join(base, name)] = string(tpl.Data)
	}
	for _, dep := range ch.Dependencies() {
		collectTemplates(dep, base, out)
	}
}
