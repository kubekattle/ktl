#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-007.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-007 proves targeted NATS fleet fan-out: compact three ready agents
into the registry, run one host.command.run stack node with runner.mode=fleet
and no explicit host.target, prove three per-target workers execute, then stop
one worker and prove execution blocks with a missing receipt.
EOF
}

cleanup_enabled=1
external_nats_url=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --nats-url)
      [[ $# -ge 2 ]] || ops_fail "--nats-url requires a value"
      external_nats_url="$2"
      shift 2
      ;;
    --cleanup)
      cleanup_enabled=1
      shift
      ;;
    --no-cleanup)
      cleanup_enabled=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      ops_fail "unknown argument: $1"
      ;;
  esac
done

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3

repo_root="$(ops_repo_root)"
ops_init_run "OPS-AGENT-007"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-007.XXXXXX")"
stack_pass_dir="${scratch_root}/stack-pass"
stack_missing_dir="${scratch_root}/stack-missing"
registry_path="${scratch_root}/agent-registry.json"
pass_marker="${scratch_root}/fanout-pass-marker.txt"
missing_marker="${scratch_root}/fanout-missing-marker.txt"
nats_pid=""
nats_url="${external_nats_url}"
cleanup_status="pending"
worker_pids=()
worker_ids=("agent-worker-01" "agent-worker-02" "agent-worker-03")
target_ids=("host/mysql-01" "host/mysql-02" "host/mysql-03")
subjects=("torque.assign.lab.host_mysql-01" "torque.assign.lab.host_mysql-02" "torque.assign.lab.host_mysql-03")
stream_name="TORQUE_AGENT_EVENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
registry_durable="torque-ops-agent-007-${OPS_RUN_ID//[^A-Za-z0-9]/}"
missing_apply_code=0

finish() {
  local code=$?
  trap - EXIT
  set +e
  for pid in "${worker_pids[@]:-}"; do
    if [[ -n "${pid}" ]]; then
      kill "${pid}" 2>/dev/null
      wait "${pid}" 2>/dev/null
    fi
  done
  if [[ -n "${nats_pid}" ]]; then
    kill "${nats_pid}" 2>/dev/null
    wait "${nats_pid}" 2>/dev/null
  fi
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    cleanup_status="removed"
  else
    cleanup_status="kept:${scratch_root}"
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object \
    "${OPS_RUN_DIR}/cleanup/receipt.json" \
    "status=succeeded" \
    "cleanup=${cleanup_status}"
  ops_write_json_object \
    "${OPS_RUN_DIR}/result.json" \
    "status=$([[ ${code} -eq 0 ]] && echo succeeded || echo failed)" \
    "taskId=${OPS_TASK_ID}" \
    "runId=${OPS_RUN_ID}" \
    "startedAt=${started_at}" \
    "finishedAt=$(ops_utc_now)"
  ops_scan_for_secret_material "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/redaction-report.json" || code=1
  ops_write_manifest "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/manifest.json" || code=1
  ops_export_bundle "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}" || code=1
  ops_validate_evidence_contract "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}" >"${OPS_BUNDLE_PATH%.tgz}.contract.json" || code=1
  if [[ ${code} -eq 0 ]]; then
    ops_log "evidence: ${OPS_RUN_DIR}"
    ops_log "bundle: ${OPS_BUNDLE_PATH}"
  else
    echo "evidence: ${OPS_RUN_DIR}" >&2
    echo "bundle: ${OPS_BUNDLE_PATH}" >&2
  fi
  exit "${code}"
}
trap finish EXIT

wait_for_nats() {
  python3 - "${nats_url}" <<'PY'
import socket
import sys
import time
from urllib.parse import urlparse

url = urlparse(sys.argv[1])
host = url.hostname or "127.0.0.1"
port = url.port or 4222
deadline = time.time() + 10
last = None
while time.time() < deadline:
    try:
        with socket.create_connection((host, port), timeout=0.2):
            sys.exit(0)
    except OSError as exc:
        last = exc
        time.sleep(0.1)
raise SystemExit(f"NATS server did not become reachable: {last}")
PY
}

wait_for_worker() {
  local pid="$1"
  local log="$2"
  for _ in $(seq 1 100); do
    if grep -q "nats worker ready" "${log}" 2>/dev/null; then
      return 0
    fi
    if ! kill -0 "${pid}" 2>/dev/null; then
      ops_fail "torque-agent nats worker exited early; see ${log}"
    fi
    sleep 0.1
  done
  ops_fail "torque-agent nats worker did not become ready; see ${log}"
}

write_stack() {
  local dir="$1"
  local marker="$2"
  local label="$3"
  mkdir -p "${dir}"
  cat >"${dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: targeted-nats-fanout-${label}
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: "${registry_path}"
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 100
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
  fanout:
    maxParallel: 3
    maxFailed: 0
    minSucceededPercent: 100
    onPartialFailure: block
nodes:
  - kind: host.command.run
    name: write-${label}-marker
    host:
      transport: nats
      command: "printf '${label}\\n' >> ${marker}"
YAML
}

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/logs" "${OPS_RUN_DIR}/verification" "${stack_pass_dir}" "${stack_missing_dir}"

ops_log "build torque and torque-agent"
make -C "${repo_root}" -s build build-agent >"${OPS_RUN_DIR}/build/make-build.out" 2>&1

if [[ -z "${nats_url}" ]]; then
  ops_require_cmd nats-server
  nats_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
  nats_url="nats://127.0.0.1:${nats_port}"
  mkdir -p "${scratch_root}/nats"
  nats-server -js -sd "${scratch_root}/nats" -a 127.0.0.1 -p "${nats_port}" >"${OPS_RUN_DIR}/logs/nats-server.log" 2>&1 &
  nats_pid="$!"
fi
wait_for_nats

ops_log "start targeted NATS workers"
for i in "${!worker_ids[@]}"; do
  agent_id="${worker_ids[$i]}"
  target_id="${target_ids[$i]}"
  subject="${subjects[$i]}"
  "${repo_root}/bin/torque-agent" nats worker \
    --nats-url "${nats_url}" \
    --subject "${subject}" \
    --agent-id "${agent_id}" \
    --tenant lab \
    --target-id "${target_id}" \
    --hostname "worker-mysql-0$((i + 1))" \
    --capability host.command.run \
    >"${OPS_RUN_DIR}/logs/worker-$((i + 1)).log" 2>&1 &
  worker_pids+=("$!")
  wait_for_worker "${worker_pids[$i]}" "${OPS_RUN_DIR}/logs/worker-$((i + 1)).log"
done

ops_log "publish heartbeats and compact registry"
for i in "${!worker_ids[@]}"; do
  "${repo_root}/bin/torque-agent" nats heartbeat \
    --nats-url "${nats_url}" \
    --jetstream \
    --stream "${stream_name}" \
    --once \
    --discover-capabilities=false \
    --agent-id "${worker_ids[$i]}" \
    --tenant lab \
    --target-id "${target_ids[$i]}" \
    --hostname "worker-mysql-0$((i + 1))" \
    --label role=mysql \
    --capability host.command.run \
    >"${OPS_RUN_DIR}/logs/heartbeat-$((i + 1)).json" 2>"${OPS_RUN_DIR}/logs/heartbeat-$((i + 1)).err"
done
"${repo_root}/bin/torque" ops agent registry compact \
  --nats-url "${nats_url}" \
  --tenant lab \
  --stream "${stream_name}" \
  --durable "${registry_durable}" \
  --store file \
  --store-path "${registry_path}" \
  --max-messages 3 \
  --timeout 10s \
  --format json \
  >"${OPS_RUN_DIR}/verification/registry-compact.json"
"${repo_root}/bin/torque" ops agent status \
  --source store \
  --store file \
  --store-path "${registry_path}" \
  --tenant lab \
  --selector role=mysql \
  --format json \
  >"${OPS_RUN_DIR}/verification/registry-status.json"

write_stack "${stack_pass_dir}" "${pass_marker}" "fanout-pass"
write_stack "${stack_missing_dir}" "${missing_marker}" "fanout-missing"

ops_log "apply targeted fan-out stack to three workers"
TORQUE_NATS_URL="${nats_url}" "${repo_root}/bin/torque" stack apply --config "${stack_pass_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/pass-apply.out" 2>"${OPS_RUN_DIR}/logs/pass-apply.err"
"${repo_root}/bin/torque" stack audit --config "${stack_pass_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/pass-audit.json"
"${repo_root}/bin/torque" stack export --config "${stack_pass_dir}" --out "${OPS_RUN_DIR}/pass-stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/pass-export.out" 2>"${OPS_RUN_DIR}/logs/pass-export.err"

ops_log "stop one worker and prove fan-out blocks on missing receipt"
kill "${worker_pids[2]}" 2>/dev/null || true
wait "${worker_pids[2]}" 2>/dev/null || true
worker_pids[2]=""
set +e
TORQUE_NATS_URL="${nats_url}" "${repo_root}/bin/torque" stack apply --config "${stack_missing_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/missing-apply.out" 2>"${OPS_RUN_DIR}/logs/missing-apply.err"
missing_apply_code="$?"
set -e
"${repo_root}/bin/torque" stack audit --config "${stack_missing_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/missing-audit.json"
"${repo_root}/bin/torque" stack export --config "${stack_missing_dir}" --out "${OPS_RUN_DIR}/missing-stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/missing-export.out" 2>"${OPS_RUN_DIR}/logs/missing-export.err"

ops_log "verify targeted fan-out receipts"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${stream_name}" "${pass_marker}" "${missing_marker}" "${missing_apply_code}" <<'PY'
import json
import sys
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
nats_url = sys.argv[5]
stream_name = sys.argv[6]
pass_marker = Path(sys.argv[7])
missing_marker = Path(sys.argv[8])
missing_apply_code = int(sys.argv[9])

agents = ["agent-worker-01", "agent-worker-02", "agent-worker-03"]
targets = ["host/mysql-01", "host/mysql-02", "host/mysql-03"]
subjects = ["torque.assign.lab.host_mysql-01", "torque.assign.lab.host_mysql-02", "torque.assign.lab.host_mysql-03"]

def load(path):
    with (run_dir / path).open("r", encoding="utf-8") as f:
        return json.load(f)

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            body = item.get("body", "")
            try:
                return json.loads(body)
            except json.JSONDecodeError:
                return {}
    return {}

def marker_count(path, token):
    if not path.exists():
        return 0
    return path.read_text(encoding="utf-8").count(token)

pass_audit = load("verification/pass-audit.json")
missing_audit = load("verification/missing-audit.json")
registry_status = load("verification/registry-status.json")
pass_fanout = artifact(pass_audit, "host-command-fanout.json")
pass_execute = artifact(pass_audit, "host-command-execute.json")
missing_fanout = artifact(missing_audit, "host-command-fanout.json")
errors = []

if registry_status.get("summary", {}).get("ready") != 3:
    errors.append("registry status must have three ready agents")
if pass_audit.get("status") != "succeeded":
    errors.append("pass stack audit must succeed")
if pass_fanout.get("status") != "succeeded":
    errors.append("pass fanout status must be succeeded")
if pass_fanout.get("summary", {}).get("targetCount") != 3 or pass_fanout.get("summary", {}).get("succeeded") != 3:
    errors.append(f"pass fanout summary wrong: {pass_fanout.get('summary')}")
if pass_execute.get("metadata", {}).get("fanout") != "targeted-nats":
    errors.append("pass execute receipt must identify targeted NATS fanout")
if marker_count(pass_marker, "fanout-pass") != 3:
    errors.append("pass marker must be written by all three workers")

seen = {}
for result in pass_fanout.get("results", []):
    receipt = result.get("receipt", {})
    metadata = receipt.get("metadata", {})
    agent_id = metadata.get("agentId")
    seen[agent_id] = {
        "targetId": metadata.get("targetId"),
        "assignmentTargetId": metadata.get("assignmentTargetId"),
        "expectedAgentId": metadata.get("expectedAgentId"),
        "workerSubject": metadata.get("workerSubject"),
        "requiredCapability": metadata.get("requiredCapability"),
        "nodeId": metadata.get("nodeId"),
        "runId": metadata.get("runId"),
        "workerDecision": metadata.get("workerDecision"),
    }
for agent, target, subject in zip(agents, targets, subjects):
    meta = seen.get(agent)
    if not meta:
        errors.append(f"missing pass receipt for {agent}")
        continue
    if meta["targetId"] != target or meta["assignmentTargetId"] != target:
        errors.append(f"{agent} target metadata mismatch: {meta}")
    if meta["expectedAgentId"] != agent:
        errors.append(f"{agent} expectedAgentId mismatch: {meta}")
    if meta["workerSubject"] != subject:
        errors.append(f"{agent} workerSubject mismatch: {meta}")
    if meta["requiredCapability"] != "host.command.run":
        errors.append(f"{agent} requiredCapability mismatch: {meta}")
    if meta["workerDecision"] != "executed":
        errors.append(f"{agent} workerDecision mismatch: {meta}")
    if not meta["runId"] or not meta["nodeId"]:
        errors.append(f"{agent} missing runId/nodeId metadata: {meta}")

if missing_apply_code == 0:
    errors.append("missing-worker stack apply unexpectedly succeeded")
if missing_audit.get("status") != "failed":
    errors.append("missing-worker stack audit must fail")
if missing_fanout.get("status") != "failed":
    errors.append("missing-worker fanout status must be failed")
summary = missing_fanout.get("summary", {})
if summary.get("targetCount") != 3 or summary.get("succeeded") != 2 or summary.get("missingReceipts", 0) < 1:
    errors.append(f"missing-worker fanout summary wrong: {summary}")
if marker_count(missing_marker, "fanout-missing") != 2:
    errors.append("missing-worker marker must be written by exactly two live workers")

status = "succeeded" if not errors else "failed"
metadata = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": __import__("datetime").datetime.utcnow().replace(microsecond=0).isoformat() + "Z",
    "labProfiles": ["local.nats.jetstream", "stack.fleet-targeted-fanout", "agent.worker-identity-receipts"],
}
(run_dir / "metadata.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "target-snapshot.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "nats/local-jetstream", "type": "nats-jetstream", "url": nats_url, "stream": stream_name},
        *[
            {"id": f"worker/{agent}", "type": "torque-agent-nats-worker", "agentId": agent, "targetId": target, "subject": subject}
            for agent, target, subject in zip(agents, targets, subjects)
        ],
    ],
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "decision.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "accept",
    "status": status,
    "reason": "targeted NATS fan-out receipts verified" if not errors else "; ".join(errors),
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "verification" / "receipt.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "errors": errors,
    "passFanout": pass_fanout.get("summary"),
    "missingFanout": missing_fanout.get("summary"),
    "missingApplyCode": missing_apply_code,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-007 passed: ${OPS_RUN_DIR}"
