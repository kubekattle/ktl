#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-012.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-012 proves target-local worker pools: two JetStream workers share one
target subject, one queue/durable consumer, and one assignment ledger. The first
stack apply records which worker executed the assignment. That worker is then
stopped, and a second stack apply must succeed through the surviving worker
without using NATS queue groups as fleet broadcast.
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
ops_init_run "OPS-AGENT-012"
started_at="$(ops_utc_now)"
run_token="${OPS_RUN_ID//[^A-Za-z0-9]/}"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-012.XXXXXX")"
stack_dir="${scratch_root}/stack"
registry_path="${scratch_root}/agent-registry.json"
marker="${scratch_root}/target-local-pool-marker.txt"
nats_pid=""
nats_url="${external_nats_url}"
worker_a_pid=""
worker_b_pid=""
cleanup_status="pending"
agent_id="agent-worker-pool-01"
worker_a_id="pool-worker-a"
worker_b_id="pool-worker-b"
target_id="host/mysql-pool-01"
subject="torque.assign.lab.host_mysql-pool-01"
worker_queue="target-pool-${run_token}"
event_stream="TORQUE_AGENT_EVENTS_${run_token}"
assignment_stream="TORQUE_ASSIGNMENTS_${run_token}"
receipt_stream="TORQUE_RECEIPTS_${run_token}"
registry_durable="torque-ops-agent-012-${run_token}"
ledger_path="${scratch_root}/agent-assignments.sqlite"

finish() {
  local code=$?
  trap - EXIT
  set +e
  if [[ -n "${worker_a_pid}" ]]; then
    kill "${worker_a_pid}" 2>/dev/null
    wait "${worker_a_pid}" 2>/dev/null
  fi
  if [[ -n "${worker_b_pid}" ]]; then
    kill "${worker_b_pid}" 2>/dev/null
    wait "${worker_b_pid}" 2>/dev/null
  fi
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
  mkdir -p "${stack_dir}"
  cat >"${stack_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: target-local-worker-pool
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
    delivery: jetstream
    maxParallel: 1
    maxFailed: 0
    minSucceededPercent: 100
    onPartialFailure: block
nodes:
  - kind: host.command.run
    name: write-target-local-pool-marker
    host:
      transport: nats
      timeout: 8s
      command: "printf 'target-local-pool\\n' >> ${marker}"
YAML
}

query_stack_run_id() {
  python3 - "${stack_dir}/.torque/stack/state.sqlite" <<'PY'
import sqlite3
import sys

db = sqlite3.connect(sys.argv[1])
try:
    row = db.execute("SELECT run_id FROM torque_stack_runs ORDER BY created_at_ns DESC LIMIT 1").fetchone()
    if not row:
        raise SystemExit("no stack runs found")
    print(row[0])
finally:
    db.close()
PY
}

extract_worker_id() {
  python3 - "$1" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as f:
    audit = json.load(f)
for item in audit.get("artifacts", []):
    if item.get("name") != "host-command-fanout.json":
        continue
    fanout = json.loads(item.get("body", "{}"))
    results = fanout.get("results") or []
    if not results:
        raise SystemExit("fanout has no results")
    receipt = results[0].get("receipt") or {}
    metadata = receipt.get("metadata") or {}
    worker_id = metadata.get("workerId")
    if not worker_id:
        raise SystemExit(f"receipt missing workerId: {metadata}")
    print(worker_id)
    sys.exit(0)
raise SystemExit("host-command-fanout.json not found")
PY
}

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/logs" "${OPS_RUN_DIR}/verification" "${stack_dir}"

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
write_stack

ops_log "publish heartbeat and compact registry"
"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --jetstream \
  --stream "${event_stream}" \
  --once \
  --discover-capabilities=false \
  --agent-id "${agent_id}" \
  --tenant lab \
  --target-id "${target_id}" \
  --hostname worker-pool-01 \
  --label role=mysql \
  --capability host.command.run \
  >"${OPS_RUN_DIR}/logs/heartbeat.json" 2>"${OPS_RUN_DIR}/logs/heartbeat.err"
"${repo_root}/bin/torque" ops agent registry compact \
  --nats-url "${nats_url}" \
  --tenant lab \
  --stream "${event_stream}" \
  --durable "${registry_durable}" \
  --store file \
  --store-path "${registry_path}" \
  --max-messages 1 \
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

ops_log "start two JetStream workers for one target-local pool"
"${repo_root}/bin/torque-agent" nats worker \
  --nats-url "${nats_url}" \
  --delivery jetstream \
  --assignment-stream "${assignment_stream}" \
  --receipt-stream "${receipt_stream}" \
  --queue "${worker_queue}" \
  --ledger-path "${ledger_path}" \
  --subject "${subject}" \
  --agent-id "${agent_id}" \
  --worker-id "${worker_a_id}" \
  --tenant lab \
  --target-id "${target_id}" \
  --hostname worker-pool-01 \
  --capability host.command.run \
  >"${OPS_RUN_DIR}/logs/worker-a.log" 2>&1 &
worker_a_pid="$!"
wait_for_worker "${worker_a_pid}" "${OPS_RUN_DIR}/logs/worker-a.log"

"${repo_root}/bin/torque-agent" nats worker \
  --nats-url "${nats_url}" \
  --delivery jetstream \
  --assignment-stream "${assignment_stream}" \
  --receipt-stream "${receipt_stream}" \
  --queue "${worker_queue}" \
  --ledger-path "${ledger_path}" \
  --subject "${subject}" \
  --agent-id "${agent_id}" \
  --worker-id "${worker_b_id}" \
  --tenant lab \
  --target-id "${target_id}" \
  --hostname worker-pool-01 \
  --capability host.command.run \
  >"${OPS_RUN_DIR}/logs/worker-b.log" 2>&1 &
worker_b_pid="$!"
wait_for_worker "${worker_b_pid}" "${OPS_RUN_DIR}/logs/worker-b.log"

ops_log "run first stack apply through one pool worker"
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply-first.out" 2>"${OPS_RUN_DIR}/logs/apply-first.err"
first_stack_run_id="$(query_stack_run_id)"
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --run-id "${first_stack_run_id}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit-first.json"
first_worker_id="$(extract_worker_id "${OPS_RUN_DIR}/verification/audit-first.json")"

case "${first_worker_id}" in
  "${worker_a_id}")
    ops_log "stop first executor ${worker_a_id}; keep ${worker_b_id}"
    kill "${worker_a_pid}" 2>/dev/null
    wait "${worker_a_pid}" 2>/dev/null || true
    worker_a_pid=""
    survivor_worker_id="${worker_b_id}"
    ;;
  "${worker_b_id}")
    ops_log "stop first executor ${worker_b_id}; keep ${worker_a_id}"
    kill "${worker_b_pid}" 2>/dev/null
    wait "${worker_b_pid}" 2>/dev/null || true
    worker_b_pid=""
    survivor_worker_id="${worker_a_id}"
    ;;
  *)
    ops_fail "unexpected first workerId: ${first_worker_id}"
    ;;
esac

ops_log "run second stack apply through surviving pool worker"
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply-second.out" 2>"${OPS_RUN_DIR}/logs/apply-second.err"
second_stack_run_id="$(query_stack_run_id)"
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --run-id "${second_stack_run_id}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit-second.json"
second_worker_id="$(extract_worker_id "${OPS_RUN_DIR}/verification/audit-second.json")"

ops_log "verify target-local worker pool evidence"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${event_stream}" "${assignment_stream}" "${receipt_stream}" "${marker}" "${agent_id}" "${target_id}" "${subject}" "${worker_queue}" "${worker_a_id}" "${worker_b_id}" "${first_worker_id}" "${survivor_worker_id}" "${second_worker_id}" "${first_stack_run_id}" "${second_stack_run_id}" <<'PY'
import json
import sys
from datetime import datetime
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
nats_url = sys.argv[5]
event_stream = sys.argv[6]
assignment_stream = sys.argv[7]
receipt_stream = sys.argv[8]
marker = Path(sys.argv[9])
agent_id = sys.argv[10]
target_id = sys.argv[11]
subject = sys.argv[12]
worker_queue = sys.argv[13]
worker_a_id = sys.argv[14]
worker_b_id = sys.argv[15]
first_worker_id = sys.argv[16]
survivor_worker_id = sys.argv[17]
second_worker_id = sys.argv[18]
first_stack_run_id = sys.argv[19]
second_stack_run_id = sys.argv[20]

def load(path):
    with (run_dir / path).open("r", encoding="utf-8") as f:
        return json.load(f)

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            return json.loads(item.get("body", "{}"))
    return {}

def fanout_result(audit):
    fanout = artifact(audit, "host-command-fanout.json")
    results = fanout.get("results") or []
    return fanout, results[0] if results else {}

audit_first = load("verification/audit-first.json")
audit_second = load("verification/audit-second.json")
registry_status = load("verification/registry-status.json")
fanout_first, result_first = fanout_result(audit_first)
fanout_second, result_second = fanout_result(audit_second)
receipt_first = result_first.get("receipt") or {}
receipt_second = result_second.get("receipt") or {}
meta_first = receipt_first.get("metadata") or {}
meta_second = receipt_second.get("metadata") or {}
errors = []

marker_count = marker.read_text(encoding="utf-8").count("target-local-pool") if marker.exists() else 0
if registry_status.get("summary", {}).get("ready") != 1:
    errors.append("registry status must have one ready agent")
if audit_first.get("status") != "succeeded":
    errors.append(f"first audit must succeed: {audit_first.get('status')}")
if audit_second.get("status") != "succeeded":
    errors.append(f"second audit must succeed: {audit_second.get('status')}")
if fanout_first.get("status") != "succeeded" or fanout_second.get("status") != "succeeded":
    errors.append(f"fanout artifacts must succeed: first={fanout_first} second={fanout_second}")
if marker_count != 2:
    errors.append(f"marker must be written exactly twice, got {marker_count}")
if first_worker_id not in {worker_a_id, worker_b_id}:
    errors.append(f"first worker {first_worker_id} not in pool")
if second_worker_id != survivor_worker_id:
    errors.append(f"second worker {second_worker_id} must equal survivor {survivor_worker_id}")
if first_worker_id == second_worker_id:
    errors.append(f"second run must move to surviving worker, got {second_worker_id}")

for label, result, receipt, metadata, expected_worker in [
    ("first", result_first, receipt_first, meta_first, first_worker_id),
    ("second", result_second, receipt_second, meta_second, second_worker_id),
]:
    if result.get("targetId") != target_id or result.get("agentId") != agent_id:
        errors.append(f"{label} result identity mismatch: {result}")
    if result.get("workerSubject") != subject:
        errors.append(f"{label} worker subject mismatch: {result.get('workerSubject')}")
    if receipt.get("status") != "succeeded":
        errors.append(f"{label} receipt must succeed: {receipt}")
    if metadata.get("workerId") != expected_worker:
        errors.append(f"{label} workerId mismatch: {metadata}")
    if metadata.get("agentId") != agent_id or metadata.get("targetId") != target_id:
        errors.append(f"{label} metadata identity mismatch: {metadata}")
    if metadata.get("queue") != worker_queue:
        errors.append(f"{label} queue metadata mismatch: {metadata}")
    if metadata.get("assignmentConsumer") != worker_queue:
        errors.append(f"{label} assignmentConsumer mismatch: {metadata}")
    if metadata.get("workerDecision") != "executed":
        errors.append(f"{label} workerDecision mismatch: {metadata}")
    if not metadata.get("assignmentId"):
        errors.append(f"{label} missing assignmentId: {metadata}")

status = "succeeded" if not errors else "failed"
metadata_doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": datetime.utcnow().replace(microsecond=0).isoformat() + "Z",
    "labProfiles": [
        "local.nats.jetstream",
        "stack.fleet-jetstream-target-local-worker-pool",
        "agent.assignment-idempotency-ledger",
    ],
}
(run_dir / "metadata.json").write_text(json.dumps(metadata_doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "target-snapshot.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "nats/local-jetstream", "type": "nats-jetstream", "url": nats_url, "streams": [event_stream, assignment_stream, receipt_stream]},
        {"id": f"worker/{worker_a_id}", "type": "torque-agent-nats-worker", "agentId": agent_id, "workerId": worker_a_id, "targetId": target_id, "subject": subject, "queue": worker_queue},
        {"id": f"worker/{worker_b_id}", "type": "torque-agent-nats-worker", "agentId": agent_id, "workerId": worker_b_id, "targetId": target_id, "subject": subject, "queue": worker_queue},
    ],
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "decision.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "accept",
    "status": status,
    "reason": "target-local worker pool verified" if not errors else "; ".join(errors),
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "verification" / "receipt.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "errors": errors,
    "firstStackRunId": first_stack_run_id,
    "secondStackRunId": second_stack_run_id,
    "firstWorkerId": first_worker_id,
    "secondWorkerId": second_worker_id,
    "survivorWorkerId": survivor_worker_id,
    "queue": worker_queue,
    "markerCount": marker_count,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-012 passed: ${OPS_RUN_DIR}"
