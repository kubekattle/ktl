package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	opsmysql "github.com/ingresslabs/torque/internal/ops/mysql"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	defaultMySQLReplicationStableAttempts = 60
	defaultMySQLReplicationStableInterval = 2 * time.Second
	defaultMySQLReplicationTimeout        = 10 * time.Minute
)

type mysqlReplicationObserveReceipt struct {
	APIVersion              string   `json:"apiVersion"`
	Kind                    string   `json:"kind"`
	NodeID                  string   `json:"nodeId"`
	NodeKind                string   `json:"nodeKind"`
	TargetID                string   `json:"targetId,omitempty"`
	Phase                   string   `json:"phase"`
	Status                  string   `json:"status"`
	GuardMode               string   `json:"guardMode"`
	SelectedTargetID        string   `json:"selectedTargetId,omitempty"`
	SelectedTargets         []string `json:"selectedTargets,omitempty"`
	TargetDigest            string   `json:"targetDigest,omitempty"`
	MySQLNodes              int      `json:"mysqlNodes"`
	ExpectedClusterSize     int      `json:"expectedClusterSize"`
	ExpectedReplicatedNodes int      `json:"expectedReplicatedNodes"`
	ProbeIDDigest           string   `json:"probeIdDigest,omitempty"`
	ObservedAt              string   `json:"observedAt"`
}

type mysqlReplicationPlanReceipt struct {
	APIVersion              string   `json:"apiVersion"`
	Kind                    string   `json:"kind"`
	NodeID                  string   `json:"nodeId"`
	NodeKind                string   `json:"nodeKind"`
	TargetID                string   `json:"targetId,omitempty"`
	Phase                   string   `json:"phase"`
	Status                  string   `json:"status"`
	Reason                  string   `json:"reason,omitempty"`
	GuardMode               string   `json:"guardMode"`
	AdapterKind             string   `json:"adapterKind,omitempty"`
	Transport               string   `json:"transport,omitempty"`
	Operation               string   `json:"operation"`
	ResourceDigest          string   `json:"resourceDigest,omitempty"`
	CommandDigest           string   `json:"commandDigest,omitempty"`
	MySQLNodes              int      `json:"mysqlNodes"`
	ExpectedClusterSize     int      `json:"expectedClusterSize"`
	ExpectedReplicatedNodes int      `json:"expectedReplicatedNodes"`
	SelectedTargets         []string `json:"selectedTargets,omitempty"`
	LockScopes              []string `json:"lockScopes,omitempty"`
	PolicySources           []string `json:"policySources,omitempty"`
	PlannedAt               string   `json:"plannedAt"`
}

type mysqlReplicationVerifyEvidence struct {
	APIVersion              string                               `json:"apiVersion"`
	Kind                    string                               `json:"kind"`
	NodeID                  string                               `json:"nodeId"`
	NodeKind                string                               `json:"nodeKind"`
	Status                  string                               `json:"status"`
	Message                 string                               `json:"message"`
	TargetID                string                               `json:"targetId,omitempty"`
	TargetDigest            string                               `json:"targetDigest,omitempty"`
	ExpectedClusterSize     int                                  `json:"expectedClusterSize"`
	ExpectedReplicatedNodes int                                  `json:"expectedReplicatedNodes"`
	ReplicatedNodes         int                                  `json:"replicatedNodes"`
	Attempt                 int                                  `json:"attempt,omitempty"`
	StableAttempts          int                                  `json:"stableAttempts"`
	StableInterval          string                               `json:"stableInterval"`
	InsertProbe             bool                                 `json:"insertProbe"`
	RequireSynced           bool                                 `json:"requireSynced"`
	ProbeID                 string                               `json:"probeId,omitempty"`
	ProbeTable              string                               `json:"probeTable,omitempty"`
	StatusPathDigest        string                               `json:"statusPathDigest,omitempty"`
	Nodes                   []mysqlReplicationVerifyNodeEvidence `json:"nodes,omitempty"`
	Receipt                 transport.OperationResult            `json:"receipt,omitempty"`
	VerifiedAt              string                               `json:"verifiedAt"`
}

type mysqlReplicationVerifyNodeEvidence struct {
	ID          string `json:"id"`
	Address     string `json:"address"`
	Attempt     int    `json:"attempt,omitempty"`
	Count       int    `json:"count"`
	ClusterSize int    `json:"clusterSize"`
	State       string `json:"state,omitempty"`
	Replicated  bool   `json:"replicated"`
}

func (e *customNodeExecutor) runMySQLReplicationVerifyNode(ctx context.Context, node *runNode, command string) error {
	phase := "mysql-replication-verify"
	if strings.EqualFold(command, "delete") {
		payload := e.mysqlReplicationVerifyPayload(node, phase, "skipped", "delete does not verify MySQL replication", nil, nil)
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		return nil
	}

	cursor := map[string]any{"kind": normalizeNodeKind(node.Kind), "phase": phase}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	runID := e.mysqlCommandRunID()
	resourcePayload, err := buildMySQLReplicationVerifyResourcePayload(node.MySQL, node.ID, runID, e.mysqlResourceTenant())
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	remoteCommand := ""
	if !transportIsNATS(node.MySQL.Transport) {
		remoteCommand = buildMySQLReplicationVerifyCommand(node.MySQL)
	}
	hostSpec := e.mysqlReplicationHostCommandSpec(node, remoteCommand, resourcePayload)
	observe := e.mysqlReplicationObserveReceipt(node, phase, "")
	plan := e.mysqlReplicationPlanReceipt(node, phase, resourcePayload, hostSpec.Command, "planned", "eligible")
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		plan.Status = "skipped"
		plan.Reason = reason
		payload := e.mysqlReplicationVerifyPayload(node, phase, "skipped", reason, &observe, &plan)
		e.recordMySQLReplicationReceipts(node, phase, "skipped", reason, observe, plan, nil, nil, payload)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	runner, err := hostCommandTransport(hostSpec)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe.TargetDigest = runner.TargetDigest()
	targetID := strings.TrimSpace(plan.TargetID)
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindMySQLReplicationVerify); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		evidence := e.mysqlReplicationVerifyEvidence(node, "blocked", guardErr.Error(), transport.OperationResult{})
		payload := e.mysqlReplicationVerifyPayload(node, phase, "blocked", guardErr.Error(), &observe, &plan)
		e.recordMySQLReplicationReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, nil, &evidence, payload)
		runErr := &RunError{Class: "MYSQL_REPLICATION_VERIFY_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("MYSQL_REPLICATION_VERIFY_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("mysql replication verify: %w", guardErr))
	}

	var receipt transport.OperationResult
	if len(resourcePayload) > 0 && transportIsNATS(hostSpec.Transport) {
		if resourceRunner, ok := runner.(interface {
			RunResource(context.Context, json.RawMessage) transport.OperationResult
		}); ok {
			receipt = resourceRunner.RunResource(ctx, resourcePayload)
		} else {
			receipt = transport.OperationResult{
				Operation:    "resource",
				Status:       "failed",
				TargetDigest: runner.TargetDigest(),
				ExitCode:     1,
				Error:        "NATS transport does not support typed resource execution",
			}
		}
	} else {
		receipt = runner.Run(ctx, remoteCommand)
	}
	evidence := e.mysqlReplicationVerifyEvidence(node, "succeeded", "replication verified", receipt)
	verifyErr := evaluateMySQLReplicationEvidence(node.MySQL, &evidence)
	if !nodeStepSucceeded(receipt.Status) && verifyErr == nil {
		verifyErr = fmt.Errorf("mysql replication verify command failed: %s", firstReceiptMessage(receipt))
	}
	if verifyErr != nil {
		evidence.Status = "failed"
		evidence.Message = verifyErr.Error()
		payload := e.mysqlReplicationVerifyPayload(node, phase, "failed", verifyErr.Error(), &observe, &plan)
		e.recordMySQLReplicationReceipts(node, phase, "failed", verifyErr.Error(), observe, plan, &receipt, &evidence, payload)
		runErr := &RunError{Class: "MYSQL_REPLICATION_VERIFY_FAILED", Message: verifyErr.Error(), Digest: computeRunErrorDigest("MYSQL_REPLICATION_VERIFY_FAILED", verifyErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, verifyErr.Error(), map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, verifyErr)
	}

	payload := e.mysqlReplicationVerifyPayload(node, phase, "succeeded", "replication verified", &observe, &plan)
	e.recordMySQLReplicationReceipts(node, phase, "succeeded", "replication verified", observe, plan, &receipt, &evidence, payload)
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "replication verified", map[string]any{
		"phase":           phase,
		"status":          "succeeded",
		"cursor":          cursor,
		"replicatedNodes": evidence.ReplicatedNodes,
	}, nil)
	return nil
}

func buildMySQLReplicationVerifyCommand(spec MySQLSpec) string {
	rawSpec, _ := json.Marshal(spec)
	var resourceSpec opsmysql.Spec
	_ = json.Unmarshal(rawSpec, &resourceSpec)
	return opsmysql.BuildVerifyCommand(resourceSpec)
}

func buildMySQLReplicationVerifyResourcePayload(spec MySQLSpec, nodeID string, runID string, tenant string) (json.RawMessage, error) {
	rawSpec, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshal MySQL resource spec: %w", err)
	}
	req := opsmysql.ResourceRequest{
		APIVersion: opsmysql.RequestAPIVersion,
		Kind:       opsmysql.RequestKind,
		Tenant:     firstNonEmptyString(tenant, "default"),
		NodeID:     strings.TrimSpace(nodeID),
		RunID:      strings.TrimSpace(runID),
		NodeKind:   NodeKindMySQLReplicationVerify,
		Spec:       rawSpec,
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal MySQL resource request: %w", err)
	}
	return json.RawMessage(raw), nil
}

func (e *customNodeExecutor) mysqlReplicationHostCommandSpec(node *runNode, command string, resource json.RawMessage) HostCommandSpec {
	spec := node.MySQL
	if transportIsNATS(spec.Transport) {
		command = ""
	}
	return e.hostCommandAssignmentSpec(HostCommandSpec{
		Transport: spec.Transport,
		TargetID:  spec.TargetID,
		Target:    spec.Target,
		TargetEnv: spec.TargetEnv,
		Command:   command,
		Resource:  cloneJSONRawMessage(resource),
		Timeout:   spec.Timeout,
	}, node, NodeKindMySQLReplicationVerify)
}

func (e *customNodeExecutor) mysqlCommandRunID() string {
	if e != nil && e.run != nil {
		if resumeFromRunID := strings.TrimSpace(e.run.ResumeFromRunID); resumeFromRunID != "" {
			return resumeFromRunID
		}
		return strings.TrimSpace(e.run.RunID)
	}
	return ""
}

func (e *customNodeExecutor) mysqlResourceTenant() string {
	if e != nil && e.run != nil && e.run.Plan != nil {
		return normalizeFleetReadiness(e.run.Plan.Runner.Readiness).Tenant
	}
	return "default"
}

func writeShellAssignment(b *strings.Builder, key string, value string) {
	fmt.Fprintf(b, "%s=%s\n", key, transport.ShellQuote(strings.TrimSpace(value)))
}

func (e *customNodeExecutor) mysqlReplicationObserveReceipt(node *runNode, phase string, targetDigest string) mysqlReplicationObserveReceipt {
	targetID, guardMode, selected := e.mysqlReplicationTargetContext(node)
	spec := node.MySQL
	return mysqlReplicationObserveReceipt{
		APIVersion:              "torque.dev/mysql-replication-node/v1",
		Kind:                    "MySQLReplicationObserveReceipt",
		NodeID:                  node.ID,
		NodeKind:                normalizeNodeKind(node.Kind),
		TargetID:                targetID,
		Phase:                   phase,
		Status:                  "observed",
		GuardMode:               guardMode,
		SelectedTargetID:        targetID,
		SelectedTargets:         selected,
		TargetDigest:            strings.TrimSpace(targetDigest),
		MySQLNodes:              len(spec.Nodes),
		ExpectedClusterSize:     spec.ExpectedClusterSize,
		ExpectedReplicatedNodes: spec.ExpectedReplicatedNodes,
		ProbeIDDigest:           digestString(spec.ProbeID),
		ObservedAt:              time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) mysqlReplicationPlanReceipt(node *runNode, phase string, resource json.RawMessage, remoteCommand string, status string, reason string) mysqlReplicationPlanReceipt {
	targetID, guardMode, selected := e.mysqlReplicationTargetContext(node)
	var lockScopes []string
	var policySources []string
	if e != nil && e.run != nil && e.run.Plan != nil && e.run.Plan.Ops != nil {
		for _, lockInput := range e.run.Plan.Ops.Locks {
			if strings.TrimSpace(lockInput.Scope) != "" {
				lockScopes = append(lockScopes, strings.TrimSpace(lockInput.Scope))
			}
		}
		for _, decision := range e.run.Plan.Ops.PolicyDecisions {
			if strings.TrimSpace(decision.Source) != "" {
				policySources = append(policySources, strings.TrimSpace(decision.Source))
			}
		}
	}
	sort.Strings(lockScopes)
	sort.Strings(policySources)
	spec := node.MySQL
	transportName := strings.TrimSpace(spec.Transport)
	adapterKind := "mysql.shell-fallback"
	if transportIsNATS(transportName) {
		adapterKind = "mysql.native"
		remoteCommand = ""
	}
	return mysqlReplicationPlanReceipt{
		APIVersion:              "torque.dev/mysql-replication-node/v1",
		Kind:                    "MySQLReplicationPlanReceipt",
		NodeID:                  node.ID,
		NodeKind:                normalizeNodeKind(node.Kind),
		TargetID:                targetID,
		Phase:                   phase,
		Status:                  status,
		Reason:                  reason,
		GuardMode:               guardMode,
		AdapterKind:             adapterKind,
		Transport:               transportName,
		Operation:               NodeKindMySQLReplicationVerify,
		ResourceDigest:          digestString(string(resource)),
		CommandDigest:           digestString(remoteCommand),
		MySQLNodes:              len(spec.Nodes),
		ExpectedClusterSize:     spec.ExpectedClusterSize,
		ExpectedReplicatedNodes: spec.ExpectedReplicatedNodes,
		SelectedTargets:         selected,
		LockScopes:              lockScopes,
		PolicySources:           policySources,
		PlannedAt:               time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) mysqlReplicationVerifyEvidence(node *runNode, status string, message string, receipt transport.OperationResult) mysqlReplicationVerifyEvidence {
	spec := node.MySQL
	requireSynced := true
	if spec.RequireSynced != nil {
		requireSynced = *spec.RequireSynced
	}
	evidence := mysqlReplicationVerifyEvidence{
		APIVersion:              "torque.dev/mysql-replication-node/v1",
		Kind:                    "MySQLReplicationVerifyEvidence",
		NodeID:                  node.ID,
		NodeKind:                normalizeNodeKind(node.Kind),
		Status:                  strings.TrimSpace(status),
		Message:                 strings.TrimSpace(message),
		TargetID:                strings.TrimSpace(spec.TargetID),
		TargetDigest:            strings.TrimSpace(receipt.TargetDigest),
		ExpectedClusterSize:     spec.ExpectedClusterSize,
		ExpectedReplicatedNodes: spec.ExpectedReplicatedNodes,
		StableAttempts:          spec.StableAttempts,
		StableInterval:          mysqlDurationString(spec.StableInterval),
		InsertProbe:             spec.InsertProbe,
		RequireSynced:           requireSynced,
		ProbeID:                 strings.TrimSpace(spec.ProbeID),
		ProbeTable:              strings.TrimSpace(spec.Database) + "." + strings.TrimSpace(spec.ProbeTable),
		StatusPathDigest:        digestString(spec.StatusPath),
		Receipt:                 receipt,
		VerifiedAt:              time.Now().UTC().Format(time.RFC3339Nano),
	}
	if typed := parseMySQLReplicationResultStdout(receipt.Stdout); typed != nil {
		evidence.Status = firstNonEmptyString(strings.TrimSpace(typed.Status), evidence.Status)
		evidence.Message = firstNonEmptyString(strings.TrimSpace(typed.Message), evidence.Message)
		evidence.ExpectedClusterSize = typed.ExpectedClusterSize
		evidence.ExpectedReplicatedNodes = typed.ExpectedReplicatedNodes
		evidence.ReplicatedNodes = typed.ReplicatedNodes
		evidence.Attempt = typed.Attempt
		evidence.StableAttempts = typed.StableAttempts
		evidence.StableInterval = typed.StableInterval
		evidence.InsertProbe = typed.InsertProbe
		evidence.RequireSynced = typed.RequireSynced
		evidence.ProbeID = firstNonEmptyString(strings.TrimSpace(typed.ProbeID), evidence.ProbeID)
		evidence.ProbeTable = firstNonEmptyString(strings.TrimSpace(typed.ProbeTable), evidence.ProbeTable)
		evidence.StatusPathDigest = firstNonEmptyString(strings.TrimSpace(typed.StatusPathDigest), evidence.StatusPathDigest)
		evidence.Nodes = convertMySQLTypedNodes(typed.Nodes)
		return evidence
	}
	evidence.Nodes = parseMySQLReplicationStatus(receipt.Stdout, spec)
	for _, nodeEvidence := range evidence.Nodes {
		if nodeEvidence.Replicated {
			evidence.ReplicatedNodes++
		}
		if nodeEvidence.Attempt > evidence.Attempt {
			evidence.Attempt = nodeEvidence.Attempt
		}
	}
	return evidence
}

func mysqlDurationString(value *time.Duration) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func evaluateMySQLReplicationEvidence(spec MySQLSpec, evidence *mysqlReplicationVerifyEvidence) error {
	if evidence == nil {
		return fmt.Errorf("missing MySQL replication evidence")
	}
	if len(evidence.Nodes) == 0 {
		return fmt.Errorf("mysql replication verify returned no node status")
	}
	if evidence.ReplicatedNodes < spec.ExpectedReplicatedNodes {
		return fmt.Errorf("mysql replication verify failed: replicated nodes %d < required %d", evidence.ReplicatedNodes, spec.ExpectedReplicatedNodes)
	}
	byID := make(map[string]mysqlReplicationVerifyNodeEvidence, len(evidence.Nodes))
	for _, nodeEvidence := range evidence.Nodes {
		byID[nodeEvidence.ID] = nodeEvidence
	}
	for _, expected := range spec.Nodes {
		id := strings.TrimSpace(expected.ID)
		if id == "" {
			id = strings.TrimSpace(expected.Address)
		}
		nodeEvidence, ok := byID[id]
		if !ok {
			return fmt.Errorf("mysql replication verify missing status for node %s", id)
		}
		if nodeEvidence.ClusterSize != spec.ExpectedClusterSize {
			return fmt.Errorf("mysql node %s cluster size %d != expected %d", id, nodeEvidence.ClusterSize, spec.ExpectedClusterSize)
		}
		if spec.RequireSynced == nil || *spec.RequireSynced {
			if !strings.EqualFold(nodeEvidence.State, "Synced") {
				return fmt.Errorf("mysql node %s state %q != Synced", id, nodeEvidence.State)
			}
		}
	}
	return nil
}

func parseMySQLReplicationStatus(raw string, spec MySQLSpec) []mysqlReplicationVerifyNodeEvidence {
	latest := map[string]mysqlReplicationVerifyNodeEvidence{}
	order := make([]string, 0, len(spec.Nodes))
	for _, expected := range spec.Nodes {
		id := strings.TrimSpace(expected.ID)
		if id == "" {
			id = strings.TrimSpace(expected.Address)
		}
		order = append(order, id)
	}
	requireSynced := true
	if spec.RequireSynced != nil {
		requireSynced = *spec.RequireSynced
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "node=") || !strings.Contains(line, "cluster=") {
			continue
		}
		fields := parseShellKeyValueLine(line)
		id := strings.TrimSpace(fields["node"])
		if id == "" {
			continue
		}
		count, _ := strconv.Atoi(strings.TrimSpace(fields["count"]))
		clusterSize, _ := strconv.Atoi(strings.TrimSpace(fields["cluster"]))
		attempt, _ := strconv.Atoi(strings.TrimSpace(fields["attempt"]))
		replicated := count >= 1 && clusterSize == spec.ExpectedClusterSize
		if requireSynced {
			replicated = replicated && strings.EqualFold(strings.TrimSpace(fields["state"]), "Synced")
		}
		latest[id] = mysqlReplicationVerifyNodeEvidence{
			ID:          id,
			Address:     strings.TrimSpace(fields["ip"]),
			Attempt:     attempt,
			Count:       count,
			ClusterSize: clusterSize,
			State:       strings.TrimSpace(fields["state"]),
			Replicated:  replicated,
		}
	}
	out := make([]mysqlReplicationVerifyNodeEvidence, 0, len(latest))
	seen := map[string]struct{}{}
	for _, id := range order {
		if item, ok := latest[id]; ok {
			out = append(out, item)
			seen[id] = struct{}{}
		}
	}
	var extra []string
	for id := range latest {
		if _, ok := seen[id]; !ok {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	for _, id := range extra {
		out = append(out, latest[id])
	}
	return out
}

func parseMySQLReplicationResultStdout(stdout string) *opsmysql.Result {
	return opsmysql.ParseResultStdout(stdout)
}

func convertMySQLTypedNodes(nodes []opsmysql.NodeResult) []mysqlReplicationVerifyNodeEvidence {
	out := make([]mysqlReplicationVerifyNodeEvidence, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, mysqlReplicationVerifyNodeEvidence{
			ID:          node.ID,
			Address:     node.Address,
			Attempt:     node.Attempt,
			Count:       node.Count,
			ClusterSize: node.ClusterSize,
			State:       node.State,
			Replicated:  node.Replicated,
		})
	}
	return out
}

func parseShellKeyValueLine(line string) map[string]string {
	out := map[string]string{}
	for _, field := range strings.Fields(line) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func (e *customNodeExecutor) mysqlReplicationVerifyPayload(node *runNode, phase string, status string, message string, observe *mysqlReplicationObserveReceipt, plan *mysqlReplicationPlanReceipt) map[string]any {
	payload := map[string]any{
		"apiVersion": "torque.dev/mysql-replication-node/v1",
		"kind":       "MySQLReplicationVerify",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      strings.TrimSpace(phase),
		"status":     strings.TrimSpace(status),
		"message":    strings.TrimSpace(message),
	}
	if observe != nil {
		payload["observe"] = *observe
	}
	if plan != nil {
		payload["plan"] = *plan
	}
	return payload
}

func (e *customNodeExecutor) recordMySQLReplicationReceipts(node *runNode, phase string, status string, reason string, observe mysqlReplicationObserveReceipt, plan mysqlReplicationPlanReceipt, execute *transport.OperationResult, evidence *mysqlReplicationVerifyEvidence, payload map[string]any) {
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	if execute != nil {
		payload["targetDigest"] = execute.TargetDigest
		payload["execute"] = *execute
	}
	if evidence != nil {
		payload["evidence"] = *evidence
	}
	e.run.RecordJSONArtifact(node.ID, "mysql-replication-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "mysql-replication-plan.json", plan)
	if execute != nil {
		e.run.RecordJSONArtifact(node.ID, "mysql-replication-execute.json", *execute)
	}
	if evidence != nil {
		e.run.RecordJSONArtifact(node.ID, "mysql-replication-verify.json", *evidence)
	}
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) mysqlReplicationTargetContext(node *runNode) (string, string, []string) {
	targetID := ""
	if node != nil {
		targetID = strings.TrimSpace(node.MySQL.TargetID)
	}
	guardMode := "legacy"
	var selected []string
	if e != nil && e.run != nil && e.run.Plan != nil && e.run.Plan.Ops != nil {
		guardMode = "ops"
		if e.run.Plan.Ops.TargetGraph != nil {
			selected = append([]string(nil), e.run.Plan.Ops.TargetGraph.Selection.MatchedTargetIDs...)
		}
		if targetID == "" && len(selected) == 1 {
			targetID = selected[0]
		}
	}
	sort.Strings(selected)
	return targetID, guardMode, selected
}
