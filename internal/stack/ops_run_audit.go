package stack

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	OpsRunAuditAPIVersion = "torque.dev/ops/run-audit/v1alpha1"
	OpsRunAuditKind       = "OpsRunAudit"
)

type OpsRunAudit struct {
	APIVersion      string                   `json:"apiVersion"`
	Kind            string                   `json:"kind"`
	Status          string                   `json:"status"`
	Verification    OpsRunAuditVerification  `json:"verification"`
	Summary         OpsRunAuditSummary       `json:"summary"`
	Replay          *OpsAuditGate            `json:"replay,omitempty"`
	Preflight       *OpsAuditGate            `json:"preflight,omitempty"`
	TargetGraph     *OpsAuditTargetGraph     `json:"targetGraph,omitempty"`
	FactEvidence    []OpsAuditFactEvidence   `json:"factEvidence,omitempty"`
	Locks           []OpsAuditLock           `json:"locks,omitempty"`
	PolicyDecisions []OpsAuditPolicyDecision `json:"policyDecisions,omitempty"`
	HostCommands    []OpsHostCommandAudit    `json:"hostCommands,omitempty"`
	Findings        []OpsAuditFinding        `json:"findings,omitempty"`
}

type OpsRunAuditVerification struct {
	Status string `json:"status"`
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
}

type OpsRunAuditSummary struct {
	SelectedTargets int  `json:"selectedTargets,omitempty"`
	FactEvidence    int  `json:"factEvidence,omitempty"`
	Locks           int  `json:"locks,omitempty"`
	PolicyDecisions int  `json:"policyDecisions,omitempty"`
	HostCommands    int  `json:"hostCommands,omitempty"`
	ReplayRequired  bool `json:"replayRequired,omitempty"`
}

type OpsAuditGate struct {
	Artifact     string           `json:"artifact"`
	Present      bool             `json:"present"`
	EventSeen    bool             `json:"eventSeen"`
	Status       string           `json:"status,omitempty"`
	Checks       int              `json:"checks,omitempty"`
	Passed       int              `json:"passed,omitempty"`
	Blocked      int              `json:"blocked,omitempty"`
	Blockers     []OpsPlanBlocker `json:"blockers,omitempty"`
	BlockedCodes []string         `json:"blockedCodes,omitempty"`
	PlanHash     string           `json:"planHash,omitempty"`
}

type OpsAuditTargetGraph struct {
	Name             string            `json:"name,omitempty"`
	Path             string            `json:"path,omitempty"`
	SourceDigest     string            `json:"sourceDigest,omitempty"`
	GraphDigest      string            `json:"graphDigest,omitempty"`
	MatchedTargetIDs []string          `json:"matchedTargetIds,omitempty"`
	Selector         map[string]string `json:"selector,omitempty"`
	Groups           []string          `json:"groups,omitempty"`
	TargetCount      int               `json:"targetCount,omitempty"`
	SelectedTargets  int               `json:"selectedTargets,omitempty"`
}

type OpsAuditFactEvidence struct {
	Source    string                 `json:"source,omitempty"`
	Kind      string                 `json:"kind,omitempty"`
	Digest    string                 `json:"digest,omitempty"`
	GraphName string                 `json:"graphName,omitempty"`
	Summary   OpsFactEvidenceSummary `json:"summary"`
	Freshness *OpsFactFreshnessInput `json:"freshness,omitempty"`
	Targets   []OpsFactTargetInput   `json:"targets,omitempty"`
}

type OpsAuditLock struct {
	Source    string `json:"source,omitempty"`
	Scope     string `json:"scope,omitempty"`
	TargetID  string `json:"targetId,omitempty"`
	Status    string `json:"status,omitempty"`
	Holder    string `json:"holder,omitempty"`
	Operation string `json:"operation,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type OpsAuditPolicyDecision struct {
	Source    string   `json:"source,omitempty"`
	Digest    string   `json:"digest,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Decision  string   `json:"decision,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Adapter   string   `json:"adapter,omitempty"`
	TargetID  string   `json:"targetId,omitempty"`
	Mutating  bool     `json:"mutating,omitempty"`
	Unsafe    bool     `json:"unsafe,omitempty"`
	Required  []string `json:"required,omitempty"`
}

type OpsHostCommandAudit struct {
	NodeID            string                    `json:"nodeId"`
	TargetID          string                    `json:"targetId,omitempty"`
	Status            string                    `json:"status,omitempty"`
	GuardMode         string                    `json:"guardMode,omitempty"`
	ObservePresent    bool                      `json:"observePresent"`
	PlanPresent       bool                      `json:"planPresent"`
	ExecutePresent    bool                      `json:"executePresent"`
	VerifyPresent     bool                      `json:"verifyPresent"`
	ObserveStatus     string                    `json:"observeStatus,omitempty"`
	PlanStatus        string                    `json:"planStatus,omitempty"`
	ExecuteStatus     string                    `json:"executeStatus,omitempty"`
	VerifyStatus      string                    `json:"verifyStatus,omitempty"`
	CommandDigest     string                    `json:"commandDigest,omitempty"`
	Redaction         hostCommandRedactionProof `json:"redaction,omitempty"`
	RedactionVerified bool                      `json:"redactionVerified"`
}

type OpsAuditFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity,omitempty"`
	NodeID   string `json:"nodeId,omitempty"`
	Artifact string `json:"artifact,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	Reason   string `json:"reason"`
}

func BuildOpsRunAudit(rp *RunPlan, artifacts []RunArtifact, events []RunEvent, runStatus string, verify bool) *OpsRunAudit {
	if !runPlanHasOps(rp) && !hasOpsArtifacts(artifacts) && !hasOpsEvents(events) {
		return nil
	}
	a := &OpsRunAudit{
		APIVersion: OpsRunAuditAPIVersion,
		Kind:       OpsRunAuditKind,
		Status:     "reported",
		Verification: OpsRunAuditVerification{
			Status: "skipped",
		},
	}
	if verify {
		a.Verification.Status = "passed"
	}
	byName := opsArtifactsByName(artifacts)
	eventSeen := opsEventSet(events)

	if rp != nil && rp.Ops != nil {
		a.Summary.SelectedTargets = rp.Ops.Summary.SelectedTargets
		a.Summary.FactEvidence = len(rp.Ops.FactEvidence)
		a.Summary.Locks = len(rp.Ops.Locks)
		a.Summary.PolicyDecisions = len(rp.Ops.PolicyDecisions)
		a.TargetGraph = buildOpsAuditTargetGraph(rp.Ops.TargetGraph)
		a.FactEvidence = buildOpsAuditFactEvidence(rp.Ops.FactEvidence)
		a.Locks = buildOpsAuditLocks(rp.Ops.Locks)
		a.PolicyDecisions = buildOpsAuditPolicyDecisions(rp.Ops.PolicyDecisions)
	}

	replayRequired := rp != nil && rp.Sealed != nil
	if _, ok := byName[opsArtifactKey("", "ops-replay.json")]; ok {
		replayRequired = true
	}
	if eventSeen[string(OpsReplay)] {
		replayRequired = true
	}
	a.Summary.ReplayRequired = replayRequired
	a.Replay = buildOpsReplayGate(byName, eventSeen)
	a.Preflight = buildOpsPreflightGate(byName, eventSeen)

	if verify {
		replayBlocked := a.Replay != nil && a.Replay.Status == "blocked"
		preflightExpected := runPlanHasOps(rp) && !replayBlocked
		if preflightExpected && a.Preflight == nil {
			a.addFinding("ops.preflight.missing", "", "ops-preflight.json", "", "ops run is missing ops-preflight.json")
		}
		if replayRequired && a.Replay == nil {
			a.addFinding("ops.replay.missing", "", "ops-replay.json", "", "sealed ops replay is missing ops-replay.json")
		}
		if replayRequired && !eventSeen[string(OpsReplay)] {
			a.addFinding("ops.replay.event_missing", "", "ops-replay.json", "", "sealed ops replay is missing OPS_REPLAY event")
		}
		if a.Replay != nil && a.Replay.Status == "unreadable" {
			a.addFinding("ops.replay.unreadable", "", "ops-replay.json", "", "ops-replay.json is not valid replay evidence")
		}
		if a.Preflight != nil && a.Preflight.Status == "unreadable" {
			a.addFinding("ops.preflight.unreadable", "", "ops-preflight.json", "", "ops-preflight.json is not valid preflight evidence")
		}
		if preflightExpected && !eventSeen[string(OpsPreflight)] {
			a.addFinding("ops.preflight.event_missing", "", "ops-preflight.json", "", "ops run is missing OPS_PREFLIGHT event")
		}
		strictPlanInputs := strings.EqualFold(strings.TrimSpace(runStatus), "succeeded") || (a.Preflight != nil && a.Preflight.Status == "eligible")
		if strings.EqualFold(strings.TrimSpace(runStatus), "succeeded") {
			if a.Replay != nil && a.Replay.Status != "eligible" {
				a.addFinding("ops.replay.not_eligible", "", "ops-replay.json", "", "succeeded ops run did not have eligible replay")
			}
			if a.Preflight != nil && a.Preflight.Status != "eligible" {
				a.addFinding("ops.preflight.not_eligible", "", "ops-preflight.json", "", "succeeded ops run did not have eligible preflight")
			}
		}
		verifyOpsPlanInputs(a, rp, strictPlanInputs)
	}

	a.HostCommands = buildOpsHostCommandAudits(rp, artifacts, byName, verify, a)
	a.Summary.HostCommands = len(a.HostCommands)
	finalizeOpsRunAudit(a)
	return a
}

func runPlanHasOps(rp *RunPlan) bool {
	return rp != nil && rp.Ops != nil
}

func (a *OpsRunAudit) addFinding(code string, nodeID string, artifact string, targetID string, reason string) {
	if a == nil {
		return
	}
	a.Findings = append(a.Findings, OpsAuditFinding{
		Code:     strings.TrimSpace(code),
		Severity: "blocker",
		NodeID:   strings.TrimSpace(nodeID),
		Artifact: strings.TrimSpace(artifact),
		TargetID: strings.TrimSpace(targetID),
		Reason:   strings.TrimSpace(reason),
	})
}

func finalizeOpsRunAudit(a *OpsRunAudit) {
	if a == nil {
		return
	}
	a.Verification.Failed = len(a.Findings)
	if a.Verification.Status == "skipped" {
		a.Status = "reported"
		return
	}
	if len(a.Findings) > 0 {
		a.Verification.Status = "failed"
		a.Status = "failed"
		return
	}
	a.Verification.Status = "passed"
	a.Status = "passed"
	a.Verification.Passed = 1
}

func hasOpsArtifacts(artifacts []RunArtifact) bool {
	for _, artifact := range artifacts {
		name := strings.TrimSpace(artifact.Name)
		if strings.HasPrefix(name, "ops-") || strings.HasPrefix(name, "host-command") {
			return true
		}
	}
	return false
}

func hasOpsEvents(events []RunEvent) bool {
	for _, event := range events {
		switch event.Type {
		case string(OpsReplay), string(OpsPreflight):
			return true
		}
	}
	return false
}

func opsArtifactKey(nodeID string, name string) string {
	return strings.TrimSpace(nodeID) + "\x00" + strings.TrimSpace(name)
}

func opsArtifactsByName(artifacts []RunArtifact) map[string]RunArtifact {
	out := make(map[string]RunArtifact, len(artifacts))
	for _, artifact := range artifacts {
		out[opsArtifactKey(artifact.NodeID, artifact.Name)] = artifact
	}
	return out
}

func opsEventSet(events []RunEvent) map[string]bool {
	out := map[string]bool{}
	for _, event := range events {
		if strings.TrimSpace(event.Type) != "" {
			out[strings.TrimSpace(event.Type)] = true
		}
	}
	return out
}

func buildOpsReplayGate(artifacts map[string]RunArtifact, eventSeen map[string]bool) *OpsAuditGate {
	artifact, ok := artifacts[opsArtifactKey("", "ops-replay.json")]
	if !ok {
		return nil
	}
	var result OpsApplyReplayResult
	gate := &OpsAuditGate{Artifact: "ops-replay.json", Present: true, EventSeen: eventSeen[string(OpsReplay)]}
	if err := json.Unmarshal([]byte(artifact.Body), &result); err != nil {
		gate.Status = "unreadable"
		return gate
	}
	gate.Status = strings.TrimSpace(result.Status)
	gate.Checks = result.Summary.Checks
	gate.Passed = result.Summary.Passed
	gate.Blocked = result.Summary.Blocked
	gate.Blockers = append([]OpsPlanBlocker(nil), result.Blockers...)
	gate.BlockedCodes = blockerCodes(result.Blockers)
	gate.PlanHash = strings.TrimSpace(result.Summary.PlanHash)
	return gate
}

func buildOpsPreflightGate(artifacts map[string]RunArtifact, eventSeen map[string]bool) *OpsAuditGate {
	artifact, ok := artifacts[opsArtifactKey("", "ops-preflight.json")]
	if !ok {
		return nil
	}
	var result OpsApplyPreflightResult
	gate := &OpsAuditGate{Artifact: "ops-preflight.json", Present: true, EventSeen: eventSeen[string(OpsPreflight)]}
	if err := json.Unmarshal([]byte(artifact.Body), &result); err != nil {
		gate.Status = "unreadable"
		return gate
	}
	gate.Status = strings.TrimSpace(result.Status)
	gate.Checks = result.Summary.Checks
	gate.Passed = result.Summary.Passed
	gate.Blocked = result.Summary.Blocked
	gate.Blockers = append([]OpsPlanBlocker(nil), result.Blockers...)
	gate.BlockedCodes = blockerCodes(result.Blockers)
	return gate
}

func blockerCodes(blockers []OpsPlanBlocker) []string {
	var out []string
	for _, blocker := range blockers {
		if strings.TrimSpace(blocker.Code) != "" {
			out = append(out, strings.TrimSpace(blocker.Code))
		}
	}
	sort.Strings(out)
	return out
}

func buildOpsAuditTargetGraph(input *OpsTargetGraphInput) *OpsAuditTargetGraph {
	if input == nil {
		return nil
	}
	return &OpsAuditTargetGraph{
		Name:             strings.TrimSpace(input.Name),
		Path:             strings.TrimSpace(input.Path),
		SourceDigest:     strings.TrimSpace(input.SourceDigest),
		GraphDigest:      strings.TrimSpace(input.GraphDigest),
		MatchedTargetIDs: append([]string(nil), input.Selection.MatchedTargetIDs...),
		Selector:         copyStringMapAudit(input.Selection.Selector),
		Groups:           append([]string(nil), input.Selection.Groups...),
		TargetCount:      input.Summary.TargetCount,
		SelectedTargets:  len(input.Selection.MatchedTargetIDs),
	}
}

func buildOpsAuditFactEvidence(inputs []OpsFactEvidenceInput) []OpsAuditFactEvidence {
	out := make([]OpsAuditFactEvidence, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, OpsAuditFactEvidence{
			Source:    strings.TrimSpace(input.Source),
			Kind:      strings.TrimSpace(input.Kind),
			Digest:    strings.TrimSpace(input.Digest),
			GraphName: strings.TrimSpace(input.GraphName),
			Summary:   input.Summary,
			Freshness: input.Freshness,
			Targets:   append([]OpsFactTargetInput(nil), input.Targets...),
		})
	}
	return out
}

func buildOpsAuditLocks(inputs []OpsLockInput) []OpsAuditLock {
	out := make([]OpsAuditLock, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, OpsAuditLock{
			Source:    strings.TrimSpace(input.Source),
			Scope:     strings.TrimSpace(input.Scope),
			TargetID:  strings.TrimSpace(input.TargetID),
			Status:    strings.TrimSpace(input.Status),
			Holder:    strings.TrimSpace(input.Holder),
			Operation: strings.TrimSpace(input.Operation),
			ExpiresAt: strings.TrimSpace(input.ExpiresAt),
		})
	}
	return out
}

func buildOpsAuditPolicyDecisions(inputs []OpsPolicyDecisionInput) []OpsAuditPolicyDecision {
	out := make([]OpsAuditPolicyDecision, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, OpsAuditPolicyDecision{
			Source:    strings.TrimSpace(input.Source),
			Digest:    strings.TrimSpace(input.Digest),
			Mode:      strings.TrimSpace(input.Mode),
			Decision:  strings.TrimSpace(input.Decision),
			Reason:    strings.TrimSpace(input.Reason),
			Operation: strings.TrimSpace(input.Operation),
			Adapter:   strings.TrimSpace(input.Adapter),
			TargetID:  strings.TrimSpace(input.TargetID),
			Mutating:  input.Mutating,
			Unsafe:    input.Unsafe,
			Required:  append([]string(nil), input.Required...),
		})
	}
	return out
}

func verifyOpsPlanInputs(a *OpsRunAudit, rp *RunPlan, strict bool) {
	if a == nil || rp == nil || rp.Ops == nil {
		return
	}
	if !strict {
		return
	}
	if rp.Ops.TargetGraph == nil {
		a.addFinding("ops.targetgraph.missing", "", "", "", "ops plan is missing TargetGraph input")
	} else {
		if strings.TrimSpace(rp.Ops.TargetGraph.SourceDigest) == "" {
			a.addFinding("ops.targetgraph.digest_missing", "", "", "", "TargetGraph source digest is missing")
		}
		if len(rp.Ops.TargetGraph.Selection.MatchedTargetIDs) == 0 {
			a.addFinding("ops.targetgraph.selection_empty", "", "", "", "TargetGraph selection is empty")
		}
	}
	if len(rp.Ops.FactEvidence) == 0 {
		a.addFinding("ops.facts.missing", "", "", "", "ops plan is missing fact evidence")
	}
	for _, facts := range rp.Ops.FactEvidence {
		if strings.TrimSpace(facts.Source) == "" || strings.TrimSpace(facts.Digest) == "" {
			a.addFinding("ops.facts.digest_missing", "", "", "", "fact evidence source or digest is missing")
		}
	}
	if len(rp.Ops.Locks) == 0 {
		a.addFinding("ops.lock.missing", "", "", "", "ops plan is missing lock evidence")
	}
	if len(rp.Ops.PolicyDecisions) == 0 {
		a.addFinding("ops.policy.missing", "", "", "", "ops plan is missing policy decision evidence")
	}
	for _, decision := range rp.Ops.PolicyDecisions {
		if strings.TrimSpace(decision.Source) == "" || strings.TrimSpace(decision.Digest) == "" {
			a.addFinding("ops.policy.digest_missing", "", "", strings.TrimSpace(decision.TargetID), "policy source or digest is missing")
		}
	}
}

func buildOpsHostCommandAudits(rp *RunPlan, artifacts []RunArtifact, byName map[string]RunArtifact, verify bool, audit *OpsRunAudit) []OpsHostCommandAudit {
	nodeIDs := map[string]struct{}{}
	if rp != nil {
		for _, node := range rp.Nodes {
			if node != nil && normalizeNodeKind(node.Kind) == NodeKindHostCommandRun {
				nodeIDs[strings.TrimSpace(node.ID)] = struct{}{}
			}
		}
	}
	for _, artifact := range artifacts {
		if strings.HasPrefix(strings.TrimSpace(artifact.Name), "host-command") {
			nodeIDs[strings.TrimSpace(artifact.NodeID)] = struct{}{}
		}
	}
	var ids []string
	for id := range nodeIDs {
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]OpsHostCommandAudit, 0, len(ids))
	preflightBlocked := audit != nil && audit.Preflight != nil && audit.Preflight.Status == "blocked"
	for _, nodeID := range ids {
		item := buildOpsHostCommandAudit(nodeID, byName)
		if verify && !preflightBlocked {
			verifyOpsHostCommandAudit(audit, item, byName)
		}
		if item.ObservePresent || item.PlanPresent || item.ExecutePresent || item.VerifyPresent || !preflightBlocked {
			out = append(out, item)
		}
	}
	return out
}

func buildOpsHostCommandAudit(nodeID string, byName map[string]RunArtifact) OpsHostCommandAudit {
	out := OpsHostCommandAudit{NodeID: strings.TrimSpace(nodeID)}
	var observe hostCommandObserveReceipt
	if decodeOpsArtifact(byName, nodeID, "host-command-observe.json", &observe) == nil {
		out.ObservePresent = true
		out.ObserveStatus = strings.TrimSpace(observe.Status)
		out.TargetID = firstNonEmptyString(out.TargetID, observe.TargetID, observe.SelectedTargetID)
		out.GuardMode = firstNonEmptyString(out.GuardMode, observe.GuardMode)
	}
	var plan hostCommandPlanReceipt
	if decodeOpsArtifact(byName, nodeID, "host-command-plan.json", &plan) == nil {
		out.PlanPresent = true
		out.PlanStatus = strings.TrimSpace(plan.Status)
		out.TargetID = firstNonEmptyString(out.TargetID, plan.TargetID)
		out.GuardMode = firstNonEmptyString(out.GuardMode, plan.GuardMode)
		out.CommandDigest = strings.TrimSpace(plan.CommandDigest)
	}
	var execute transport.OperationResult
	if decodeOpsArtifact(byName, nodeID, "host-command-execute.json", &execute) == nil {
		out.ExecutePresent = true
		out.ExecuteStatus = strings.TrimSpace(execute.Status)
	}
	var verify hostCommandVerifyReceipt
	if decodeOpsArtifact(byName, nodeID, "host-command-verify.json", &verify) == nil {
		out.VerifyPresent = true
		out.VerifyStatus = strings.TrimSpace(verify.Status)
		out.TargetID = firstNonEmptyString(out.TargetID, verify.TargetID)
		out.Redaction = verify.Redaction
		out.RedactionVerified = verify.Redaction.NoSecretRefs && verify.Redaction.NoSensitiveKV && verify.Redaction.NoAuthorizationBearer
	}
	out.Status = firstNonEmptyString(out.VerifyStatus, out.ExecuteStatus, out.PlanStatus, out.ObserveStatus)
	return out
}

func verifyOpsHostCommandAudit(audit *OpsRunAudit, item OpsHostCommandAudit, byName map[string]RunArtifact) {
	if audit == nil {
		return
	}
	required := []struct {
		ok   bool
		name string
		code string
	}{
		{item.ObservePresent, "host-command-observe.json", "ops.host_command.observe_missing"},
		{item.PlanPresent, "host-command-plan.json", "ops.host_command.plan_missing"},
		{item.VerifyPresent, "host-command-verify.json", "ops.host_command.verify_missing"},
	}
	for _, req := range required {
		if !req.ok {
			audit.addFinding(req.code, item.NodeID, req.name, item.TargetID, fmt.Sprintf("%s is missing", req.name))
		}
	}
	if item.PlanStatus != "blocked" && item.PlanStatus != "skipped" && !item.ExecutePresent {
		audit.addFinding("ops.host_command.execute_missing", item.NodeID, "host-command-execute.json", item.TargetID, "host-command-execute.json is missing")
	}
	if item.GuardMode != "" && item.GuardMode != "ops" {
		audit.addFinding("ops.host_command.guard_mode", item.NodeID, "host-command-plan.json", item.TargetID, "host.command.run did not use ops guard mode")
	}
	if item.VerifyPresent && !item.RedactionVerified {
		audit.addFinding("ops.host_command.redaction_proof_failed", item.NodeID, "host-command-verify.json", item.TargetID, "host.command.run redaction proof did not pass")
	}
	for _, name := range []string{"host-command-observe.json", "host-command-plan.json", "host-command-execute.json", "host-command-verify.json", "host-command.json"} {
		artifact, ok := byName[opsArtifactKey(item.NodeID, name)]
		if !ok {
			continue
		}
		if opsArtifactHasSensitiveMaterial(artifact.Body) {
			audit.addFinding("ops.host_command.redaction_leak", item.NodeID, name, item.TargetID, "host.command.run artifact contains unredacted sensitive output")
		}
	}
}

func decodeOpsArtifact(byName map[string]RunArtifact, nodeID string, name string, dst any) error {
	artifact, ok := byName[opsArtifactKey(nodeID, name)]
	if !ok {
		return fmt.Errorf("missing artifact %s", name)
	}
	if err := json.Unmarshal([]byte(artifact.Body), dst); err != nil {
		return err
	}
	return nil
}

func opsArtifactHasSensitiveMaterial(body string) bool {
	value := strings.ToLower(body)
	if strings.Contains(value, "secret://") || strings.Contains(value, "authorization: bearer ") {
		return true
	}
	return hostCommandHasRawSensitiveKV(value)
}

func copyStringMapAudit(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func OpsAuditVerificationFailed(a *RunAudit) bool {
	return a != nil && a.Ops != nil && a.Ops.Verification.Status == "failed"
}
