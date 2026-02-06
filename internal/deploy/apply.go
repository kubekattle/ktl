// File: internal/deploy/apply.go
// Brief: Internal deploy package implementation for 'apply'.

// apply.go wraps Helm install/upgrade hooks so ktl can apply releases.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/ktl/internal/secretstore"
	"github.com/pmezard/go-difflib/difflib"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
)

// InstallOptions capture user-facing helm install/upgrade settings.
type InstallOptions struct {
	Chart             string
	Version           string
	ReleaseName       string
	Namespace         string
	ValuesFiles       []string
	SetValues         []string
	SetStringValues   []string
	SetFileValues     []string
	Secrets           *SecretOptions
	Timeout           time.Duration
	Wait              bool
	Atomic            bool
	CreateNamespace   bool
	DryRun            bool
	Diff              bool
	UpgradeOnly       bool
	ProgressObservers []ProgressObserver
}

type InstallResult struct {
	Release            *release.Release
	ManifestDiff       string
	PlanSummary        *PlanSummary
	PlanSummarizeError string
}

// InstallOrUpgrade renders the chart and applies it using Helm's upgrade --install semantics.
func InstallOrUpgrade(ctx context.Context, actionCfg *action.Configuration, settings *cli.EnvSettings, opts InstallOptions) (*InstallResult, error) {
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

	observers := append([]ProgressObserver(nil), opts.ProgressObservers...)
	notifyPhaseStarted(observers, PhaseRender)

	chartPathOptions := action.ChartPathOptions{Version: opts.Version}
	chartPath, err := chartPathOptions.LocateChart(opts.Chart, settings)
	if err != nil {
		notifyPhaseCompleted(observers, PhaseRender, "failed", err.Error())
		return nil, fmt.Errorf("locate chart: %w", err)
	}
	chartRequested, err := loader.Load(chartPath)
	if err != nil {
		notifyPhaseCompleted(observers, PhaseRender, "failed", err.Error())
		return nil, fmt.Errorf("load chart: %w", err)
	}
	if err := ensureInstallable(chartRequested); err != nil {
		notifyPhaseCompleted(observers, PhaseRender, "failed", err.Error())
		return nil, fmt.Errorf("chart not installable: %w", err)
	}

	vals, err := buildValues(ctx, settings, opts.ValuesFiles, opts.SetValues, opts.SetStringValues, opts.SetFileValues, opts.Secrets)
	if err != nil {
		notifyPhaseCompleted(observers, PhaseRender, "failed", err.Error())
		return nil, err
	}
	chartName := "chart"
	if chartRequested.Metadata != nil && chartRequested.Metadata.Name != "" {
		chartName = chartRequested.Metadata.Name
	}
	notifyPhaseCompleted(observers, PhaseRender, "succeeded", fmt.Sprintf("Rendered chart %s", chartName))
	notifyEvent(observers, "info", fmt.Sprintf("Deploying release %s in namespace %s", opts.ReleaseName, namespace))

	upgrade := action.NewUpgrade(actionCfg)
	upgrade.Namespace = namespace
	upgrade.Timeout = opts.Timeout
	upgrade.Wait = opts.Wait
	upgrade.Atomic = opts.Atomic
	upgrade.Install = true
	upgrade.DryRun = opts.DryRun || opts.Diff

	diffEnabled := opts.Diff
	if diffEnabled {
		notifyPhaseStarted(observers, PhaseDiff)
	} else {
		notifyPhaseCompleted(observers, PhaseDiff, "skipped", "Diff disabled")
	}

	if opts.Wait {
		notifyPhaseStarted(observers, PhaseWait)
	} else {
		notifyPhaseCompleted(observers, PhaseWait, "skipped", "Helm --wait disabled")
	}
	notifyPhaseStarted(observers, PhaseUpgrade)

	var previousManifest string
	if opts.Diff {
		getAction := action.NewGet(actionCfg)
		if rel, err := getAction.Run(opts.ReleaseName); err == nil && rel != nil {
			previousManifest = rel.Manifest
		}
	}

	release, err := upgrade.RunWithContext(ctx, opts.ReleaseName, chartRequested, vals)
	installPerformed := false
	if err != nil {
		if !opts.UpgradeOnly && isNoDeployedReleaseErr(err) {
			notifyPhaseCompleted(observers, PhaseUpgrade, "skipped", "No deployed release; installing fresh")
			notifyPhaseStarted(observers, PhaseInstall)
			install := action.NewInstall(actionCfg)
			install.ReleaseName = opts.ReleaseName
			install.Namespace = namespace
			install.Timeout = opts.Timeout
			install.Wait = opts.Wait
			install.Atomic = opts.Atomic
			install.CreateNamespace = opts.CreateNamespace
			install.DryRun = upgrade.DryRun
			release, err = install.RunWithContext(ctx, chartRequested, vals)
			if err != nil {
				notifyPhaseCompleted(observers, PhaseInstall, "failed", err.Error())
				if opts.Wait {
					notifyPhaseCompleted(observers, PhaseWait, "failed", "Install failed")
				}
				if diffEnabled {
					notifyPhaseCompleted(observers, PhaseDiff, "failed", "Install failed before diff")
				}
				return nil, fmt.Errorf("helm install: %w", err)
			}
			installPerformed = true
			notifyPhaseCompleted(observers, PhaseInstall, "succeeded", "Release installed fresh")
		} else {
			notifyPhaseCompleted(observers, PhaseUpgrade, "failed", err.Error())
			if opts.Wait {
				notifyPhaseCompleted(observers, PhaseWait, "failed", "Upgrade failed")
			}
			if diffEnabled {
				notifyPhaseCompleted(observers, PhaseDiff, "failed", "Upgrade failed before diff")
			}
			if opts.UpgradeOnly && isNoDeployedReleaseErr(err) {
				return nil, wrapUpgradeOnlyNoDeployedReleaseErr(opts.ReleaseName, namespace, err)
			}
			return nil, fmt.Errorf("helm upgrade: %w", err)
		}
	} else {
		notifyPhaseCompleted(observers, PhaseUpgrade, "succeeded", "Release upgrade completed")
	}

	if opts.Wait {
		notifyPhaseCompleted(observers, PhaseWait, "succeeded", "Helm reported release ready")
	}
	if !installPerformed {
		notifyPhaseCompleted(observers, PhaseInstall, "skipped", "Install fallback not required")
	}

	result := &InstallResult{Release: release}
	if opts.Diff {
		result.ManifestDiff = diffManifests(previousManifest, release.Manifest)
		if kc, ok := actionCfg.KubeClient.(*kube.Client); ok && kc != nil {
			if summary, err := SummarizeManifestPlanWithHelmKube(kc, previousManifest, release.Manifest); err == nil {
				result.PlanSummary = summary
			} else if fallback, ferr := SummarizeManifestPlan(previousManifest, release.Manifest); ferr == nil {
				result.PlanSummary = fallback
				result.PlanSummarizeError = err.Error()
			} else {
				result.PlanSummarizeError = err.Error()
			}
		} else if summary, err := SummarizeManifestPlan(previousManifest, release.Manifest); err == nil {
			result.PlanSummary = summary
		} else {
			result.PlanSummarizeError = err.Error()
		}
		notifyEmitDiff(observers, result.ManifestDiff)
		msg := "No manifest changes"
		if result.ManifestDiff != "" {
			msg = "Rendered manifest diff"
		}
		notifyPhaseCompleted(observers, PhaseDiff, "succeeded", msg)
	}
	notifyPhaseStarted(observers, PhasePostHooks)
	notifyPhaseCompleted(observers, PhasePostHooks, "succeeded", "Helm post-upgrade hooks completed")
	return result, nil
}

func buildValues(ctx context.Context, settings *cli.EnvSettings, files, setVals, setStringVals, setFileVals []string, secrets *SecretOptions) (map[string]interface{}, error) {
	valOpts := &cliValues.Options{
		ValueFiles:   files,
		Values:       setVals,
		StringValues: setStringVals,
		FileValues:   setFileVals,
	}
	providers := getter.All(settings)
	vals, err := valOpts.MergeValues(providers)
	if err != nil {
		return nil, fmt.Errorf("merge values: %w", err)
	}
	if secrets == nil || secrets.Resolver == nil {
		refs := secretstore.FindRefs(vals)
		if len(refs) > 0 {
			limit := 3
			if len(refs) < limit {
				limit = len(refs)
			}
			extra := len(refs) - limit
			msg := fmt.Sprintf("secret reference(s) detected but no secret provider configured: %s", strings.Join(refs[:limit], ", "))
			if extra > 0 {
				msg = fmt.Sprintf("%s (and %d more)", msg, extra)
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return vals, nil
	}
	if secrets.Validate {
		if err := secretstore.ValidateRefs(ctx, secrets.Resolver, vals, secretstore.ValidationOptions{}); err != nil {
			return nil, err
		}
	}
	if err := secrets.Resolver.ResolveValues(ctx, vals); err != nil {
		return nil, err
	}
	report := secrets.Resolver.Audit()
	if !report.Empty() && secrets.AuditSink != nil {
		secrets.AuditSink(report)
	}
	return vals, nil
}

func diffManifests(previous, next string) string {
	if previous == next {
		return ""
	}
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(previous),
		B:        difflib.SplitLines(next),
		FromFile: "previous",
		ToFile:   "proposed",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return fmt.Sprintf("failed to render diff: %v", err)
	}
	return text
}

func ensureInstallable(ch *chart.Chart) error {
	if ch.Metadata == nil {
		return fmt.Errorf("chart metadata missing")
	}
	chartType := ch.Metadata.Type
	if chartType == "" || chartType == "application" {
		return nil
	}
	return fmt.Errorf("%s charts are not installable", chartType)
}

func isNoDeployedReleaseErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return true
	}
	return strings.Contains(err.Error(), "has no deployed releases")
}

func wrapUpgradeOnlyNoDeployedReleaseErr(releaseName, namespace string, err error) error {
	releaseName = strings.TrimSpace(releaseName)
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}
	if releaseName == "" {
		return fmt.Errorf("helm upgrade: %w", err)
	}
	return fmt.Errorf(
		"helm upgrade: %w; release %q is not deployed in namespace %q (omit --upgrade to allow install fallback, or pick an existing release name from `ktl list --namespace %s`)",
		err,
		releaseName,
		namespace,
		namespace,
	)
}

func notifyPhaseStarted(observers []ProgressObserver, name string) {
	if len(observers) == 0 || strings.TrimSpace(name) == "" {
		return
	}
	for _, obs := range observers {
		if obs != nil {
			obs.PhaseStarted(name)
		}
	}
}

func notifyPhaseCompleted(observers []ProgressObserver, name, status, message string) {
	if len(observers) == 0 || strings.TrimSpace(name) == "" {
		return
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "succeeded"
	}
	message = strings.TrimSpace(message)
	for _, obs := range observers {
		if obs != nil {
			obs.PhaseCompleted(name, status, message)
		}
	}
}

func notifyEvent(observers []ProgressObserver, level, message string) {
	if len(observers) == 0 {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	for _, obs := range observers {
		if obs != nil {
			obs.EmitEvent(level, message)
		}
	}
}

func notifyEmitDiff(observers []ProgressObserver, diff string) {
	if len(observers) == 0 {
		return
	}
	for _, obs := range observers {
		if obs != nil {
			obs.SetDiff(diff)
		}
	}
}
