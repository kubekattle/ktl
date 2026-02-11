package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/kube"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

// GeneratePlanPreview renders the chart in dry-run mode to produce a plan summary.
// If planServer is true, it also performs server-side checks to detect replacements.
func GeneratePlanPreview(ctx context.Context, actionCfg *action.Configuration, settings *cli.EnvSettings, kubeClient *kube.Client, opts InstallOptions, planServer bool) (*InstallResult, error) {
	previewOpts := opts
	previewOpts.DryRun = true
	previewOpts.Wait = false
	previewOpts.Atomic = false
	// Diff must be enabled to generate the plan summary
	previewOpts.Diff = true

	preview, err := InstallOrUpgrade(ctx, actionCfg, settings, previewOpts)
	if err != nil {
		return nil, err
	}

	if planServer && preview != nil && preview.Release != nil && preview.PlanSummary != nil {
		hints, hintErr := DetectServerSideReplaceKeys(ctx, kubeClient, preview.Release.Manifest, ServerPlanOptions{FieldManager: "ktl-plan", Force: true})
		// We ignore the error here as it's an enhancement, similar to the original CLI logic which only logged it.
		// If strict error handling is needed, we could return it.
		_ = hintErr

		if len(hints) > 0 {
			for i := range preview.PlanSummary.Changes {
				ch := preview.PlanSummary.Changes[i]
				if ch.IsHook || ch.Action != PlanUpdate {
					continue
				}
				key := fmt.Sprintf("%s/%s/%s/%s/%s", strings.ToLower(strings.TrimSpace(ch.Group)), strings.ToLower(strings.TrimSpace(ch.Version)), strings.ToLower(strings.TrimSpace(ch.Kind)), strings.TrimSpace(ch.Namespace), strings.TrimSpace(ch.Name))
				if hints[key] {
					preview.PlanSummary.Changes[i].Action = PlanReplace
					preview.PlanSummary.Change--
					preview.PlanSummary.Replace++
				}
			}
		}
	}

	return preview, nil
}
