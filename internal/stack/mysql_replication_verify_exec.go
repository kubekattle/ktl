package stack

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	Operation               string   `json:"operation"`
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

	remoteCommand := buildMySQLReplicationVerifyCommand(node.MySQL)
	observe := e.mysqlReplicationObserveReceipt(node, phase, "")
	plan := e.mysqlReplicationPlanReceipt(node, phase, remoteCommand, "planned", "eligible")
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

	runner, err := mysqlReplicationVerifyRunner(node.MySQL)
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

	receipt := runner.Run(ctx, remoteCommand)
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

func mysqlReplicationVerifyRunner(spec MySQLSpec) (hostCommandRunner, error) {
	return hostCommandTransport(HostCommandSpec{
		Transport: spec.Transport,
		Target:    spec.Target,
		TargetEnv: spec.TargetEnv,
		Timeout:   spec.Timeout,
	})
}

func buildMySQLReplicationVerifyCommand(spec MySQLSpec) string {
	interval := defaultMySQLReplicationStableInterval
	if spec.StableInterval != nil {
		interval = *spec.StableInterval
	}
	requireSynced := true
	if spec.RequireSynced != nil {
		requireSynced = *spec.RequireSynced
	}
	var b strings.Builder
	b.WriteString("bash <<'TORQUE_MYSQL_REPLICATION_VERIFY'\n")
	b.WriteString("set -euo pipefail\n")
	writeShellAssignment(&b, "NODE_IDENTITY_FILE", spec.NodeIdentityFile)
	writeShellAssignment(&b, "NODE_SSH_OPTIONS", spec.NodeSSHOptions)
	writeShellAssignment(&b, "DATABASE_NAME", spec.Database)
	writeShellAssignment(&b, "PROBE_TABLE", spec.ProbeTable)
	writeShellAssignment(&b, "PROBE_ID", spec.ProbeID)
	writeShellAssignment(&b, "PROBE_PAYLOAD", spec.ProbePayload)
	writeShellAssignment(&b, "STATUS_PATH", spec.StatusPath)
	fmt.Fprintf(&b, "EXPECTED_CLUSTER_SIZE=%d\n", spec.ExpectedClusterSize)
	fmt.Fprintf(&b, "EXPECTED_REPLICATED_NODES=%d\n", spec.ExpectedReplicatedNodes)
	fmt.Fprintf(&b, "STABLE_ATTEMPTS=%d\n", spec.StableAttempts)
	fmt.Fprintf(&b, "STABLE_INTERVAL_SECONDS=%s\n", transport.ShellQuote(fmt.Sprintf("%.3f", interval.Seconds())))
	if spec.InsertProbe {
		b.WriteString("INSERT_PROBE=1\n")
	} else {
		b.WriteString("INSERT_PROBE=0\n")
	}
	if requireSynced {
		b.WriteString("REQUIRE_SYNCED=1\n")
	} else {
		b.WriteString("REQUIRE_SYNCED=0\n")
	}
	b.WriteString("NODES=()\n")
	for _, node := range spec.Nodes {
		entry := strings.Join([]string{
			strings.TrimSpace(node.ID),
			strings.TrimSpace(node.Address),
			firstNonEmptyString(node.SSHUser, "root"),
			strconv.Itoa(node.SSHPort),
		}, "|")
		fmt.Fprintf(&b, "NODES+=(%s)\n", transport.ShellQuote(entry))
	}
	b.WriteString(mysqlReplicationVerifyScriptBody)
	b.WriteString("\nTORQUE_MYSQL_REPLICATION_VERIFY\n")
	return b.String()
}

func writeShellAssignment(b *strings.Builder, key string, value string) {
	fmt.Fprintf(b, "%s=%s\n", key, transport.ShellQuote(strings.TrimSpace(value)))
}

const mysqlReplicationVerifyScriptBody = `
EXTRA_SSH_OPTS=()
if [[ -n "${NODE_SSH_OPTIONS}" ]]; then
  read -r -a EXTRA_SSH_OPTS <<< "${NODE_SSH_OPTIONS}"
fi
SSH_OPTS=(-n -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=2)
if [[ -n "${NODE_IDENTITY_FILE}" ]]; then
  SSH_OPTS+=(-i "${NODE_IDENTITY_FILE}")
fi
SSH_OPTS+=("${EXTRA_SSH_OPTS[@]}")

sql_literal() {
  local escaped
  escaped="$(printf '%s' "$1" | sed "s/'/''/g")"
  printf "'%s'" "${escaped}"
}

node_query() {
  local node_addr="$1"
  local node_user="$2"
  local node_port="$3"
  local sql="$4"
  local quoted_sql
  local args=("${SSH_OPTS[@]}")
  if [[ -n "${node_port}" && "${node_port}" != "0" ]]; then
    args+=(-p "${node_port}")
  fi
  printf -v quoted_sql '%q' "${sql}"
  ssh "${args[@]}" "${node_user}@${node_addr}" "if command -v mariadb >/dev/null 2>&1; then mariadb -uroot -Nse ${quoted_sql}; else mysql -uroot -Nse ${quoted_sql}; fi"
}

if [[ "${#NODES[@]}" -eq 0 ]]; then
  echo "mysql replication verify requires at least one node" >&2
  exit 2
fi

first_entry="${NODES[0]}"
IFS='|' read -r first_id first_addr first_user first_port <<< "${first_entry}"
first_user="${first_user:-root}"
status_tmp="$(mktemp)"
trap 'rm -f "${status_tmp}"' EXIT

if [[ "${INSERT_PROBE}" == "1" ]]; then
  probe_id_sql="$(sql_literal "${PROBE_ID}")"
  probe_payload_sql="$(sql_literal "${PROBE_PAYLOAD}")"
  node_query "${first_addr}" "${first_user}" "${first_port}" "CREATE DATABASE IF NOT EXISTS ${DATABASE_NAME};"
  node_query "${first_addr}" "${first_user}" "${first_port}" "CREATE TABLE IF NOT EXISTS ${DATABASE_NAME}.${PROBE_TABLE} (id varchar(128) PRIMARY KEY, payload varchar(255), observed_at timestamp DEFAULT current_timestamp ON UPDATE current_timestamp);"
  node_query "${first_addr}" "${first_user}" "${first_port}" "INSERT INTO ${DATABASE_NAME}.${PROBE_TABLE}(id, payload) VALUES (${probe_id_sql}, ${probe_payload_sql}) ON DUPLICATE KEY UPDATE payload=VALUES(payload), observed_at=current_timestamp;"
fi

copy_status() {
  if [[ -n "${STATUS_PATH}" ]]; then
    mkdir -p "$(dirname "${STATUS_PATH}")"
    cp "${status_tmp}" "${STATUS_PATH}"
  fi
}

replicated=0
for attempt in $(seq 1 "${STABLE_ATTEMPTS}"); do
  replicated=0
  : >"${status_tmp}"
  for entry in "${NODES[@]}"; do
    IFS='|' read -r node_id node_addr node_user node_port <<< "${entry}"
    node_user="${node_user:-root}"
    count="$(node_query "${node_addr}" "${node_user}" "${node_port}" "SELECT COUNT(*) FROM ${DATABASE_NAME}.${PROBE_TABLE} WHERE id=$(sql_literal "${PROBE_ID}");" 2>/dev/null || true)"
    size="$(node_query "${node_addr}" "${node_user}" "${node_port}" "SHOW STATUS LIKE 'wsrep_cluster_size';" 2>/dev/null | awk '{print $2}' | tail -n1 || true)"
    state="$(node_query "${node_addr}" "${node_user}" "${node_port}" "SHOW STATUS LIKE 'wsrep_local_state_comment';" 2>/dev/null | awk '{print $2}' | tail -n1 || true)"
    printf 'attempt=%s node=%s ip=%s count=%s cluster=%s state=%s\n' "${attempt}" "${node_id}" "${node_addr}" "${count:-0}" "${size:-0}" "${state}" >>"${status_tmp}"
    if [[ "${count}" == "1" && "${size}" == "${EXPECTED_CLUSTER_SIZE}" ]]; then
      if [[ "${REQUIRE_SYNCED}" != "1" || "${state}" == "Synced" ]]; then
        replicated="$((replicated + 1))"
      fi
    fi
  done
  copy_status
  if [[ "${replicated}" -ge "${EXPECTED_REPLICATED_NODES}" ]]; then
    cat "${status_tmp}"
    printf 'mysql-replication-verified replicated=%s/%s cluster=%s\n' "${replicated}" "${EXPECTED_REPLICATED_NODES}" "${EXPECTED_CLUSTER_SIZE}"
    exit 0
  fi
  if [[ "${attempt}" != "${STABLE_ATTEMPTS}" ]]; then
    sleep "${STABLE_INTERVAL_SECONDS}"
  fi
done

cat "${status_tmp}"
echo "mysql replication verification failed: replicated=${replicated}/${EXPECTED_REPLICATED_NODES} cluster=${EXPECTED_CLUSTER_SIZE}" >&2
exit 1
`

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

func (e *customNodeExecutor) mysqlReplicationPlanReceipt(node *runNode, phase string, remoteCommand string, status string, reason string) mysqlReplicationPlanReceipt {
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
		Operation:               NodeKindMySQLReplicationVerify,
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
