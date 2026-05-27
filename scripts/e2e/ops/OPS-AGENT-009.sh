#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-009.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-009 proves durable assignment retry policy: a JetStream worker retries
a transient host.command.run failure until it succeeds, publishes a dead-letter
receipt when the retry budget is exhausted, and still dedupes a duplicate
successful assignment from its local ledger.
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
ops_init_run "OPS-AGENT-009"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-009.XXXXXX")"
retry_stack_dir="${scratch_root}/retry-stack"
deadletter_stack_dir="${scratch_root}/deadletter-stack"
registry_path="${scratch_root}/agent-registry.json"
retry_attempts="${scratch_root}/retry-attempts.txt"
retry_marker="${scratch_root}/retry-marker.txt"
deadletter_attempts="${scratch_root}/deadletter-attempts.txt"
nats_pid=""
nats_url="${external_nats_url}"
worker_pid=""
retry_apply_code=0
deadletter_apply_code=0
cleanup_status="pending"
agent_id="agent-worker-retry-01"
target_id="host/mysql-retry-01"
subject="torque.assign.lab.host_mysql-retry-01"
event_stream="TORQUE_AGENT_EVENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
assignment_stream="TORQUE_ASSIGNMENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
receipt_stream="TORQUE_RECEIPTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
registry_durable="torque-ops-agent-009-${OPS_RUN_ID//[^A-Za-z0-9]/}"
worker_durable="torque-worker-retry-${OPS_RUN_ID//[^A-Za-z0-9]/}"
ledger_path="${scratch_root}/agent-assignments.sqlite"

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

write_retry_stack() {
  mkdir -p "${retry_stack_dir}"
  cat >"${retry_stack_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: durable-retry-success
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
    retry:
      maxDeliver: 3
      ackWait: 500ms
      backoff:
        - 100ms
        - 100ms
        - 100ms
      onExhausted: block
nodes:
  - kind: host.command.run
    name: retry-until-success
    host:
      transport: nats
      timeout: 20s
      command: "count=0; if [ -f '${retry_attempts}' ]; then count=\$(cat '${retry_attempts}'); fi; count=\$((count + 1)); printf '%s\\n' \"\${count}\" > '${retry_attempts}'; if [ \"\${count}\" -lt 3 ]; then echo transient failure >&2; exit 42; fi; printf 'retry-success\\n' >> '${retry_marker}'"
YAML
}

write_deadletter_stack() {
  mkdir -p "${deadletter_stack_dir}"
  cat >"${deadletter_stack_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: durable-retry-deadletter
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
    retry:
      maxDeliver: 3
      ackWait: 500ms
      backoff:
        - 100ms
        - 100ms
        - 100ms
      onExhausted: block
nodes:
  - kind: host.command.run
    name: exhaust-retry-budget
    host:
      transport: nats
      timeout: 20s
      command: "count=0; if [ -f '${deadletter_attempts}' ]; then count=\$(cat '${deadletter_attempts}'); fi; count=\$((count + 1)); printf '%s\\n' \"\${count}\" > '${deadletter_attempts}'; echo deterministic failure >&2; exit 43"
YAML
}

write_duplicate_probe() {
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
	conn, err := nats.Connect(natsURL, nats.Name("torque-ops-agent-009-duplicate-probe"), nats.Timeout(timeout))
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
}

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/logs" "${OPS_RUN_DIR}/verification" "${retry_stack_dir}" "${deadletter_stack_dir}"

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
  --hostname worker-retry-01 \
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

write_retry_stack
write_deadletter_stack
write_duplicate_probe

ops_log "start JetStream worker with explicit retry policy"
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
  --hostname worker-retry-01 \
  --capability host.command.run \
  --max-deliver 3 \
  --ack-wait 500ms \
  --backoff 100ms \
  --backoff 100ms \
  --backoff 100ms \
  --nak-delay 100ms \
  --on-exhausted block \
  >"${OPS_RUN_DIR}/logs/worker.log" 2>&1 &
worker_pid="$!"
wait_for_worker "${worker_pid}" "${OPS_RUN_DIR}/logs/worker.log"

ops_log "run retry-success stack"
set +e
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${retry_stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/retry-apply.out" 2>"${OPS_RUN_DIR}/logs/retry-apply.err"
retry_apply_code="$?"
set -e
"${repo_root}/bin/torque" stack audit --config "${retry_stack_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/retry-audit.json"

ops_log "republish successful assignment to prove idempotent duplicate handling"
duplicate_receipt_subject="$(
python3 - "${OPS_RUN_DIR}/verification/retry-audit.json" "${scratch_root}/duplicate-assignment.json" <<'PY'
import json
import re
import sys
from pathlib import Path

audit = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
assignment_path = Path(sys.argv[2])

def artifact(name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            return json.loads(item.get("body", "{}"))
    return {}

def token(value, fallback):
    value = (value or "").strip() or fallback
    value = re.sub(r"[^A-Za-z0-9_-]+", "_", value).strip("_")
    return value or fallback

fanout = artifact("host-command-fanout.json")
assignment = (fanout.get("results") or [{}])[0].get("assignment") or {}
assignment_path.write_text(json.dumps(assignment, sort_keys=True) + "\n", encoding="utf-8")
print("torque.receipt.lab." + token(assignment.get("runId"), "run") + "." + token(assignment.get("targetId"), "target"))
PY
)"
go run "${scratch_root}/duplicate-probe.go" \
  "${nats_url}" \
  "${scratch_root}/duplicate-assignment.json" \
  "${duplicate_receipt_subject}" \
  "${receipt_stream}" \
  "torque-ops-agent-009-duplicate-${OPS_RUN_ID//[^A-Za-z0-9]/}" \
  10s \
  >"${OPS_RUN_DIR}/verification/duplicate-receipt.json" \
  2>"${OPS_RUN_DIR}/logs/duplicate-probe.err"

ops_log "run dead-letter stack"
set +e
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
  "${repo_root}/bin/torque" stack apply --config "${deadletter_stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/deadletter-apply.out" 2>"${OPS_RUN_DIR}/logs/deadletter-apply.err"
deadletter_apply_code="$?"
set -e
set +e
"${repo_root}/bin/torque" stack audit --config "${deadletter_stack_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/deadletter-audit.json" \
  2>"${OPS_RUN_DIR}/logs/deadletter-audit.err"
audit_code="$?"
set -e
if [[ "${audit_code}" != "0" ]]; then
  ops_fail "dead-letter stack audit failed with code ${audit_code}; see ${OPS_RUN_DIR}/logs/deadletter-audit.err"
fi

ops_log "verify durable retry and dead-letter receipts"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${event_stream}" "${assignment_stream}" "${receipt_stream}" "${retry_attempts}" "${retry_marker}" "${deadletter_attempts}" "${retry_apply_code}" "${deadletter_apply_code}" "${agent_id}" "${target_id}" "${subject}" <<'PY'
import json
import sys
from pathlib import Path
from datetime import datetime

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
nats_url = sys.argv[5]
event_stream = sys.argv[6]
assignment_stream = sys.argv[7]
receipt_stream = sys.argv[8]
retry_attempts_path = Path(sys.argv[9])
retry_marker_path = Path(sys.argv[10])
deadletter_attempts_path = Path(sys.argv[11])
retry_apply_code = int(sys.argv[12])
deadletter_apply_code = int(sys.argv[13])
agent_id = sys.argv[14]
target_id = sys.argv[15]
subject = sys.argv[16]

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

def text_int(path):
    if not path.exists():
        return 0
    raw = path.read_text(encoding="utf-8").strip()
    return int(raw or "0")

def text_count(path, token):
    if not path.exists():
        return 0
    return path.read_text(encoding="utf-8").count(token)

retry_audit = load("verification/retry-audit.json")
deadletter_audit = load("verification/deadletter-audit.json")
registry_status = load("verification/registry-status.json")
duplicate_receipt = load("verification/duplicate-receipt.json")
retry_fanout = artifact(retry_audit, "host-command-fanout.json")
deadletter_fanout = artifact(deadletter_audit, "host-command-fanout.json")
errors = []

if retry_apply_code != 0:
    errors.append(f"retry stack apply failed with code {retry_apply_code}")
if deadletter_apply_code == 0:
    errors.append("dead-letter stack apply should fail after retry exhaustion")
if registry_status.get("summary", {}).get("ready") != 1:
    errors.append("registry status must have one ready agent")
if retry_audit.get("status") != "succeeded":
    errors.append(f"retry audit must succeed: {retry_audit.get('status')}")
if retry_fanout.get("status") != "succeeded":
    errors.append(f"retry fanout status must succeed: {retry_fanout.get('status')}")
if retry_fanout.get("policy", {}).get("retry", {}).get("maxDeliver") != 3:
    errors.append(f"retry policy missing from fanout: {retry_fanout.get('policy')}")
if retry_attempts_path.exists() and text_int(retry_attempts_path) != 3:
    errors.append(f"retry attempts must be 3, got {text_int(retry_attempts_path)}")
if text_count(retry_marker_path, "retry-success") != 1:
    errors.append("retry success marker must be written exactly once")

retry_results = retry_fanout.get("results", [])
if len(retry_results) != 1:
    errors.append(f"expected one retry fanout result, got {len(retry_results)}")
else:
    result = retry_results[0]
    receipt = result.get("receipt", {})
    metadata = receipt.get("metadata", {})
    if receipt.get("status") != "succeeded":
        errors.append(f"retry receipt must succeed: {receipt}")
    expected = {
        "agentId": agent_id,
        "targetId": target_id,
        "assignmentTargetId": target_id,
        "expectedAgentId": agent_id,
        "workerDecision": "executed",
        "numDelivered": "3",
        "maxDeliver": "3",
        "ledgerAttempt": "3",
    }
    for key, value in expected.items():
        if metadata.get(key) != value:
            errors.append(f"retry metadata[{key}]={metadata.get(key)!r}, want {value!r}: {metadata}")
    if result.get("assignment", {}).get("target") != subject:
        errors.append(f"retry assignment target mismatch: {result.get('assignment')}")
    if result.get("assignmentOffset", {}).get("stream") != assignment_stream:
        errors.append(f"retry assignment stream offset missing: {result.get('assignmentOffset')}")
    if result.get("receiptOffset", {}).get("stream") != receipt_stream:
        errors.append(f"retry receipt stream offset missing: {result.get('receiptOffset')}")

duplicate_metadata = duplicate_receipt.get("metadata", {})
if duplicate_receipt.get("status") != "succeeded":
    errors.append(f"duplicate receipt must keep succeeded status: {duplicate_receipt}")
if duplicate_metadata.get("deduped") != "true" or duplicate_metadata.get("replayedReceipt") != "true" or duplicate_metadata.get("workerDecision") != "deduped":
    errors.append(f"duplicate receipt must prove ledger dedupe: {duplicate_metadata}")
if text_count(retry_marker_path, "retry-success") != 1:
    errors.append("duplicate publish must not write retry marker again")

if deadletter_fanout.get("status") != "failed":
    errors.append(f"dead-letter fanout status must fail: {deadletter_fanout.get('status')}")
if deadletter_attempts_path.exists() and text_int(deadletter_attempts_path) != 3:
    errors.append(f"dead-letter attempts must be 3, got {text_int(deadletter_attempts_path)}")
deadletter_results = deadletter_fanout.get("results", [])
if len(deadletter_results) != 1:
    errors.append(f"expected one dead-letter fanout result, got {len(deadletter_results)}")
else:
    result = deadletter_results[0]
    receipt = result.get("receipt", {})
    metadata = receipt.get("metadata", {})
    if receipt.get("status") != "blocked":
        errors.append(f"dead-letter receipt must be blocked: {receipt}")
    expected = {
        "workerDecision": "dead-letter",
        "deadLetter": "true",
        "retryExhausted": "true",
        "numDelivered": "3",
        "maxDeliver": "3",
        "ledgerAttempt": "3",
    }
    for key, value in expected.items():
        if metadata.get(key) != value:
            errors.append(f"dead-letter metadata[{key}]={metadata.get(key)!r}, want {value!r}: {metadata}")
    if "retry budget exhausted" not in receipt.get("error", ""):
        errors.append(f"dead-letter receipt must explain exhausted budget: {receipt}")

status = "succeeded" if not errors else "failed"
metadata = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": datetime.utcnow().replace(microsecond=0).isoformat() + "Z",
    "labProfiles": ["local.nats.jetstream", "stack.fleet-jetstream-durable-retry", "agent.assignment-idempotency-ledger", "agent.dead-letter-receipts"],
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
    "reason": "durable retry, dead-letter, and dedupe receipts verified" if not errors else "; ".join(errors),
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "verification" / "receipt.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "errors": errors,
    "retryApplyCode": retry_apply_code,
    "deadletterApplyCode": deadletter_apply_code,
    "retryFanout": retry_fanout.get("summary", {}),
    "deadletterFanout": deadletter_fanout.get("summary", {}),
    "duplicateReceipt": {"status": duplicate_receipt.get("status"), "metadata": duplicate_metadata},
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-009 passed: ${OPS_RUN_DIR}"
