#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-008.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-008 proves JetStream durable assignments: start stack apply while the
target worker is offline, publish a durable assignment to TORQUE_ASSIGNMENTS,
start the worker later, then prove the marker is written and
host-command-fanout.json preserves assignment and receipt stream offsets. It
then republishes the same assignment and proves the worker returns a deduped
ledger receipt without executing the command a second time.
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
ops_init_run "OPS-AGENT-008"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-008.XXXXXX")"
stack_dir="${scratch_root}/stack"
registry_path="${scratch_root}/agent-registry.json"
marker="${scratch_root}/durable-marker.txt"
nats_pid=""
nats_url="${external_nats_url}"
worker_pid=""
apply_pid=""
apply_code=0
cleanup_status="pending"
agent_id="agent-worker-durable-01"
target_id="host/mysql-durable-01"
subject="torque.assign.lab.host_mysql-durable-01"
event_stream="TORQUE_AGENT_EVENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
assignment_stream="TORQUE_ASSIGNMENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
receipt_stream="TORQUE_RECEIPTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
registry_durable="torque-ops-agent-008-${OPS_RUN_ID//[^A-Za-z0-9]/}"
worker_durable="torque-worker-durable-${OPS_RUN_ID//[^A-Za-z0-9]/}"
ledger_path="${scratch_root}/agent-assignments.sqlite"

finish() {
  local code=$?
  trap - EXIT
  set +e
  if [[ -n "${apply_pid}" ]]; then
    kill "${apply_pid}" 2>/dev/null
    wait "${apply_pid}" 2>/dev/null
  fi
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
name: durable-jetstream-assignment
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
    name: write-durable-marker
    host:
      transport: nats
      timeout: 20s
      command: "printf 'durable-jetstream\\n' >> ${marker}"
YAML
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
  --hostname worker-durable-01 \
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

write_stack

ops_log "start stack apply while JetStream worker is offline"
set +e
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply.out" 2>"${OPS_RUN_DIR}/logs/apply.err" &
apply_pid="$!"
set -e

sleep 2
if ! kill -0 "${apply_pid}" 2>/dev/null; then
  wait "${apply_pid}" || apply_code="$?"
  ops_fail "stack apply exited before worker start with code ${apply_code}; see ${OPS_RUN_DIR}/logs/apply.err"
fi

ops_log "start JetStream worker after assignment publication window"
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
  --hostname worker-durable-01 \
  --capability host.command.run \
  >"${OPS_RUN_DIR}/logs/worker.log" 2>&1 &
worker_pid="$!"
wait_for_worker "${worker_pid}" "${OPS_RUN_DIR}/logs/worker.log"

set +e
wait "${apply_pid}"
apply_code="$?"
apply_pid=""
set -e

"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit.json"
"${repo_root}/bin/torque" stack export --config "${stack_dir}" --out "${OPS_RUN_DIR}/stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/export.out" 2>"${OPS_RUN_DIR}/logs/export.err"

ops_log "republish duplicate assignment and prove worker dedupes it"
duplicate_receipt_subject="$(
python3 - "${OPS_RUN_DIR}/verification/audit.json" "${scratch_root}/duplicate-assignment.json" <<'PY'
import json
import re
import sys
from pathlib import Path

audit_path = Path(sys.argv[1])
assignment_path = Path(sys.argv[2])
audit = json.loads(audit_path.read_text(encoding="utf-8"))

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            return json.loads(item.get("body", "{}"))
    return {}

def token(value, fallback):
    value = (value or "").strip() or fallback
    value = re.sub(r"[^A-Za-z0-9_-]+", "_", value).strip("_")
    return value or fallback

fanout = artifact(audit, "host-command-fanout.json")
assignment = (fanout.get("results") or [{}])[0].get("assignment") or {}
assignment_path.write_text(json.dumps(assignment, sort_keys=True) + "\n", encoding="utf-8")
print("torque.receipt.lab." + token(assignment.get("runId"), "run") + "." + token(assignment.get("targetId"), "target"))
PY
)"
cat >"${scratch_root}/duplicate-probe.go" <<'GO'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	nats "github.com/nats-io/nats.go"
)

func main() {
	if len(os.Args) != 7 {
		fmt.Fprintf(os.Stderr, "usage: duplicate-probe <nats-url> <assignment-file> <receipt-subject> <receipt-stream> <durable> <timeout>\n")
		os.Exit(2)
	}
	natsURL := os.Args[1]
	assignmentFile := os.Args[2]
	receiptSubject := os.Args[3]
	receiptStream := os.Args[4]
	durable := os.Args[5]
	timeout, err := time.ParseDuration(os.Args[6])
	if err != nil {
		panic(err)
	}
	raw, err := os.ReadFile(assignmentFile)
	if err != nil {
		panic(err)
	}
	var assignment struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(raw, &assignment); err != nil {
		panic(err)
	}
	if assignment.Target == "" {
		panic("assignment target is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	conn, err := nats.Connect(natsURL, nats.Name("torque-ops-agent-008-duplicate-probe"), nats.Timeout(timeout))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	js, err := conn.JetStream(nats.MaxWait(timeout))
	if err != nil {
		panic(err)
	}
	sub, err := js.PullSubscribe(
		receiptSubject,
		durable,
		nats.BindStream(receiptStream),
		nats.DeliverNew(),
		nats.AckExplicit(),
		nats.ManualAck(),
	)
	if err != nil {
		panic(err)
	}
	if _, err := js.Publish(assignment.Target, raw, nats.Context(ctx)); err != nil {
		panic(err)
	}
	msgs, err := sub.Fetch(1, nats.MaxWait(timeout))
	if err != nil {
		panic(err)
	}
	if len(msgs) != 1 {
		panic("expected one duplicate receipt")
	}
	fmt.Println(string(msgs[0].Data))
	_ = msgs[0].Ack(nats.Context(ctx))
}
GO
go run "${scratch_root}/duplicate-probe.go" \
  "${nats_url}" \
  "${scratch_root}/duplicate-assignment.json" \
  "${duplicate_receipt_subject}" \
  "${receipt_stream}" \
  "torque-ops-agent-008-duplicate-${OPS_RUN_ID//[^A-Za-z0-9]/}" \
  10s \
  >"${OPS_RUN_DIR}/verification/duplicate-receipt.json" \
  2>"${OPS_RUN_DIR}/logs/duplicate-probe.err"

ops_log "verify durable assignment receipts"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${event_stream}" "${assignment_stream}" "${receipt_stream}" "${marker}" "${apply_code}" "${agent_id}" "${target_id}" "${subject}" <<'PY'
import json
import sys
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
apply_code = int(sys.argv[10])
agent_id = sys.argv[11]
target_id = sys.argv[12]
subject = sys.argv[13]

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

audit = load("verification/audit.json")
registry_status = load("verification/registry-status.json")
duplicate_receipt = load("verification/duplicate-receipt.json")
fanout = artifact(audit, "host-command-fanout.json")
execute = artifact(audit, "host-command-execute.json")
errors = []

if apply_code != 0:
    errors.append(f"stack apply failed with code {apply_code}")
if registry_status.get("summary", {}).get("ready") != 1:
    errors.append("registry status must have one ready agent")
if audit.get("status") != "succeeded":
    errors.append("stack audit must succeed")
if fanout.get("status") != "succeeded":
    errors.append(f"fanout status must succeed: {fanout.get('status')}")
if fanout.get("policy", {}).get("delivery") != "jetstream":
    errors.append(f"fanout policy delivery must be jetstream: {fanout.get('policy')}")
summary = fanout.get("summary", {})
if summary.get("targetCount") != 1 or summary.get("succeeded") != 1:
    errors.append(f"fanout summary wrong: {summary}")
if execute.get("metadata", {}).get("delivery") != "jetstream":
    errors.append("execute receipt must identify jetstream delivery")
if marker_count(marker, "durable-jetstream") != 1:
    errors.append("durable marker must be written exactly once")
duplicate_metadata = duplicate_receipt.get("metadata", {})
if duplicate_receipt.get("status") != "succeeded":
    errors.append(f"duplicate receipt must preserve succeeded status: {duplicate_receipt}")
if duplicate_metadata.get("deduped") != "true" or duplicate_metadata.get("replayedReceipt") != "true":
    errors.append(f"duplicate receipt must prove ledger replay: {duplicate_metadata}")
if duplicate_metadata.get("workerDecision") != "deduped":
    errors.append(f"duplicate worker decision must be deduped: {duplicate_metadata}")

results = fanout.get("results", [])
if len(results) != 1:
    errors.append(f"expected one fanout result, got {len(results)}")
else:
    result = results[0]
    receipt = result.get("receipt", {})
    metadata = receipt.get("metadata", {})
    assignment = result.get("assignment") or {}
    assignment_offset = result.get("assignmentOffset") or {}
    receipt_offset = result.get("receiptOffset") or {}
    if assignment.get("target") != subject:
        errors.append(f"assignment target mismatch: {assignment}")
    if assignment.get("targetId") != target_id or assignment.get("expectedAgentId") != agent_id:
        errors.append(f"assignment identity mismatch: {assignment}")
    if assignment_offset.get("stream") != assignment_stream or not assignment_offset.get("sequence"):
        errors.append(f"missing assignment stream offset: {assignment_offset}")
    if receipt_offset.get("stream") != receipt_stream or not receipt_offset.get("sequence"):
        errors.append(f"missing receipt stream offset: {receipt_offset}")
    if metadata.get("agentId") != agent_id or metadata.get("targetId") != target_id:
        errors.append(f"receipt identity mismatch: {metadata}")
    if metadata.get("delivery") != "jetstream" or metadata.get("workerDecision") != "executed":
        errors.append(f"receipt durable metadata mismatch: {metadata}")
    if metadata.get("assignmentStream") != assignment_stream or metadata.get("receiptStream") != receipt_stream:
        errors.append(f"receipt stream metadata mismatch: {metadata}")
    if not metadata.get("assignmentSequence"):
        errors.append(f"receipt missing assignment sequence: {metadata}")

status = "succeeded" if not errors else "failed"
metadata = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": __import__("datetime").datetime.utcnow().replace(microsecond=0).isoformat() + "Z",
    "labProfiles": ["local.nats.jetstream", "stack.fleet-jetstream-durable-assignments", "agent.worker-identity-receipts", "agent.assignment-idempotency-ledger"],
}
(run_dir / "metadata.json").write_text(json.dumps(metadata, indent=2, sort_keys=True) + "\n", encoding="utf-8")
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
    "reason": "durable JetStream assignment and receipt offsets verified" if not errors else "; ".join(errors),
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "verification" / "receipt.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "errors": errors,
    "fanout": summary,
    "duplicateReceipt": {
        "status": duplicate_receipt.get("status"),
        "metadata": duplicate_metadata,
    },
    "applyCode": apply_code,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-008 passed: ${OPS_RUN_DIR}"
