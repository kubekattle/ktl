package mysql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	RequestAPIVersion = "torque.dev/mysql-resource-request/v1"
	RequestKind       = "MySQLResourceRequest"
	ResultAPIVersion  = "torque.dev/mysql-resource-result/v1"
	ResultKind        = "MySQLResourceResult"

	defaultStableAttempts = 60
	defaultStableInterval = 2 * time.Second
)

type ResourceRequest struct {
	APIVersion string          `json:"apiVersion,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Tenant     string          `json:"tenant,omitempty"`
	NodeID     string          `json:"nodeId,omitempty"`
	RunID      string          `json:"runId,omitempty"`
	NodeKind   string          `json:"nodeKind"`
	Spec       json.RawMessage `json:"spec"`
}

type Spec struct {
	NodeIdentityFile string `json:"nodeIdentityFile,omitempty"`
	NodeSSHOptions   string `json:"nodeSshOptions,omitempty"`

	Nodes                   []NodeSpec     `json:"nodes,omitempty"`
	ExpectedClusterSize     int            `json:"expectedClusterSize,omitempty"`
	ExpectedReplicatedNodes int            `json:"expectedReplicatedNodes,omitempty"`
	Database                string         `json:"database,omitempty"`
	ProbeTable              string         `json:"probeTable,omitempty"`
	ProbeID                 string         `json:"probeId,omitempty"`
	ProbePayload            string         `json:"probePayload,omitempty"`
	StatusPath              string         `json:"statusPath,omitempty"`
	InsertProbe             bool           `json:"insertProbe,omitempty"`
	RequireSynced           *bool          `json:"requireSynced,omitempty"`
	StableAttempts          int            `json:"stableAttempts,omitempty"`
	StableInterval          *time.Duration `json:"stableInterval,omitempty"`
}

type NodeSpec struct {
	ID      string `json:"id,omitempty"`
	Address string `json:"address,omitempty"`
	SSHUser string `json:"sshUser,omitempty"`
	SSHPort int    `json:"sshPort,omitempty"`
}

type Result struct {
	APIVersion              string       `json:"apiVersion"`
	Kind                    string       `json:"kind"`
	NodeID                  string       `json:"nodeId,omitempty"`
	RunID                   string       `json:"runId,omitempty"`
	NodeKind                string       `json:"nodeKind"`
	Status                  string       `json:"status"`
	Changed                 bool         `json:"changed"`
	Message                 string       `json:"message,omitempty"`
	Database                string       `json:"database,omitempty"`
	ExpectedClusterSize     int          `json:"expectedClusterSize,omitempty"`
	ExpectedReplicatedNodes int          `json:"expectedReplicatedNodes,omitempty"`
	ReplicatedNodes         int          `json:"replicatedNodes,omitempty"`
	Attempt                 int          `json:"attempt,omitempty"`
	StableAttempts          int          `json:"stableAttempts,omitempty"`
	StableInterval          string       `json:"stableInterval,omitempty"`
	InsertProbe             bool         `json:"insertProbe,omitempty"`
	RequireSynced           bool         `json:"requireSynced"`
	ProbeID                 string       `json:"probeId,omitempty"`
	ProbeTable              string       `json:"probeTable,omitempty"`
	StatusPathDigest        string       `json:"statusPathDigest,omitempty"`
	Nodes                   []NodeResult `json:"nodes,omitempty"`
	StartedAt               string       `json:"startedAt"`
	CompletedAt             string       `json:"completedAt"`
}

type NodeResult struct {
	ID          string `json:"id"`
	Address     string `json:"address,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	Count       int    `json:"count"`
	ClusterSize int    `json:"clusterSize"`
	State       string `json:"state,omitempty"`
	Replicated  bool   `json:"replicated"`
}

type Runner struct {
	CommandRunner transport.Runner
	Stdout        io.Writer
	Stderr        io.Writer
}

func Execute(ctx context.Context, req ResourceRequest) (Result, error) {
	return Runner{}.Execute(ctx, req)
}

func (r Runner) Execute(ctx context.Context, req ResourceRequest) (Result, error) {
	req = normalizeRequest(req)
	spec, err := decodeSpec(req.Spec)
	if err != nil {
		return failureResult(req, Spec{}, err), err
	}
	if strings.TrimSpace(req.NodeKind) != "mysql.replication.verify" {
		err := fmt.Errorf("unsupported MySQL resource kind %q", strings.TrimSpace(req.NodeKind))
		return failureResult(req, spec, err), err
	}

	command := BuildVerifyCommand(spec)
	commandRunner := r.CommandRunner
	if commandRunner == nil {
		commandRunner = transport.ExecRunner{}
	}
	started := time.Now().UTC()
	output, runErr := commandRunner.Run(ctx, "bash", []string{"-c", command})
	if r.Stdout != nil && len(output.Stdout) > 0 {
		_, _ = r.Stdout.Write(output.Stdout)
	}
	if r.Stderr != nil && len(output.Stderr) > 0 {
		_, _ = r.Stderr.Write(output.Stderr)
	}

	nodes := ParseReplicationStatus(string(output.Stdout), spec)
	replicatedNodes, attempt := summarizeNodes(nodes)
	result := Result{
		APIVersion:              ResultAPIVersion,
		Kind:                    ResultKind,
		NodeID:                  strings.TrimSpace(req.NodeID),
		RunID:                   strings.TrimSpace(req.RunID),
		NodeKind:                strings.TrimSpace(req.NodeKind),
		Status:                  "succeeded",
		Changed:                 spec.InsertProbe,
		Message:                 "replication verified",
		Database:                strings.TrimSpace(spec.Database),
		ExpectedClusterSize:     spec.ExpectedClusterSize,
		ExpectedReplicatedNodes: spec.ExpectedReplicatedNodes,
		ReplicatedNodes:         replicatedNodes,
		Attempt:                 attempt,
		StableAttempts:          spec.StableAttempts,
		StableInterval:          durationString(spec.StableInterval),
		InsertProbe:             spec.InsertProbe,
		RequireSynced:           requireSynced(spec),
		ProbeID:                 strings.TrimSpace(spec.ProbeID),
		ProbeTable:              qualifyProbeTable(spec),
		StatusPathDigest:        optionalDigest(spec.StatusPath),
		Nodes:                   nodes,
		StartedAt:               started.Format(time.RFC3339Nano),
		CompletedAt:             time.Now().UTC().Format(time.RFC3339Nano),
	}
	if verifyErr := Evaluate(spec, nodes); verifyErr != nil {
		result.Status = "failed"
		result.Message = verifyErr.Error()
		if runErr == nil {
			runErr = verifyErr
		}
	}
	if runErr != nil {
		result.Status = "failed"
		result.Message = firstNonEmptyString(result.Message, strings.TrimSpace(string(output.Stderr)), runErr.Error())
		return result, runErr
	}
	return result, nil
}

func ExecuteFromBase64(ctx context.Context, encoded string) (Result, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return failureResult(ResourceRequest{NodeKind: "mysql.replication.verify"}, Spec{}, fmt.Errorf("decode base64 MySQL resource request: %w", err)), err
	}
	var req ResourceRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return failureResult(ResourceRequest{NodeKind: "mysql.replication.verify"}, Spec{}, fmt.Errorf("decode MySQL resource request JSON: %w", err)), err
	}
	return Execute(ctx, req)
}

func WriteResult(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(result)
}

func BuildVerifyCommand(spec Spec) string {
	spec = normalizeSpec(spec)
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
	fmt.Fprintf(&b, "STABLE_INTERVAL_SECONDS=%s\n", transport.ShellQuote(fmt.Sprintf("%.3f", spec.StableInterval.Seconds())))
	if spec.InsertProbe {
		b.WriteString("INSERT_PROBE=1\n")
	} else {
		b.WriteString("INSERT_PROBE=0\n")
	}
	if requireSynced(spec) {
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

func ParseResultStdout(stdout string) *Result {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var result Result
		if err := json.Unmarshal([]byte(line), &result); err == nil && strings.TrimSpace(result.Kind) == ResultKind {
			return &result
		}
	}
	return nil
}

func ParseReplicationStatus(raw string, spec Spec) []NodeResult {
	latest := map[string]NodeResult{}
	order := make([]string, 0, len(spec.Nodes))
	for _, expected := range spec.Nodes {
		id := strings.TrimSpace(expected.ID)
		if id == "" {
			id = strings.TrimSpace(expected.Address)
		}
		order = append(order, id)
	}
	requireSynced := requireSynced(spec)
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
		latest[id] = NodeResult{
			ID:          id,
			Address:     strings.TrimSpace(fields["ip"]),
			Attempt:     attempt,
			Count:       count,
			ClusterSize: clusterSize,
			State:       strings.TrimSpace(fields["state"]),
			Replicated:  replicated,
		}
	}
	out := make([]NodeResult, 0, len(latest))
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

func Evaluate(spec Spec, nodes []NodeResult) error {
	if len(nodes) == 0 {
		return fmt.Errorf("mysql replication verify returned no node status")
	}
	replicatedNodes, _ := summarizeNodes(nodes)
	if replicatedNodes < spec.ExpectedReplicatedNodes {
		return fmt.Errorf("mysql replication verify failed: replicated nodes %d < required %d", replicatedNodes, spec.ExpectedReplicatedNodes)
	}
	byID := make(map[string]NodeResult, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	for _, expected := range spec.Nodes {
		id := strings.TrimSpace(expected.ID)
		if id == "" {
			id = strings.TrimSpace(expected.Address)
		}
		node, ok := byID[id]
		if !ok {
			return fmt.Errorf("mysql replication verify missing status for node %s", id)
		}
		if node.ClusterSize != spec.ExpectedClusterSize {
			return fmt.Errorf("mysql node %s cluster size %d != expected %d", id, node.ClusterSize, spec.ExpectedClusterSize)
		}
		if requireSynced(spec) && !strings.EqualFold(node.State, "Synced") {
			return fmt.Errorf("mysql node %s state %q != Synced", id, node.State)
		}
	}
	return nil
}

func normalizeRequest(req ResourceRequest) ResourceRequest {
	req.APIVersion = firstNonEmptyString(strings.TrimSpace(req.APIVersion), RequestAPIVersion)
	req.Kind = firstNonEmptyString(strings.TrimSpace(req.Kind), RequestKind)
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.NodeKind = strings.TrimSpace(req.NodeKind)
	return req
}

func decodeSpec(raw json.RawMessage) (Spec, error) {
	var spec Spec
	if len(raw) == 0 {
		return normalizeSpec(spec), nil
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return Spec{}, fmt.Errorf("decode MySQL resource spec: %w", err)
	}
	return normalizeSpec(spec), nil
}

func normalizeSpec(spec Spec) Spec {
	spec.NodeIdentityFile = strings.TrimSpace(spec.NodeIdentityFile)
	spec.NodeSSHOptions = strings.TrimSpace(spec.NodeSSHOptions)
	spec.Database = strings.TrimSpace(spec.Database)
	spec.ProbeTable = strings.TrimSpace(spec.ProbeTable)
	spec.ProbeID = strings.TrimSpace(spec.ProbeID)
	spec.ProbePayload = strings.TrimSpace(spec.ProbePayload)
	spec.StatusPath = strings.TrimSpace(spec.StatusPath)
	for i := range spec.Nodes {
		spec.Nodes[i].ID = strings.TrimSpace(spec.Nodes[i].ID)
		spec.Nodes[i].Address = strings.TrimSpace(spec.Nodes[i].Address)
		spec.Nodes[i].SSHUser = strings.TrimSpace(spec.Nodes[i].SSHUser)
	}
	if spec.ExpectedClusterSize == 0 && len(spec.Nodes) > 0 {
		spec.ExpectedClusterSize = len(spec.Nodes)
	}
	if spec.ExpectedReplicatedNodes == 0 && spec.ExpectedClusterSize > 0 {
		spec.ExpectedReplicatedNodes = spec.ExpectedClusterSize
	}
	if spec.StableAttempts <= 0 {
		spec.StableAttempts = defaultStableAttempts
	}
	if spec.StableInterval == nil || *spec.StableInterval <= 0 {
		value := defaultStableInterval
		spec.StableInterval = &value
	}
	return spec
}

func failureResult(req ResourceRequest, spec Spec, err error) Result {
	spec = normalizeSpec(spec)
	return Result{
		APIVersion:              ResultAPIVersion,
		Kind:                    ResultKind,
		NodeID:                  strings.TrimSpace(req.NodeID),
		RunID:                   strings.TrimSpace(req.RunID),
		NodeKind:                firstNonEmptyString(strings.TrimSpace(req.NodeKind), "mysql.replication.verify"),
		Status:                  "failed",
		Changed:                 spec.InsertProbe,
		Message:                 err.Error(),
		Database:                strings.TrimSpace(spec.Database),
		ExpectedClusterSize:     spec.ExpectedClusterSize,
		ExpectedReplicatedNodes: spec.ExpectedReplicatedNodes,
		StableAttempts:          spec.StableAttempts,
		StableInterval:          durationString(spec.StableInterval),
		InsertProbe:             spec.InsertProbe,
		RequireSynced:           requireSynced(spec),
		ProbeID:                 strings.TrimSpace(spec.ProbeID),
		ProbeTable:              qualifyProbeTable(spec),
		StatusPathDigest:        optionalDigest(spec.StatusPath),
		StartedAt:               time.Now().UTC().Format(time.RFC3339Nano),
		CompletedAt:             time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func requireSynced(spec Spec) bool {
	return spec.RequireSynced == nil || *spec.RequireSynced
}

func summarizeNodes(nodes []NodeResult) (int, int) {
	replicated := 0
	attempt := 0
	for _, node := range nodes {
		if node.Replicated {
			replicated++
		}
		if node.Attempt > attempt {
			attempt = node.Attempt
		}
	}
	return replicated, attempt
}

func durationString(value *time.Duration) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func qualifyProbeTable(spec Spec) string {
	database := strings.TrimSpace(spec.Database)
	table := strings.TrimSpace(spec.ProbeTable)
	switch {
	case database == "":
		return table
	case table == "":
		return database
	default:
		return database + "." + table
	}
}

func optionalDigest(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return transport.ValueDigest(strings.TrimSpace(value))
}

func writeShellAssignment(b *strings.Builder, key string, value string) {
	fmt.Fprintf(b, "%s=%s\n", key, transport.ShellQuote(strings.TrimSpace(value)))
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
if [[ "${#EXTRA_SSH_OPTS[@]}" -gt 0 ]]; then
  SSH_OPTS+=("${EXTRA_SSH_OPTS[@]}")
fi

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
