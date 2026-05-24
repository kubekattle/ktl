package stack

import (
	"context"
	"strings"
	"time"
)

const (
	OpsApplyReplayAPIVersion = "torque.dev/ops/apply-replay/v1alpha1"
	OpsApplyReplayKind       = "OpsApplyReplay"
)

type OpsApplyReplayOptions struct {
	Required bool
	Approved bool
}

type OpsApplyReplayResult struct {
	APIVersion  string                `json:"apiVersion"`
	Kind        string                `json:"kind"`
	Status      string                `json:"status"`
	GeneratedAt string                `json:"generatedAt"`
	Summary     OpsApplyReplaySummary `json:"summary"`
	Checks      []OpsApplyReplayCheck `json:"checks,omitempty"`
	Blockers    []OpsPlanBlocker      `json:"blockers,omitempty"`
}

type OpsApplyReplaySummary struct {
	Checks         int    `json:"checks"`
	Passed         int    `json:"passed"`
	Blocked        int    `json:"blocked"`
	SourceKind     string `json:"sourceKind,omitempty"`
	PlanHash       string `json:"planHash,omitempty"`
	Approved       bool   `json:"approved"`
	SealedVerified bool   `json:"sealedVerified"`
}

type OpsApplyReplayCheck struct {
	Code           string `json:"code"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	Source         string `json:"source,omitempty"`
	ExpectedDigest string `json:"expectedDigest,omitempty"`
	ObservedDigest string `json:"observedDigest,omitempty"`
}

func EvaluateOpsApplyReplay(ctx context.Context, p *Plan, opts OpsApplyReplayOptions) *OpsApplyReplayResult {
	if !opts.Required {
		return nil
	}
	result := &OpsApplyReplayResult{
		APIVersion:  OpsApplyReplayAPIVersion,
		Kind:        OpsApplyReplayKind,
		Status:      "eligible",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Summary: OpsApplyReplaySummary{
			Approved: opts.Approved,
		},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:   "ops.replay.context",
			Status: "blocked",
			Reason: err.Error(),
		})
	}
	if opts.Approved {
		addOpsReplayCheck(result, OpsApplyReplayCheck{
			Code:   "ops.replay.approval",
			Status: "passed",
			Reason: "approved replay was explicitly confirmed",
		})
	} else {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:   "ops.replay.approval_required",
			Status: "blocked",
			Reason: "ops stack apply replay requires --yes",
		})
	}
	if p == nil {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:   "ops.replay.plan_missing",
			Status: "blocked",
			Reason: "stack plan is missing",
		})
		return finalizeOpsReplay(result)
	}
	if p.Ops == nil {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:   "ops.replay.ops_inputs_missing",
			Status: "blocked",
			Reason: "ops replay requires frozen ops plan inputs",
		})
		return finalizeOpsReplay(result)
	}
	sealed := p.Sealed
	if sealed == nil {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:   "ops.replay.sealed_plan_missing",
			Status: "blocked",
			Reason: "ops replay requires a sealed plan loaded from --from-bundle or --sealed-dir",
		})
		return finalizeOpsReplay(result)
	}
	result.Summary.SourceKind = strings.TrimSpace(sealed.SourceKind)
	result.Summary.PlanHash = strings.TrimSpace(sealed.PlanHash)
	result.Summary.SealedVerified = sealed.Verified
	if !sealed.Verified {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:   "ops.replay.sealed_plan_unverified",
			Status: "blocked",
			Source: strings.TrimSpace(sealed.SourcePath),
			Reason: "sealed plan verification did not complete",
		})
	}
	wantPlan := strings.TrimSpace(sealed.PlanHash)
	gotPlan := strings.TrimSpace(sealed.ComputedPlanHash)
	if wantPlan == "" || gotPlan == "" {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:           "ops.replay.plan_digest_unverifiable",
			Status:         "blocked",
			Source:         strings.TrimSpace(sealed.SourcePath),
			ExpectedDigest: wantPlan,
			ObservedDigest: gotPlan,
			Reason:         "sealed plan digest was not recorded",
		})
	} else if wantPlan != gotPlan {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:           "ops.replay.plan_digest_mismatch",
			Status:         "blocked",
			Source:         strings.TrimSpace(sealed.SourcePath),
			ExpectedDigest: wantPlan,
			ObservedDigest: gotPlan,
			Reason:         "sealed plan digest changed before replay",
		})
	} else {
		addOpsReplayCheck(result, OpsApplyReplayCheck{
			Code:           "ops.replay.plan_digest",
			Status:         "passed",
			Source:         strings.TrimSpace(sealed.SourcePath),
			ExpectedDigest: wantPlan,
			ObservedDigest: gotPlan,
			Reason:         "sealed plan digest matches the frozen plan",
		})
	}
	wantInputs := strings.TrimSpace(sealed.InputsBundleDigest)
	gotInputs := strings.TrimSpace(sealed.ObservedInputsBundleDigest)
	if wantInputs != "" && gotInputs != "" && wantInputs != gotInputs {
		addOpsReplayBlocked(result, OpsApplyReplayCheck{
			Code:           "ops.replay.inputs_digest_mismatch",
			Status:         "blocked",
			Source:         strings.TrimSpace(sealed.InputsBundle),
			ExpectedDigest: wantInputs,
			ObservedDigest: gotInputs,
			Reason:         "sealed input bundle digest changed before replay",
		})
	} else if wantInputs != "" || gotInputs != "" {
		addOpsReplayCheck(result, OpsApplyReplayCheck{
			Code:           "ops.replay.inputs_digest",
			Status:         "passed",
			Source:         strings.TrimSpace(sealed.InputsBundle),
			ExpectedDigest: firstNonEmptyString(wantInputs, gotInputs),
			ObservedDigest: firstNonEmptyString(gotInputs, wantInputs),
			Reason:         "sealed input bundle digest matches the frozen plan",
		})
	}
	return finalizeOpsReplay(result)
}

func finalizeOpsReplay(result *OpsApplyReplayResult) *OpsApplyReplayResult {
	if result != nil && len(result.Blockers) > 0 {
		result.Status = "blocked"
	}
	return result
}

func addOpsReplayBlocked(result *OpsApplyReplayResult, check OpsApplyReplayCheck) {
	addOpsReplayCheck(result, check)
	result.Blockers = append(result.Blockers, OpsPlanBlocker{
		Code:     check.Code,
		Severity: "blocker",
		Source:   check.Source,
		Reason:   check.Reason,
	})
}

func addOpsReplayCheck(result *OpsApplyReplayResult, check OpsApplyReplayCheck) {
	if result == nil {
		return
	}
	check.Status = strings.TrimSpace(check.Status)
	if check.Status == "" {
		check.Status = "passed"
	}
	result.Checks = append(result.Checks, check)
	result.Summary.Checks++
	if check.Status == "blocked" {
		result.Summary.Blocked++
		return
	}
	result.Summary.Passed++
}
