package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kubekattle/ktl/internal/kube"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"
)

// RunDriftCheck executes the drift detection logic based on the specified mode.
// It supports "last-applied" (comparing against the live cluster) and "desired"
// (comparing the live cluster against a fresh render of the chart).
func RunDriftCheck(ctx context.Context, actionCfg *action.Configuration, settings *cli.EnvSettings, kubeClient *kube.Client, mode string, releaseName string, installOpts InstallOptions) error {
	getAction := action.NewGet(actionCfg)
	current, err := getAction.Run(releaseName)
	if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("drift guard: read current release manifest: %w", err)
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "last-applied":
		if current != nil && strings.TrimSpace(current.Manifest) != "" {
			report, derr := CheckReleaseDrift(ctx, releaseName, current.Manifest, DriftLiveGetterFromKube(kubeClient))
			if derr != nil {
				return fmt.Errorf("drift guard: %w", derr)
			}
			if !report.Empty() {
				return fmt.Errorf("drift detected for release %s in ns/%s (%d objects)\n%s", releaseName, installOpts.Namespace, len(report.Items), FormatDriftReport(report, 6, 80))
			}
		}
	case "desired":
		// Force dry-run options for the preview render
		previewOpts := installOpts
		previewOpts.DryRun = true
		previewOpts.Wait = false
		previewOpts.Atomic = false
		// Ensure we don't accidentally upgrade if we just wanted to check drift
		// although InstallOrUpgrade handles dry-run safely.

		preview, previewErr := InstallOrUpgrade(ctx, actionCfg, settings, previewOpts)
		if previewErr != nil {
			return fmt.Errorf("drift guard: render desired: %w", previewErr)
		}
		if preview != nil && preview.Release != nil && strings.TrimSpace(preview.Release.Manifest) != "" {
			report, derr := CheckReleaseDriftWithOptions(ctx, releaseName, preview.Release.Manifest, DriftLiveGetterFromKube(kubeClient), DriftOptions{
				RequireHelmOwnership: true,
				IgnoreMissing:        true,
				MaxConcurrency:       8,
			})
			if derr != nil {
				return fmt.Errorf("drift guard: %w", derr)
			}
			if !report.Empty() {
				return fmt.Errorf("drift detected against desired state for release %s in ns/%s (%d objects)\n%s", releaseName, installOpts.Namespace, len(report.Items), FormatDriftReport(report, 6, 80))
			}
		}
	default:
		return fmt.Errorf("invalid --drift-guard-mode %q (expected last-applied or desired)", mode)
	}
	return nil
}
