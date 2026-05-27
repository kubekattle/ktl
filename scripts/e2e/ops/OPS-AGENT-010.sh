#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-010.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-010 proves signed assignment envelopes: a verified JetStream worker
executes a valid signed stack assignment and blocks unsigned, expired,
wrong-policy, and wrong-target assignments without running their commands.
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
ops_init_run "OPS-AGENT-010"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-010.XXXXXX")"
helper_dir="${repo_root}/.torque/e2e/${OPS_RUN_ID}"
stack_dir="${scratch_root}/stack"
registry_path="${scratch_root}/agent-registry.json"
private_key="${scratch_root}/assignment-key.json"
public_key="${scratch_root}/assignment-pub.json"
signed_marker="${scratch_root}/signed-marker.txt"
invalid_marker="${scratch_root}/invalid-marker.txt"
nats_pid=""
nats_url="${external_nats_url}"
worker_pid=""
apply_code=0
cleanup_status="pending"
agent_id="agent-worker-signature-01"
target_id="host/mysql-signature-01"
subject="torque.assign.lab.host_mysql-signature-01"
event_stream="TORQUE_AGENT_EVENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
assignment_stream="TORQUE_ASSIGNMENTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
receipt_stream="TORQUE_RECEIPTS_${OPS_RUN_ID//[^A-Za-z0-9]/}"
registry_durable="torque-ops-agent-010-${OPS_RUN_ID//[^A-Za-z0-9]/}"
worker_durable="torque-worker-signature-${OPS_RUN_ID//[^A-Za-z0-9]/}"
ledger_path="${scratch_root}/agent-assignments.sqlite"
policy_digest="sha256:assignment-policy-good"

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
  rm -rf "${helper_dir}"
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
name: signed-assignment-envelope
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
      onExhausted: block
nodes:
  - kind: host.command.run
    name: signed-valid
    host:
      transport: nats
      timeout: 20s
      command: "printf 'signed-valid\\n' >> '${signed_marker}'"
YAML
}

write_probe_helper() {
  mkdir -p "${helper_dir}"
  cat >"${helper_dir}/assignment-probe.go" <<'GO'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	"github.com/ingresslabs/torque/internal/stack"
	nats "github.com/nats-io/nats.go"
)

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: assignment-probe <keygen|payload|publish> ...")
	}
	switch os.Args[1] {
	case "keygen":
		if len(os.Args) != 4 {
			fatalf("usage: assignment-probe keygen <private-key> <public-key>")
		}
		key, err := stack.GenerateEd25519Key()
		must(err)
		must(writeJSON(os.Args[2], key))
		pub := *key
		pub.PrivateKey = ""
		pub.Seed = ""
		must(writeJSON(os.Args[3], &pub))
	case "payload":
		if len(os.Args) != 13 {
			fatalf("usage: assignment-probe payload <signed|unsigned> <private-key|-> <out> <subject> <target-id> <agent-id> <run-id> <node-id> <policy-digest> <expires-offset> <command>")
		}
		kind := os.Args[2]
		keyPath := os.Args[3]
		out := os.Args[4]
		subject := os.Args[5]
		targetID := os.Args[6]
		agentID := os.Args[7]
		runID := os.Args[8]
		nodeID := os.Args[9]
		policyDigest := os.Args[10]
		expiresOffset, err := time.ParseDuration(os.Args[11])
		must(err)
		command := os.Args[12]
		assignment := natstransport.NewCommandAssignmentWithMetadata("run", subject, command, time.Now(), natstransport.CommandAssignmentMetadata{
			TargetID:           targetID,
			ExpectedAgentID:    agentID,
			RequiredCapability: "host.command.run",
			NodeKind:           "host.command.run",
			RunID:              runID,
			NodeID:             nodeID,
			PlanDigest:         "sha256:plan",
		})
		switch kind {
		case "unsigned":
			must(writeJSON(out, assignment))
		case "signed":
			_, _, privateKey, err := stack.LoadBundleKey(keyPath)
			must(err)
			issuedAt := time.Now().UTC()
			envelope, err := natstransport.SignCommandAssignmentEnvelope(assignment, natstransport.CommandAssignmentEnvelopeOptions{
				PrivateKey:    privateKey,
				Issuer:        "torque-stack-e2e",
				Tenant:        "lab",
				PolicyDigest:  policyDigest,
				IssuedAt:      issuedAt,
				ExpiresAt:     issuedAt.Add(expiresOffset),
			})
			must(err)
			must(writeJSON(out, envelope))
		default:
			fatalf("unknown payload kind %q", kind)
		}
	case "publish":
		if len(os.Args) != 8 {
			fatalf("usage: assignment-probe publish <nats-url> <receipt-stream> <durable> <payload> <timeout> <out>")
		}
		natsURL := os.Args[2]
		receiptStream := os.Args[3]
		durable := os.Args[4]
		payloadPath := os.Args[5]
		timeout, err := time.ParseDuration(os.Args[6])
		must(err)
		out := os.Args[7]
		raw, err := os.ReadFile(payloadPath)
		must(err)
		assignment, _, err := natstransport.ParseCommandAssignmentMessage(raw)
		must(err)
		receiptSubject := natstransport.ReceiptSubject("lab", assignment.RunID, assignment.TargetID)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		conn, err := nats.Connect(natsURL, nats.Name("torque-ops-agent-010-assignment-probe"), nats.Timeout(timeout))
		must(err)
		defer conn.Close()
		js, err := conn.JetStream(nats.MaxWait(timeout))
		must(err)
		sub, err := js.PullSubscribe(
			receiptSubject,
			durable,
			nats.BindStream(receiptStream),
			nats.DeliverNew(),
			nats.AckExplicit(),
			nats.ManualAck(),
		)
		must(err)
		_, err = js.Publish(assignment.Target, raw, nats.Context(ctx))
		must(err)
		msgs, err := sub.Fetch(1, nats.MaxWait(timeout))
		must(err)
		if len(msgs) != 1 {
			fatalf("expected one receipt, got %d", len(msgs))
		}
		must(os.WriteFile(out, append(msgs[0].Data, '\n'), 0o600))
		_ = msgs[0].Ack(nats.Context(ctx))
	default:
		fatalf("unknown command %q", os.Args[1])
	}
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o600)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
GO
}

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/logs" "${OPS_RUN_DIR}/verification" "${stack_dir}"
write_probe_helper

ops_log "build torque and torque-agent"
make -C "${repo_root}" -s build build-agent >"${OPS_RUN_DIR}/build/make-build.out" 2>&1

ops_log "generate assignment signing key"
go run "${helper_dir}/assignment-probe.go" keygen "${private_key}" "${public_key}" \
  >"${OPS_RUN_DIR}/logs/keygen.out" 2>"${OPS_RUN_DIR}/logs/keygen.err"

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
  --hostname worker-signature-01 \
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

ops_log "start verified JetStream worker"
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
  --hostname worker-signature-01 \
  --capability host.command.run \
  --verify-assignments \
  --trusted-issuer-key "${public_key}" \
  --policy-digest "${policy_digest}" \
  --max-deliver 3 \
  --ack-wait 500ms \
  --nak-delay 100ms \
  >"${OPS_RUN_DIR}/logs/worker.log" 2>&1 &
worker_pid="$!"
wait_for_worker "${worker_pid}" "${OPS_RUN_DIR}/logs/worker.log"

ops_log "run signed stack assignment"
set +e
TORQUE_NATS_URL="${nats_url}" \
TORQUE_NATS_ASSIGNMENT_STREAM="${assignment_stream}" \
TORQUE_NATS_RECEIPT_STREAM="${receipt_stream}" \
TORQUE_NATS_ASSIGNMENT_SIGNING_KEY="${private_key}" \
TORQUE_NATS_ASSIGNMENT_ISSUER="torque-stack-e2e" \
TORQUE_NATS_ASSIGNMENT_POLICY_DIGEST="${policy_digest}" \
TORQUE_NATS_ASSIGNMENT_TTL="2m" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/apply.out" 2>"${OPS_RUN_DIR}/logs/apply.err"
apply_code="$?"
set -e
"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/audit.json"

ops_log "publish invalid assignment probes"
go run "${helper_dir}/assignment-probe.go" payload unsigned - "${scratch_root}/unsigned.json" "${subject}" "${target_id}" "${agent_id}" "run-unsigned" "host.command.run/unsigned" "${policy_digest}" "2m" "printf invalid-unsigned >> '${invalid_marker}'" \
  >"${OPS_RUN_DIR}/logs/payload-unsigned.out" 2>"${OPS_RUN_DIR}/logs/payload-unsigned.err"
go run "${helper_dir}/assignment-probe.go" publish "${nats_url}" "${receipt_stream}" "torque-ops-agent-010-unsigned-${OPS_RUN_ID//[^A-Za-z0-9]/}" "${scratch_root}/unsigned.json" 10s "${OPS_RUN_DIR}/verification/unsigned-receipt.json" \
  >"${OPS_RUN_DIR}/logs/publish-unsigned.out" 2>"${OPS_RUN_DIR}/logs/publish-unsigned.err"

go run "${helper_dir}/assignment-probe.go" payload signed "${private_key}" "${scratch_root}/expired.json" "${subject}" "${target_id}" "${agent_id}" "run-expired" "host.command.run/expired" "${policy_digest}" "-1s" "printf invalid-expired >> '${invalid_marker}'" \
  >"${OPS_RUN_DIR}/logs/payload-expired.out" 2>"${OPS_RUN_DIR}/logs/payload-expired.err"
go run "${helper_dir}/assignment-probe.go" publish "${nats_url}" "${receipt_stream}" "torque-ops-agent-010-expired-${OPS_RUN_ID//[^A-Za-z0-9]/}" "${scratch_root}/expired.json" 10s "${OPS_RUN_DIR}/verification/expired-receipt.json" \
  >"${OPS_RUN_DIR}/logs/publish-expired.out" 2>"${OPS_RUN_DIR}/logs/publish-expired.err"

go run "${helper_dir}/assignment-probe.go" payload signed "${private_key}" "${scratch_root}/wrong-policy.json" "${subject}" "${target_id}" "${agent_id}" "run-wrong-policy" "host.command.run/wrong-policy" "sha256:assignment-policy-bad" "2m" "printf invalid-policy >> '${invalid_marker}'" \
  >"${OPS_RUN_DIR}/logs/payload-policy.out" 2>"${OPS_RUN_DIR}/logs/payload-policy.err"
go run "${helper_dir}/assignment-probe.go" publish "${nats_url}" "${receipt_stream}" "torque-ops-agent-010-policy-${OPS_RUN_ID//[^A-Za-z0-9]/}" "${scratch_root}/wrong-policy.json" 10s "${OPS_RUN_DIR}/verification/wrong-policy-receipt.json" \
  >"${OPS_RUN_DIR}/logs/publish-policy.out" 2>"${OPS_RUN_DIR}/logs/publish-policy.err"

go run "${helper_dir}/assignment-probe.go" payload signed "${private_key}" "${scratch_root}/wrong-target.json" "${subject}" "host/wrong-target" "${agent_id}" "run-wrong-target" "host.command.run/wrong-target" "${policy_digest}" "2m" "printf invalid-target >> '${invalid_marker}'" \
  >"${OPS_RUN_DIR}/logs/payload-target.out" 2>"${OPS_RUN_DIR}/logs/payload-target.err"
go run "${helper_dir}/assignment-probe.go" publish "${nats_url}" "${receipt_stream}" "torque-ops-agent-010-target-${OPS_RUN_ID//[^A-Za-z0-9]/}" "${scratch_root}/wrong-target.json" 10s "${OPS_RUN_DIR}/verification/wrong-target-receipt.json" \
  >"${OPS_RUN_DIR}/logs/publish-target.out" 2>"${OPS_RUN_DIR}/logs/publish-target.err"

ops_log "verify signed assignment evidence"
python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${event_stream}" "${assignment_stream}" "${receipt_stream}" "${signed_marker}" "${invalid_marker}" "${apply_code}" "${agent_id}" "${target_id}" "${subject}" "${policy_digest}" <<'PY'
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
signed_marker = Path(sys.argv[9])
invalid_marker = Path(sys.argv[10])
apply_code = int(sys.argv[11])
agent_id = sys.argv[12]
target_id = sys.argv[13]
subject = sys.argv[14]
policy_digest = sys.argv[15]

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

def text_count(path, token):
    if not path.exists():
        return 0
    return path.read_text(encoding="utf-8").count(token)

audit = load("verification/audit.json")
registry_status = load("verification/registry-status.json")
unsigned_receipt = load("verification/unsigned-receipt.json")
expired_receipt = load("verification/expired-receipt.json")
wrong_policy_receipt = load("verification/wrong-policy-receipt.json")
wrong_target_receipt = load("verification/wrong-target-receipt.json")
fanout = artifact(audit, "host-command-fanout.json")
errors = []

if apply_code != 0:
    errors.append(f"signed stack apply failed with code {apply_code}")
if registry_status.get("summary", {}).get("ready") != 1:
    errors.append("registry status must have one ready agent")
if audit.get("status") != "succeeded":
    errors.append(f"audit must succeed: {audit.get('status')}")
if fanout.get("status") != "succeeded":
    errors.append(f"fanout must succeed: {fanout.get('status')}")
if text_count(signed_marker, "signed-valid") != 1:
    errors.append("signed marker must be written exactly once")
if invalid_marker.exists() and invalid_marker.read_text(encoding="utf-8").strip():
    errors.append("invalid assignment marker must not be written")

results = fanout.get("results", [])
if len(results) != 1:
    errors.append(f"expected one signed fanout result, got {len(results)}")
else:
    result = results[0]
    receipt = result.get("receipt", {})
    metadata = receipt.get("metadata", {})
    envelope = result.get("assignmentEnvelope") or {}
    assignment = result.get("assignment") or {}
    if receipt.get("status") != "succeeded":
        errors.append(f"signed receipt must succeed: {receipt}")
    if not envelope:
        errors.append("fanout result must preserve assignmentEnvelope")
    if (envelope.get("signature") or {}).get("algorithm") != "ed25519":
        errors.append(f"assignment envelope signature missing: {envelope}")
    expected = {
        "agentId": agent_id,
        "targetId": target_id,
        "assignmentTargetId": target_id,
        "expectedAgentId": agent_id,
        "assignmentEnvelope": "true",
        "signatureVerified": "true",
        "assignmentIssuer": "torque-stack-e2e",
        "assignmentEnvelopeTenant": "lab",
        "policyDigest": policy_digest,
        "workerDecision": "executed",
    }
    for key, value in expected.items():
        if metadata.get(key) != value:
            errors.append(f"signed metadata[{key}]={metadata.get(key)!r}, want {value!r}: {metadata}")
    if assignment.get("target") != subject:
        errors.append(f"signed assignment target mismatch: {assignment}")
    if result.get("assignmentOffset", {}).get("stream") != assignment_stream:
        errors.append(f"assignment stream offset missing: {result.get('assignmentOffset')}")
    if result.get("receiptOffset", {}).get("stream") != receipt_stream:
        errors.append(f"receipt stream offset missing: {result.get('receiptOffset')}")

def expect_blocked(name, receipt, text, signed):
    metadata = receipt.get("metadata", {})
    if receipt.get("status") != "blocked":
        errors.append(f"{name} receipt must be blocked: {receipt}")
    if text not in receipt.get("error", ""):
        errors.append(f"{name} receipt error must contain {text!r}: {receipt}")
    if metadata.get("workerDecision") != "signature-blocked":
        errors.append(f"{name} workerDecision mismatch: {metadata}")
    if signed:
        if metadata.get("assignmentEnvelope") != "true" or metadata.get("signatureVerified") != "true":
            errors.append(f"{name} signed metadata missing verification: {metadata}")
    else:
        if metadata.get("signatureVerified"):
            errors.append(f"{name} unsigned metadata must not claim signature verification: {metadata}")

expect_blocked("unsigned", unsigned_receipt, "signed assignment envelope is required", False)
expect_blocked("expired", expired_receipt, "expired", True)
expect_blocked("wrong-policy", wrong_policy_receipt, "policyDigest", True)
expect_blocked("wrong-target", wrong_target_receipt, "targetId", True)

status = "succeeded" if not errors else "failed"
metadata = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": datetime.utcnow().replace(microsecond=0).isoformat() + "Z",
    "labProfiles": ["local.nats.jetstream", "stack.fleet-jetstream-signed-assignments", "agent.signed-assignment-envelope"],
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
    "reason": "signed assignment envelope and fail-closed worker verification proved" if not errors else "; ".join(errors),
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
(run_dir / "verification" / "receipt.json").write_text(json.dumps({
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "errors": errors,
    "applyCode": apply_code,
    "fanout": fanout.get("summary", {}),
    "invalidReceipts": {
        "unsigned": {"status": unsigned_receipt.get("status"), "metadata": unsigned_receipt.get("metadata", {})},
        "expired": {"status": expired_receipt.get("status"), "metadata": expired_receipt.get("metadata", {})},
        "wrongPolicy": {"status": wrong_policy_receipt.get("status"), "metadata": wrong_policy_receipt.get("metadata", {})},
        "wrongTarget": {"status": wrong_target_receipt.get("status"), "metadata": wrong_target_receipt.get("metadata", {})},
    },
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("\n".join(errors))
PY

ops_log "OPS-AGENT-010 passed: ${OPS_RUN_DIR}"
