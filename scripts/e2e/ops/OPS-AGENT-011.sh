#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-011.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-011 proves durable receipt offset resume: run a JetStream-backed
fleet stack once, checkpoint the receipt offset into .torque/stack/state.sqlite,
stop the worker, mark the first run as interrupted, then resume that run. The
resume must succeed from the stored TORQUE_RECEIPTS offset without publishing
new work or mutating the marker again.
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
ops_init_run "OPS-AGENT-011"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-011.XXXXXX")"
stack_dir="${scratch_root}/stack"
registry_path="${scratch_root}/agent-registry.json"
marker="${scratch_root}/resume-marker.txt"
nats_pid=""
nats_url="${external_nats_url}"
worker_pid=""
cleanup_status="pending"
agent_id="agent-worker-resume-01"
target_id="host/mysql-resume-01"
subject="torque.assign.lab.host_mysql-resume-01"
event_stream="TORQUE_AGENT_EVENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
assignment_stream="TORQUE_ASSIGNMENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
receipt_stream="TORQUE_RECEIPTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
registry_durable="torque-ops-agent-011-${OPS_RUN_ID//[^A-Za-z0-9]/}"
worker_durable="torque-worker-resume-${OPS_RUN_ID//[^A-Za-z0-9]/}"
ledger_path="${scratch_root}/agent-assignments.sqlite"
node_id="host.command.run/write-resume-marker"

finish() {
  local code=$?
  trap - EXIT
  set +e
  if [[ -n "${worker_pid}" ]]; then
    kill "${worker_pid}" 2>/dev/null
    wait "${worker_pid}" 2>/dev/null
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
name: durable-receipt-resume
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
    name: write-resume-marker
    host:
      transport: nats
      timeout: 8s
      command: "printf 'receipt-resume\\n' >> ${marker}"
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

mark_run_interrupted() {
  python3 - "${stack_dir}/.torque/stack/state.sqlite" "$1" "${node_id}" <<'PY'
import sqlite3
import sys

db_path, run_id, node_id = sys.argv[1:4]
db = sqlite3.connect(db_path)
try:
    db.execute(
        "UPDATE torque_stack_nodes SET status = 'failed', error = 'simulated controller restart' WHERE run_id = ? AND node_id = ?",
        (run_id, node_id),
    )
    db.execute("UPDATE torque_stack_runs SET status = 'failed' WHERE run_id = ?", (run_id,))
    db.commit()
finally:
    db.close()
PY
}

dump_receipt_offsets() {
  python3 - "${stack_dir}/.torque/stack/state.sqlite" "$1" "${node_id}" <<'PY'
import json
import sqlite3
import sys

db_path, run_id, node_id = sys.argv[1:4]
db = sqlite3.connect(db_path)
db.row_factory = sqlite3.Row
try:
    rows = db.execute(
        """
        SELECT run_id, receipt_run_id, node_id, target_id, assignment_id,
               agent_id, worker_subject, receipt_stream, subject, consumer,
               sequence, receipt_sha256
        FROM torque_stack_receipt_offsets
        WHERE run_id = ? AND node_id = ?
        ORDER BY sequence ASC
        """,
        (run_id, node_id),
    ).fetchall()
    print(json.dumps([dict(row) for row in rows], indent=2, sort_keys=True))
finally:
    db.close()
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
  --hostname worker-resume-01 \
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

ops_log "start JetStream worker for the first apply"
"${repo_root}/bin/torque-agent" nats worker \
  --nats-url "${nats_url}" \
  --delivery jetstream \
  --assignment-stream "${assignment_stream}" \
  --receipt-stream "${receipt_stream}" \
  --durable "${worker_durable}" \
  --ledger-path "${ledger_path}" \
  --subject "${subject}" \
  --agent-id "${agent_id}" \
  --tenant lab \
  --target-id "${target_id}" \
  --hostname worker-resume-01 \
  --capability host.command.run \
  >"${OPS_RUN_DIR}/logs/worker.log" 2>&1 &
worker_pid="$!"
wait_for_worker "${worker_pid}" "${OPS_RUN_DIR}/logs/worker.log"

ops_log "run first stack apply and checkpoint receipt offset"
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply-first.out" 2>"${OPS_RUN_DIR}/logs/apply-first.err"
first_stack_run_id="$(query_stack_run_id)"
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --run-id "${first_stack_run_id}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit-first.json"
dump_receipt_offsets "${first_stack_run_id}" >"${OPS_RUN_DIR}/verification/receipt-offsets-first.json"

ops_log "stop worker and mark first run interrupted"
kill "${worker_pid}" 2>/dev/null
wait "${worker_pid}" 2>/dev/null || true
worker_pid=""
mark_run_interrupted "${first_stack_run_id}"

ops_log "resume from stored receipt offset with no worker running"
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes --resume --run-id "${first_stack_run_id}" \
  >"${OPS_RUN_DIR}/logs/apply-resume.out" 2>"${OPS_RUN_DIR}/logs/apply-resume.err"
resume_stack_run_id="$(query_stack_run_id)"
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --run-id "${resume_stack_run_id}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit-resume.json"
dump_receipt_offsets "${resume_stack_run_id}" >"${OPS_RUN_DIR}/verification/receipt-offsets-resume.json"
"${repo_root}/bin/torque" stack export --config "${stack_dir}" --run-id "${resume_stack_run_id}" --out "${OPS_RUN_DIR}/stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/export.out" 2>"${OPS_RUN_DIR}/logs/export.err"

ops_log "verify receipt resume evidence"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${event_stream}" "${assignment_stream}" "${receipt_stream}" "${marker}" "${agent_id}" "${target_id}" "${subject}" "${first_stack_run_id}" "${resume_stack_run_id}" <<'PY'
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
first_stack_run_id = sys.argv[13]
resume_stack_run_id = sys.argv[14]

def load(path):
    with (run_dir / path).open("r", encoding="utf-8") as f:
        return json.load(f)

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            try:
                return json.loads(item.get("body", "{}"))
            except json.JSONDecodeError:
                return {}
    return {}

audit_first = load("verification/audit-first.json")
audit_resume = load("verification/audit-resume.json")
registry_status = load("verification/registry-status.json")
offsets_first = load("verification/receipt-offsets-first.json")
offsets_resume = load("verification/receipt-offsets-resume.json")
fanout_first = artifact(audit_first, "host-command-fanout.json")
fanout_resume = artifact(audit_resume, "host-command-fanout.json")
errors = []

marker_count = marker.read_text(encoding="utf-8").count("receipt-resume") if marker.exists() else 0
if registry_status.get("summary", {}).get("ready") != 1:
    errors.append("registry status must have one ready agent")
if audit_first.get("status") != "succeeded":
    errors.append(f"first audit must succeed: {audit_first.get('status')}")
if audit_resume.get("status") != "succeeded":
    errors.append(f"resume audit must succeed: {audit_resume.get('status')}")
if marker_count != 1:
    errors.append(f"marker must be written exactly once, got {marker_count}")
if len(offsets_first) != 1:
    errors.append(f"expected one first receipt offset, got {offsets_first}")
if len(offsets_resume) != 1:
    errors.append(f"expected one resume receipt offset, got {offsets_resume}")
if offsets_first and offsets_first[0].get("sequence", 0) <= 0:
    errors.append(f"first receipt offset missing sequence: {offsets_first}")
if offsets_resume and offsets_resume[0].get("receipt_run_id") != first_stack_run_id:
    errors.append(f"resume receipt offset must point at first run: {offsets_resume}")

if fanout_first.get("status") != "succeeded":
    errors.append(f"first fanout must succeed: {fanout_first}")
if fanout_resume.get("status") != "succeeded":
    errors.append(f"resume fanout must succeed: {fanout_resume}")
if fanout_resume.get("receiptRunId") != first_stack_run_id:
    errors.append(f"resume fanout receiptRunId mismatch: {fanout_resume.get('receiptRunId')}")
if fanout_resume.get("resumeFromRunId") != first_stack_run_id:
    errors.append(f"resume fanout resumeFromRunId mismatch: {fanout_resume.get('resumeFromRunId')}")

results = fanout_resume.get("results") or []
if len(results) != 1:
    errors.append(f"resume fanout expected one result, got {len(results)}")
else:
    result = results[0]
    receipt = result.get("receipt") or {}
    metadata = receipt.get("metadata") or {}
    offset = result.get("receiptOffset") or {}
    if result.get("targetId") != target_id or result.get("agentId") != agent_id:
        errors.append(f"resume result identity mismatch: {result}")
    if result.get("workerSubject") != subject:
        errors.append(f"resume worker subject mismatch: {result.get('workerSubject')}")
    if metadata.get("receiptOffsetResumed") != "true":
        errors.append(f"resume receipt did not mark offset resume: {metadata}")
    if metadata.get("resumeFromRunId") != first_stack_run_id:
        errors.append(f"resume metadata run mismatch: {metadata}")
    if offset.get("stream") != receipt_stream or not offset.get("sequence"):
        errors.append(f"resume receipt offset missing stream sequence: {offset}")

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
        "stack.fleet-jetstream-receipt-offset-resume",
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
        {"id": f"worker/{agent_id}", "type": "torque-agent-nats-worker", "agentId": agent_id, "targetId": target_id, "subject": subject},
    ],
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "decision.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "accept",
    "status": status,
    "reason": "durable receipt offset resume verified" if not errors else "; ".join(errors),
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "verification" / "receipt.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "errors": errors,
    "firstStackRunId": first_stack_run_id,
    "resumeStackRunId": resume_stack_run_id,
    "markerCount": marker_count,
    "receiptOffsetsFirst": offsets_first,
    "receiptOffsetsResume": offsets_resume,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-011 passed: ${OPS_RUN_DIR}"
