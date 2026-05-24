package stack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/ops/locks"
)

const (
	OpsApplyPreflightAPIVersion = "torque.dev/ops/apply-preflight/v1alpha1"
	OpsApplyPreflightKind       = "OpsApplyPreflight"
)

type OpsApplyPreflightResult struct {
	APIVersion  string                   `json:"apiVersion"`
	Kind        string                   `json:"kind"`
	Status      string                   `json:"status"`
	GeneratedAt string                   `json:"generatedAt"`
	Summary     OpsApplyPreflightSummary `json:"summary"`
	Checks      []OpsApplyPreflightCheck `json:"checks,omitempty"`
	Blockers    []OpsPlanBlocker         `json:"blockers,omitempty"`
}

type OpsApplyPreflightSummary struct {
	Checks          int `json:"checks"`
	Passed          int `json:"passed"`
	Blocked         int `json:"blocked"`
	Skipped         int `json:"skipped"`
	SelectedTargets int `json:"selectedTargets,omitempty"`
	FactEvidence    int `json:"factEvidence,omitempty"`
	Locks           int `json:"locks,omitempty"`
	PolicyDecisions int `json:"policyDecisions,omitempty"`
}

type OpsApplyPreflightCheck struct {
	Code           string `json:"code"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
	Source         string `json:"source,omitempty"`
	TargetID       string `json:"targetId,omitempty"`
	Scope          string `json:"scope,omitempty"`
	ExpectedDigest string `json:"expectedDigest,omitempty"`
	ObservedDigest string `json:"observedDigest,omitempty"`
}

func EvaluateOpsApplyPreflight(ctx context.Context, p *Plan) *OpsApplyPreflightResult {
	if p == nil || p.Ops == nil {
		return nil
	}
	selectedTargets := p.Ops.Summary.SelectedTargets
	if p.Ops.TargetGraph != nil && len(p.Ops.TargetGraph.Selection.MatchedTargetIDs) > selectedTargets {
		selectedTargets = len(p.Ops.TargetGraph.Selection.MatchedTargetIDs)
	}
	result := &OpsApplyPreflightResult{
		APIVersion:  OpsApplyPreflightAPIVersion,
		Kind:        OpsApplyPreflightKind,
		Status:      "eligible",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Summary: OpsApplyPreflightSummary{
			SelectedTargets: selectedTargets,
			FactEvidence:    len(p.Ops.FactEvidence),
			Locks:           len(p.Ops.Locks),
			PolicyDecisions: len(p.Ops.PolicyDecisions),
		},
	}
	if ctx == nil {
		ctx = context.Background()
	}

	evaluateOpsTargetGraphPreflight(ctx, result, p.Ops.TargetGraph)
	evaluateOpsFactPreflight(result, p.Ops)
	evaluateOpsLockPreflight(ctx, result, p.Ops)
	evaluateOpsPolicyPreflight(result, p.Ops)

	if len(result.Blockers) > 0 {
		result.Status = "blocked"
	}
	return result
}

func evaluateOpsTargetGraphPreflight(ctx context.Context, result *OpsApplyPreflightResult, input *OpsTargetGraphInput) {
	if input == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
			Code:   "ops.targetgraph.context",
			Status: "blocked",
			Reason: err.Error(),
			Source: input.Path,
		})
		return
	}
	path := strings.TrimSpace(input.Path)
	expected := strings.TrimSpace(input.SourceDigest)
	if path == "" || expected == "" {
		addOpsPreflightCheck(result, OpsApplyPreflightCheck{
			Code:   "ops.targetgraph.digest",
			Status: "skipped",
			Reason: "target graph path or source digest was not recorded in the plan",
			Source: path,
		})
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
			Code:           "ops.targetgraph.unreadable",
			Status:         "blocked",
			Reason:         fmt.Sprintf("read target graph: %v", err),
			Source:         path,
			ExpectedDigest: expected,
		})
		return
	}
	observed := opsApplyPreflightDigest(raw)
	if observed != expected {
		addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
			Code:           "ops.targetgraph.changed",
			Status:         "blocked",
			Reason:         "TargetGraph source digest changed after planning",
			Source:         path,
			ExpectedDigest: expected,
			ObservedDigest: observed,
		})
		return
	}
	addOpsPreflightCheck(result, OpsApplyPreflightCheck{
		Code:           "ops.targetgraph.digest",
		Status:         "passed",
		Reason:         "TargetGraph source digest matches the plan",
		Source:         path,
		ExpectedDigest: expected,
		ObservedDigest: observed,
	})
}

func evaluateOpsFactPreflight(result *OpsApplyPreflightResult, inputs *OpsPlanInputs) {
	if inputs == nil {
		return
	}
	if result.Summary.SelectedTargets > 0 && len(inputs.FactEvidence) == 0 {
		addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
			Code:   "ops.facts.missing",
			Status: "blocked",
			Reason: "selected ops targets have no fact evidence attached to the plan",
		})
		return
	}
	for _, facts := range inputs.FactEvidence {
		source := strings.TrimSpace(facts.Source)
		expected := strings.TrimSpace(facts.Digest)
		if source == "" || expected == "" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:           "ops.facts.digest_unverifiable",
				Status:         "blocked",
				Source:         source,
				ExpectedDigest: expected,
				Reason:         "fact evidence source or digest was not recorded in the plan",
			})
			continue
		}
		observed, err := ComputeOpsFactEvidenceDigest(source)
		if err != nil {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:           "ops.facts.unreadable",
				Status:         "blocked",
				Source:         source,
				ExpectedDigest: expected,
				Reason:         fmt.Sprintf("read fact evidence: %v", err),
			})
			continue
		}
		if observed != expected {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:           "ops.facts.changed",
				Status:         "blocked",
				Source:         source,
				ExpectedDigest: expected,
				ObservedDigest: observed,
				Reason:         "fact evidence digest changed after planning",
			})
			continue
		}
		addOpsPreflightCheck(result, OpsApplyPreflightCheck{
			Code:           "ops.facts.digest",
			Status:         "passed",
			Source:         source,
			ExpectedDigest: expected,
			ObservedDigest: observed,
			Reason:         "fact evidence digest matches the plan",
		})
		if facts.Summary.Blocked > 0 || facts.Summary.Failed > 0 || facts.Summary.Skipped > 0 {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:   "ops.facts.incomplete",
				Status: "blocked",
				Source: facts.Source,
				Reason: fmt.Sprintf("facts evidence has %d blocked, %d failed, and %d skipped target(s)", facts.Summary.Blocked, facts.Summary.Failed, facts.Summary.Skipped),
			})
			continue
		}
		staleTarget := ""
		for _, target := range facts.Targets {
			switch strings.ToLower(strings.TrimSpace(target.Status)) {
			case "stale", "blocked", "failed", "skipped":
				staleTarget = target.TargetID
			}
			if staleTarget != "" {
				break
			}
		}
		if staleTarget != "" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.facts.stale",
				Status:   "blocked",
				Source:   facts.Source,
				TargetID: staleTarget,
				Reason:   "fact evidence contains a stale or incomplete target observation",
			})
			continue
		}
		addOpsPreflightCheck(result, OpsApplyPreflightCheck{
			Code:   "ops.facts.evidence",
			Status: "passed",
			Source: facts.Source,
			Reason: "fact evidence is complete",
		})
	}
}

func evaluateOpsLockPreflight(ctx context.Context, result *OpsApplyPreflightResult, inputs *OpsPlanInputs) {
	if inputs == nil {
		return
	}
	if result.Summary.SelectedTargets > 0 && len(inputs.Locks) == 0 {
		addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
			Code:   "ops.lock.missing",
			Status: "blocked",
			Reason: "selected ops targets have no lock evidence attached to the plan",
		})
		return
	}
	for _, input := range inputs.Locks {
		if err := ctx.Err(); err != nil {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:   "ops.lock.context",
				Status: "blocked",
				Reason: err.Error(),
				Source: input.Source,
				Scope:  input.Scope,
			})
			continue
		}
		source := strings.TrimSpace(input.Source)
		scope := strings.TrimSpace(input.Scope)
		if source == "" || scope == "" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:   "ops.lock.unverifiable",
				Status: "blocked",
				Reason: "lock source directory or scope was not recorded in the plan",
				Source: source,
				Scope:  scope,
			})
			continue
		}
		record, found, err := locks.FileStore{Dir: source}.Inspect(scope)
		if err != nil {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:   "ops.lock.read_failed",
				Status: "blocked",
				Reason: fmt.Sprintf("read lock: %v", err),
				Source: source,
				Scope:  scope,
			})
			continue
		}
		if !found {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.lock.missing",
				Status:   "blocked",
				Reason:   "target lock is missing",
				Source:   source,
				TargetID: input.TargetID,
				Scope:    scope,
			})
			continue
		}
		status := strings.ToLower(strings.TrimSpace(record.Status))
		if status == "stale" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.lock.stale",
				Status:   "blocked",
				Reason:   "target lock is stale",
				Source:   source,
				TargetID: firstNonEmptyString(record.TargetID, input.TargetID),
				Scope:    scope,
			})
			continue
		}
		if status != "held" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.lock.not_held",
				Status:   "blocked",
				Reason:   fmt.Sprintf("target lock status is %q", record.Status),
				Source:   source,
				TargetID: firstNonEmptyString(record.TargetID, input.TargetID),
				Scope:    scope,
			})
			continue
		}
		expectedHolder := strings.TrimSpace(input.Holder)
		if expectedHolder == "" || strings.TrimSpace(record.Holder) != expectedHolder {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.lock.holder_mismatch",
				Status:   "blocked",
				Reason:   fmt.Sprintf("target lock is held by %s, not planned holder %s", firstNonEmptyString(record.Holder, "unknown"), firstNonEmptyString(expectedHolder, "unknown")),
				Source:   source,
				TargetID: firstNonEmptyString(record.TargetID, input.TargetID),
				Scope:    scope,
			})
			continue
		}
		if input.TargetID != "" && record.TargetID != "" && strings.TrimSpace(record.TargetID) != strings.TrimSpace(input.TargetID) {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.lock.target_mismatch",
				Status:   "blocked",
				Reason:   fmt.Sprintf("target lock is for %s, not %s", record.TargetID, input.TargetID),
				Source:   source,
				TargetID: input.TargetID,
				Scope:    scope,
			})
			continue
		}
		addOpsPreflightCheck(result, OpsApplyPreflightCheck{
			Code:     "ops.lock.held",
			Status:   "passed",
			Reason:   "target lock matches the planned holder",
			Source:   source,
			TargetID: firstNonEmptyString(record.TargetID, input.TargetID),
			Scope:    scope,
		})
	}
}

func evaluateOpsPolicyPreflight(result *OpsApplyPreflightResult, inputs *OpsPlanInputs) {
	if inputs == nil {
		return
	}
	if result.Summary.SelectedTargets > 0 && len(inputs.PolicyDecisions) == 0 {
		addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
			Code:   "ops.policy.missing",
			Status: "blocked",
			Reason: "selected ops targets have no mutation policy decision attached to the plan",
		})
		return
	}
	for _, decision := range inputs.PolicyDecisions {
		source := strings.TrimSpace(decision.Source)
		expected := strings.TrimSpace(decision.Digest)
		if source == "" || expected == "" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:           "ops.policy.digest_unverifiable",
				Status:         "blocked",
				Source:         source,
				TargetID:       decision.TargetID,
				ExpectedDigest: expected,
				Reason:         "policy decision source or digest was not recorded in the plan",
			})
			continue
		}
		raw, err := os.ReadFile(source)
		if err != nil {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:           "ops.policy.unreadable",
				Status:         "blocked",
				Source:         source,
				TargetID:       decision.TargetID,
				ExpectedDigest: expected,
				Reason:         fmt.Sprintf("read policy decision: %v", err),
			})
			continue
		}
		observed := opsApplyPreflightDigest(raw)
		if observed != expected {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:           "ops.policy.changed",
				Status:         "blocked",
				Source:         source,
				TargetID:       decision.TargetID,
				ExpectedDigest: expected,
				ObservedDigest: observed,
				Reason:         "policy decision digest changed after planning",
			})
			continue
		}
		addOpsPreflightCheck(result, OpsApplyPreflightCheck{
			Code:           "ops.policy.digest",
			Status:         "passed",
			Source:         source,
			TargetID:       decision.TargetID,
			ExpectedDigest: expected,
			ObservedDigest: observed,
			Reason:         "policy decision digest matches the plan",
		})
		if strings.TrimSpace(decision.Decision) != "allow" {
			addOpsPreflightBlocked(result, OpsApplyPreflightCheck{
				Code:     "ops.policy." + firstNonEmptyString(decision.Decision, "unknown"),
				Status:   "blocked",
				Reason:   firstNonEmptyString(decision.Reason, "policy did not allow mutation"),
				Source:   decision.Source,
				TargetID: decision.TargetID,
			})
			continue
		}
		addOpsPreflightCheck(result, OpsApplyPreflightCheck{
			Code:     "ops.policy.allow",
			Status:   "passed",
			Reason:   firstNonEmptyString(decision.Reason, "policy allowed mutation"),
			Source:   decision.Source,
			TargetID: decision.TargetID,
		})
	}
}

func addOpsPreflightBlocked(result *OpsApplyPreflightResult, check OpsApplyPreflightCheck) {
	addOpsPreflightCheck(result, check)
	result.Blockers = append(result.Blockers, OpsPlanBlocker{
		Code:     check.Code,
		Severity: "blocker",
		Source:   check.Source,
		TargetID: check.TargetID,
		Scope:    check.Scope,
		Reason:   check.Reason,
	})
}

func addOpsPreflightCheck(result *OpsApplyPreflightResult, check OpsApplyPreflightCheck) {
	if result == nil {
		return
	}
	check.Status = strings.TrimSpace(check.Status)
	if check.Status == "" {
		check.Status = "passed"
	}
	result.Checks = append(result.Checks, check)
	result.Summary.Checks++
	switch check.Status {
	case "blocked":
		result.Summary.Blocked++
	case "skipped":
		result.Summary.Skipped++
	default:
		result.Summary.Passed++
	}
}

func opsApplyPreflightDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
