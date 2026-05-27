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

OPS-AGENT-012 proves target-local worker pools and durable slot leases: two
JetStream workers share one target subject, one queue/durable consumer, and one
assignment ledger. The heartbeat advertises workerSlots, stack fan-out reserves
a durable target slot, renews it while a command outlives the original TTL,
receipts carry slot lease metadata, and the slot ledger must block a concurrent
reservation, reclaim an expired reservation, and record released leases. The
first stack apply records which worker executed the
assignment. That worker is then stopped, and a second stack apply must succeed
through the surviving worker without using NATS queue groups as fleet broadcast.
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
slot_ledger_path="${scratch_root}/target-slot-ledger.sqlite"
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
    targetConcurrency:
      enabled: true
      requireAvailable: true
      maxPerTarget: 2
      leaseTTL: 2s
      ledger:
        enabled: true
        store: file
        storePath: "${slot_ledger_path}"
        renewInterval: 500ms
nodes:
  - kind: host.command.run
    name: write-target-local-pool-marker
    host:
      transport: nats
      timeout: 8s
      command: "sleep 3; printf 'target-local-pool\\n' >> ${marker}"
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
  --worker-slots 2 \
  --worker-in-use 1 \
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

ops_log "prove durable slot ledger blocks concurrent target reservation"
python3 - "${slot_ledger_path}" "${target_id}" "${run_token}" <<'PY'
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

path = sys.argv[1]
target_id = sys.argv[2]
run_token = sys.argv[3]
now = datetime.now(timezone.utc)
conn = sqlite3.connect(path)
try:
    conn.execute(
        """
        INSERT INTO target_slot_leases (
          lease_id, tenant, target_id, slot_index, slots, max_slots, holder, run_id,
          node_id, token_digest, status, acquired_at, expires_at, released_at,
          updated_at, store, store_key, store_scope, metadata_json
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        """,
        (
            f"external-held-{run_token}",
            "lab",
            target_id,
            1,
            1,
            1,
            "external-controller",
            "external-run",
            "external-node",
            "sha256:external-held",
            "held",
            now.isoformat().replace("+00:00", "Z"),
            (now + timedelta(minutes=10)).isoformat().replace("+00:00", "Z"),
            None,
            now.isoformat().replace("+00:00", "Z"),
            "file",
            f"external-held-{run_token}",
            path,
            "{}",
        ),
    )
    conn.commit()
finally:
    conn.close()
PY
if TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply-blocked.out" 2>"${OPS_RUN_DIR}/logs/apply-blocked.err"; then
  ops_fail "expected stack apply to block on held target slot lease"
fi
blocked_stack_run_id="$(query_stack_run_id)"
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --run-id "${blocked_stack_run_id}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit-blocked.json"

ops_log "expire held slot lease and prove reclaim through surviving worker"
python3 - "${slot_ledger_path}" "${run_token}" <<'PY'
import sqlite3
import sys
from datetime import datetime, timedelta, timezone

path = sys.argv[1]
run_token = sys.argv[2]
expired = (datetime.now(timezone.utc) - timedelta(seconds=5)).isoformat().replace("+00:00", "Z")
conn = sqlite3.connect(path)
try:
    conn.execute(
        "UPDATE target_slot_leases SET expires_at = ?, updated_at = ? WHERE lease_id = ?",
        (expired, expired, f"external-held-{run_token}"),
    )
    conn.commit()
finally:
    conn.close()
PY
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply-reclaim.out" 2>"${OPS_RUN_DIR}/logs/apply-reclaim.err"
reclaim_stack_run_id="$(query_stack_run_id)"
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --run-id "${reclaim_stack_run_id}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit-reclaim.json"
reclaim_worker_id="$(extract_worker_id "${OPS_RUN_DIR}/verification/audit-reclaim.json")"

ops_log "verify target-local worker pool evidence"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${event_stream}" "${assignment_stream}" "${receipt_stream}" "${marker}" "${agent_id}" "${target_id}" "${subject}" "${worker_queue}" "${worker_a_id}" "${worker_b_id}" "${first_worker_id}" "${survivor_worker_id}" "${second_worker_id}" "${first_stack_run_id}" "${second_stack_run_id}" "${blocked_stack_run_id}" "${reclaim_stack_run_id}" "${reclaim_worker_id}" "${slot_ledger_path}" <<'PY'
import json
import sqlite3
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
blocked_stack_run_id = sys.argv[21]
reclaim_stack_run_id = sys.argv[22]
reclaim_worker_id = sys.argv[23]
slot_ledger_path = Path(sys.argv[24])

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

def duration_matches(value, text, ns):
    return value == text or value == ns

def int_value(value):
    try:
        return int(value)
    except Exception:
        return 0

audit_first = load("verification/audit-first.json")
audit_second = load("verification/audit-second.json")
audit_blocked = load("verification/audit-blocked.json")
audit_reclaim = load("verification/audit-reclaim.json")
registry_status = load("verification/registry-status.json")
fanout_first, result_first = fanout_result(audit_first)
fanout_second, result_second = fanout_result(audit_second)
fanout_blocked = artifact(audit_blocked, "host-command-fanout.json")
fanout_reclaim, result_reclaim = fanout_result(audit_reclaim)
receipt_first = result_first.get("receipt") or {}
receipt_second = result_second.get("receipt") or {}
receipt_reclaim = result_reclaim.get("receipt") or {}
meta_first = receipt_first.get("metadata") or {}
meta_second = receipt_second.get("metadata") or {}
meta_reclaim = receipt_reclaim.get("metadata") or {}
errors = []

marker_count = marker.read_text(encoding="utf-8").count("target-local-pool") if marker.exists() else 0
if registry_status.get("summary", {}).get("ready") != 1:
    errors.append("registry status must have one ready agent")
if audit_first.get("status") != "succeeded":
    errors.append(f"first audit must succeed: {audit_first.get('status')}")
if audit_second.get("status") != "succeeded":
    errors.append(f"second audit must succeed: {audit_second.get('status')}")
if audit_reclaim.get("status") != "succeeded":
    errors.append(f"reclaim audit must succeed: {audit_reclaim.get('status')}")
if fanout_first.get("status") != "succeeded" or fanout_second.get("status") != "succeeded" or fanout_reclaim.get("status") != "succeeded":
    errors.append(f"fanout artifacts must succeed: first={fanout_first} second={fanout_second} reclaim={fanout_reclaim}")
if fanout_blocked.get("status") != "blocked" or "slot ledger blocked" not in (fanout_blocked.get("reason") or ""):
    errors.append(f"blocked fanout must prove held slot ledger block: {fanout_blocked}")
for label, fanout in [("first", fanout_first), ("second", fanout_second), ("reclaim", fanout_reclaim)]:
    if '"slotLeaseToken":' in json.dumps(fanout, sort_keys=True):
        errors.append(f"{label} fanout artifact must redact raw slot lease tokens")
    policy = fanout.get("policy") or {}
    target_concurrency = policy.get("targetConcurrency") or {}
    summary = fanout.get("summary") or {}
    targets = fanout.get("targets") or []
    if not target_concurrency.get("enabled"):
        errors.append(f"{label} fanout must enable targetConcurrency: {policy}")
    if target_concurrency.get("maxPerTarget") != 2:
        errors.append(f"{label} maxPerTarget mismatch: {target_concurrency}")
    if not duration_matches(target_concurrency.get("leaseTTL"), "2s", 2000000000):
        errors.append(f"{label} leaseTTL mismatch: {target_concurrency}")
    ledger_policy = target_concurrency.get("ledger") or {}
    if not duration_matches(ledger_policy.get("renewInterval"), "500ms", 500000000):
        errors.append(f"{label} renewInterval mismatch: {ledger_policy}")
    if summary.get("slotLeases") != 1:
        errors.append(f"{label} summary slotLeases mismatch: {summary}")
    if summary.get("workerSlotsTotal") != 2 or summary.get("workerSlotsAvailable") != 1:
        errors.append(f"{label} worker slot summary mismatch: {summary}")
    if len(targets) != 1:
        errors.append(f"{label} expected one target view, got {len(targets)}")
    else:
        target = targets[0]
        slots = target.get("workerSlots") or {}
        lease = target.get("slotLease") or {}
        if slots.get("total") != 2 or slots.get("inUse") != 1:
            errors.append(f"{label} target workerSlots mismatch: {target}")
        if target.get("workerSlotsAvailable") != 1:
            errors.append(f"{label} target workerSlotsAvailable mismatch: {target}")
        if not lease.get("id") or lease.get("targetId") != target_id:
            errors.append(f"{label} target slotLease mismatch: {target}")
        if lease.get("status") != "released" or not lease.get("releasedAt"):
            errors.append(f"{label} target slotLease must be released: {lease}")
        if lease.get("renewals", 0) < 1 or not lease.get("renewedAt"):
            errors.append(f"{label} target slotLease must prove renewal: {lease}")
        if lease.get("ledgerStore") != "file" or not lease.get("ledgerTokenDigest"):
            errors.append(f"{label} target slotLedger evidence missing: {lease}")
        if label == "reclaim" and lease.get("reclaimed") != 1:
            errors.append(f"{label} target slotLease must prove one reclaimed lease: {lease}")
if marker_count != 3:
    errors.append(f"marker must be written exactly three times, got {marker_count}")
if first_worker_id not in {worker_a_id, worker_b_id}:
    errors.append(f"first worker {first_worker_id} not in pool")
if second_worker_id != survivor_worker_id:
    errors.append(f"second worker {second_worker_id} must equal survivor {survivor_worker_id}")
if first_worker_id == second_worker_id:
    errors.append(f"second run must move to surviving worker, got {second_worker_id}")
if reclaim_worker_id != survivor_worker_id:
    errors.append(f"reclaim worker {reclaim_worker_id} must equal survivor {survivor_worker_id}")

for label, result, receipt, metadata, expected_worker in [
    ("first", result_first, receipt_first, meta_first, first_worker_id),
    ("second", result_second, receipt_second, meta_second, second_worker_id),
    ("reclaim", result_reclaim, receipt_reclaim, meta_reclaim, reclaim_worker_id),
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
    lease = result.get("slotLease") or {}
    assignment = result.get("assignment") or {}
    if not lease.get("id"):
        errors.append(f"{label} missing result slotLease: {result}")
    if lease.get("status") != "released" or not lease.get("releasedAt"):
        errors.append(f"{label} result slotLease must be released: {lease}")
    if lease.get("renewals", 0) < 1 or not lease.get("renewedAt"):
        errors.append(f"{label} result slotLease must prove renewal: {lease}")
    if label == "reclaim" and lease.get("reclaimed") != 1:
        errors.append(f"{label} result slotLease must prove one reclaimed lease: {lease}")
    if assignment.get("slotLeaseId") != lease.get("id"):
        errors.append(f"{label} assignment slotLeaseId mismatch: assignment={assignment} lease={lease}")
    if assignment.get("slotLeaseToken"):
        errors.append(f"{label} assignment artifact leaked raw slot lease token: {assignment}")
    if assignment.get("slotLeaseTokenDigest") != lease.get("ledgerTokenDigest"):
        errors.append(f"{label} assignment slot lease token digest mismatch: assignment={assignment} lease={lease}")
    if assignment.get("slotLeaseLedgerStore") != "file" or not assignment.get("slotLeaseLedgerStorePath"):
        errors.append(f"{label} assignment slot lease ledger grant missing: {assignment}")
    if metadata.get("slotLeaseId") != lease.get("id"):
        errors.append(f"{label} receipt slotLeaseId mismatch: {metadata} lease={lease}")
    if metadata.get("slotLeaseTargetId") != target_id:
        errors.append(f"{label} receipt slotLeaseTargetId mismatch: {metadata}")
    if metadata.get("slotLeaseIndex") != "1" or metadata.get("slotLeaseSlots") != "1":
        errors.append(f"{label} receipt slot lease cardinality mismatch: {metadata}")
    if metadata.get("slotLeaseDecision") != "accepted":
        errors.append(f"{label} receipt must prove worker accepted slot lease: {metadata}")
    if metadata.get("slotLeaseGrant") != "true" or metadata.get("slotLeaseGrantRedacted") != "true":
        errors.append(f"{label} receipt must prove redacted worker slot lease grant: {metadata}")
    if metadata.get("slotLeaseTokenDigest") != lease.get("ledgerTokenDigest") or metadata.get("slotLeaseGrantDigest") != lease.get("ledgerTokenDigest"):
        errors.append(f"{label} receipt slot lease grant digest mismatch: {metadata} lease={lease}")
    if metadata.get("slotLeaseRenewedBy") != "worker" or metadata.get("slotLeaseWorkerReleased") != "true":
        errors.append(f"{label} receipt must prove worker-owned slot lease renew/release: {metadata}")
    if int_value(metadata.get("slotLeaseWorkerRenewals")) < 1:
        errors.append(f"{label} receipt must prove worker slot lease renewal count: {metadata}")

if not slot_ledger_path.exists():
    errors.append(f"slot ledger not found: {slot_ledger_path}")
else:
    conn = sqlite3.connect(slot_ledger_path)
    try:
        rows = conn.execute("SELECT status, COUNT(*) FROM target_slot_leases GROUP BY status").fetchall()
        counts = {status: count for status, count in rows}
        held = conn.execute("SELECT COUNT(*) FROM target_slot_leases WHERE status='held'").fetchone()[0]
    finally:
        conn.close()
    if held != 0:
        errors.append(f"slot ledger must not leave held leases, held={held}")
    if counts.get("released", 0) < 3:
        errors.append(f"slot ledger must record released stack leases: {counts}")
    if counts.get("expired", 0) < 1:
        errors.append(f"slot ledger must record one reclaimed expired lease: {counts}")

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
        "stack.fleet-jetstream-target-capacity-slot-lease",
        "stack.fleet-jetstream-target-slot-ledger",
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
        {"id": "slot-ledger/local-sqlite", "type": "sqlite", "path": str(slot_ledger_path)},
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
    "reason": "target-local worker pool and durable slot ledger verified" if not errors else "; ".join(errors),
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
    "blockedStackRunId": blocked_stack_run_id,
    "reclaimStackRunId": reclaim_stack_run_id,
    "firstWorkerId": first_worker_id,
    "secondWorkerId": second_worker_id,
    "reclaimWorkerId": reclaim_worker_id,
    "survivorWorkerId": survivor_worker_id,
    "queue": worker_queue,
    "targetConcurrency": {"enabled": True, "maxPerTarget": 2, "leaseTTL": "2s", "ledger": {"enabled": True, "store": "file", "renewInterval": "500ms"}},
    "markerCount": marker_count,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-012 passed: ${OPS_RUN_DIR}"
