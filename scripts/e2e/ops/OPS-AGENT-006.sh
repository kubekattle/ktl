#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-006.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-006 proves stack fleet readiness and capability gates end to end:
capture an agent capability report, publish durable agent heartbeats through
JetStream, compact them into a registry store, run torque stack apply in
runner.mode=fleet over NATS, and prove insufficient readiness plus missing
capability cases block before mutation. It also proves the worker enforces
requiredCapability locally and rejects unsafe assignments before execution.
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
ops_init_run "OPS-AGENT-006"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-006.XXXXXX")"
stack_pass_dir="${scratch_root}/stack-pass"
stack_block_dir="${scratch_root}/stack-block"
stack_capability_block_dir="${scratch_root}/stack-capability-block"
stack_worker_capability_block_dir="${scratch_root}/stack-worker-capability-block"
registry_path="${scratch_root}/agent-registry.json"
pass_marker="${scratch_root}/pass-marker.txt"
block_marker="${scratch_root}/block-marker.txt"
capability_block_marker="${scratch_root}/capability-block-marker.txt"
worker_capability_block_marker="${scratch_root}/worker-capability-block-marker.txt"
subject="torque.e2e.assign.fleet.${OPS_RUN_ID//[^A-Za-z0-9]/}"
blocked_subject="torque.e2e.assign.blocked.${OPS_RUN_ID//[^A-Za-z0-9]/}"
registry_durable="torque-ops-agent-006-${OPS_RUN_ID//[^A-Za-z0-9]/}"
nats_pid=""
worker_pid=""
blocked_worker_pid=""
nats_url="${external_nats_url}"
cleanup_status="pending"

finish() {
  local code=$?
  trap - EXIT
  set +e
  if [[ -n "${worker_pid}" ]]; then
    kill "${worker_pid}" 2>/dev/null
    wait "${worker_pid}" 2>/dev/null
  fi
  if [[ -n "${blocked_worker_pid}" ]]; then
    kill "${blocked_worker_pid}" 2>/dev/null
    wait "${blocked_worker_pid}" 2>/dev/null
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

  python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${subject}" "${blocked_subject}" "${cleanup_status}" "${code}" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
nats_url = sys.argv[5]
subject = sys.argv[6]
blocked_subject = sys.argv[7]
cleanup_status = sys.argv[8]
exit_code = int(sys.argv[9])
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def load(rel):
    path = run_dir / rel
    if not path.is_file():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}

def write(rel, doc):
    path = run_dir / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            body = item.get("body", "")
            try:
                return json.loads(body)
            except json.JSONDecodeError:
                return {}
    return {}

pass_audit = load("verification/pass-audit.json")
block_audit = load("verification/block-audit.json")
capability_block_audit = load("verification/capability-block-audit.json")
worker_capability_block_audit = load("verification/worker-capability-block-audit.json")
capability_report = load("verification/capability-report.json")
missing_path_capability_report = load("verification/capability-report-missing-path.json")
status_store = load("verification/status-store.json")
status_nocap_store = load("verification/status-nocap-store.json")
compact = load("verification/registry-compact.json")
compact_nocap = load("verification/registry-compact-nocap.json")
pass_marker = (run_dir / "verification/pass-marker.txt").read_text(encoding="utf-8").strip() if (run_dir / "verification/pass-marker.txt").is_file() else ""
block_marker_exists = (run_dir / "verification/block-marker-present").is_file()
capability_block_marker_exists = (run_dir / "verification/capability-block-marker-present").is_file()
worker_capability_block_marker_exists = (run_dir / "verification/worker-capability-block-marker-present").is_file()
block_code_raw = (run_dir / "verification/block-exit-code.txt").read_text(encoding="utf-8").strip() if (run_dir / "verification/block-exit-code.txt").is_file() else ""
capability_block_code_raw = (run_dir / "verification/capability-block-exit-code.txt").read_text(encoding="utf-8").strip() if (run_dir / "verification/capability-block-exit-code.txt").is_file() else ""
worker_capability_block_code_raw = (run_dir / "verification/worker-capability-block-exit-code.txt").read_text(encoding="utf-8").strip() if (run_dir / "verification/worker-capability-block-exit-code.txt").is_file() else ""
try:
    block_code = int(block_code_raw)
except ValueError:
    block_code = -1
try:
    capability_block_code = int(capability_block_code_raw)
except ValueError:
    capability_block_code = -1
try:
    worker_capability_block_code = int(worker_capability_block_code_raw)
except ValueError:
    worker_capability_block_code = -1

pass_readiness = artifact(pass_audit, "fleet-readiness.json")
block_readiness = artifact(block_audit, "fleet-readiness.json")
capability_block_readiness = artifact(capability_block_audit, "fleet-readiness.json")
worker_capability_block_readiness = artifact(worker_capability_block_audit, "fleet-readiness.json")
worker_execute = artifact(worker_capability_block_audit, "host-command-execute.json")
errors = []
available_caps = {
    item.get("adapter")
    for item in capability_report.get("capabilities", [])
    if item.get("status") == "available"
}
missing_path_caps = {
    item.get("adapter"): item
    for item in missing_path_capability_report.get("capabilities", [])
}
store_agents = status_store.get("agents", [])
store_agent = store_agents[0] if store_agents else {}
if not str(capability_report.get("digest", "")).startswith("sha256:"):
    errors.append("capability report must include a sha256 digest")
if "host.command.run" not in available_caps:
    errors.append("capability report must discover host.command.run")
if store_agent.get("capabilityDigest") != capability_report.get("digest"):
    errors.append("store-backed heartbeat must include discovered capability digest")
if "host.command.run" not in set(store_agent.get("capabilities", [])):
    errors.append("store-backed heartbeat must include discovered host.command.run")
missing_host_command = missing_path_caps.get("host.command.run", {})
if missing_host_command.get("status") != "unavailable" or not missing_host_command.get("missingDependencies"):
    errors.append("missing-path capability report must explain unavailable host.command.run")
if compact.get("stored") != 1:
    errors.append("registry compaction must store exactly one agent")
if compact_nocap.get("stored") != 1:
    errors.append("nocap registry compaction must store exactly one agent")
if status_store.get("summary", {}).get("ready") != 1:
    errors.append("store-backed status must report one ready agent")
if status_nocap_store.get("summary", {}).get("ready") != 1:
    errors.append("nocap store-backed status must report one ready agent")
if pass_audit.get("status") != "succeeded":
    errors.append("pass stack audit status must be succeeded")
if pass_readiness.get("status") != "ready":
    errors.append("pass fleet-readiness artifact status must be ready")
if pass_readiness.get("summary", {}).get("readyPercent") != 100:
    errors.append("pass readiness readyPercent must be 100")
if pass_readiness.get("summary", {}).get("coveredCapabilities") != 1:
    errors.append("pass readiness must cover one required capability")
if pass_readiness.get("summary", {}).get("missingCapabilities", 0) != 0:
    errors.append("pass readiness must not report missing capabilities")
if pass_marker != "fleet-pass":
    errors.append("pass marker was not written by NATS worker")
if block_code == 0:
    errors.append("block stack apply unexpectedly succeeded")
if block_audit.get("status") != "blocked":
    errors.append("block stack audit status must be blocked")
if block_readiness.get("status") != "blocked":
    errors.append("block fleet-readiness artifact status must be blocked")
if block_marker_exists:
    errors.append("block marker exists; mutation ran despite readiness block")
if capability_block_code == 0:
    errors.append("capability block stack apply unexpectedly succeeded")
if capability_block_audit.get("status") != "blocked":
    errors.append("capability block stack audit status must be blocked")
if capability_block_readiness.get("status") != "blocked":
    errors.append("capability block fleet-readiness artifact status must be blocked")
if capability_block_readiness.get("summary", {}).get("missingCapabilities") != 1:
    errors.append("capability block readiness must report one missing capability")
if not any(blocker.get("code") == "fleet.capability.missing" for blocker in capability_block_readiness.get("blockers", [])):
    errors.append("capability block readiness must include fleet.capability.missing")
if capability_block_marker_exists:
    errors.append("capability block marker exists; mutation ran despite capability block")
if worker_capability_block_code == 0:
    errors.append("worker capability block stack apply unexpectedly succeeded")
if worker_capability_block_audit.get("status") == "succeeded":
    errors.append("worker capability block stack audit status must not be succeeded")
if worker_capability_block_readiness.get("status") != "ready":
    errors.append("worker capability block readiness must pass before worker-side rejection")
if worker_execute.get("status") != "blocked":
    errors.append("worker-side capability rejection receipt must be blocked")
if "missing required capability host.command.run" not in worker_execute.get("error", ""):
    errors.append("worker-side capability rejection must explain missing host.command.run")
if worker_capability_block_marker_exists:
    errors.append("worker capability block marker exists; worker executed despite missing capability")
run_status = "succeeded" if exit_code == 0 and not errors else "failed"

write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["local.nats.jetstream", "ops.agent.registry", "stack.fleet-readiness", "stack.fleet-capability", "agent.worker-capability-enforcement"],
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "nats/local-jetstream", "type": "nats-jetstream", "url": nats_url, "stream": "TORQUE_AGENT_EVENTS"},
        {"id": "worker/local", "type": "torque-agent-nats-worker", "subject": subject},
        {"id": "worker/incapable", "type": "torque-agent-nats-worker", "subject": blocked_subject, "capabilities": []},
        {"id": "agent/agent-mysql-01", "type": "torque-agent", "tenant": "lab", "labels": {"role": "mysql", "site": "lab"}},
        {"id": "agent/agent-nocap-01", "type": "torque-agent", "tenant": "lab", "labels": {"role": "nocap", "site": "lab"}},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "enforce-stack-fleet-readiness-and-capability-before-mutation",
    "status": "succeeded" if run_status == "succeeded" else "blocked",
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": run_status,
    "capabilityReportDigest": capability_report.get("digest"),
    "capabilityReportSummary": capability_report.get("summary"),
    "missingPathCapabilityReportSummary": missing_path_capability_report.get("summary"),
    "compact": compact,
    "nocapCompact": compact_nocap,
    "storeSummary": status_store.get("summary"),
    "nocapStoreSummary": status_nocap_store.get("summary"),
    "passReadiness": pass_readiness.get("summary"),
    "blockReadiness": block_readiness.get("summary"),
    "capabilityBlockReadiness": capability_block_readiness.get("summary"),
    "workerCapabilityBlockReadiness": worker_capability_block_readiness.get("summary"),
    "workerCapabilityBlockReceipt": worker_execute,
    "blockExitCode": block_code,
    "capabilityBlockExitCode": capability_block_code,
    "workerCapabilityBlockExitCode": worker_capability_block_code,
    "errors": errors,
    "verifiedAt": finished_at,
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded",
    "mode": cleanup_status,
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": run_status,
    "finishedAt": finished_at,
    "natsUrl": nats_url,
    "subject": subject,
    "blockedSubject": blocked_subject,
})
if run_status != "succeeded":
    sys.exit(1)
PY
  local receipt_code=$?
  if [[ ${receipt_code} -ne 0 ]]; then
    code=1
  fi
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/logs" "${OPS_RUN_DIR}/verification" "${stack_pass_dir}" "${stack_block_dir}" "${stack_capability_block_dir}" "${stack_worker_capability_block_dir}"

ops_log "build torque and torque-agent"
make -C "${repo_root}" -s build build-agent >"${OPS_RUN_DIR}/build/make-build.out" 2>&1

ops_log "capture discovered agent capability reports"
"${repo_root}/bin/torque-agent" capabilities report \
  --format json \
  >"${OPS_RUN_DIR}/verification/capability-report.json" 2>"${OPS_RUN_DIR}/logs/capability-report.err"
PATH=/nonexistent "${repo_root}/bin/torque-agent" capabilities report \
  --format json \
  --adapter host.command.run \
  >"${OPS_RUN_DIR}/verification/capability-report-missing-path.json" 2>"${OPS_RUN_DIR}/logs/capability-report-missing-path.err"

if [[ -z "${nats_url}" ]]; then
  if command -v nats-server >/dev/null 2>&1; then
    nats_bin="$(command -v nats-server)"
  else
    ops_fail "nats-server is required when --nats-url is not provided"
  fi
  nats_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
  nats_url="nats://127.0.0.1:${nats_port}"
  "${nats_bin}" -js -sd "${scratch_root}/nats" -a 127.0.0.1 -p "${nats_port}" >"${OPS_RUN_DIR}/logs/nats-server.log" 2>&1 &
  nats_pid="$!"
fi

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

"${repo_root}/bin/torque-agent" nats worker \
  --nats-url "${nats_url}" \
  --subject "${subject}" \
  --queue torque-ops-agent-006 \
  >"${OPS_RUN_DIR}/logs/worker.log" 2>&1 &
worker_pid="$!"

PATH=/nonexistent "${repo_root}/bin/torque-agent" nats worker \
  --nats-url "${nats_url}" \
  --subject "${blocked_subject}" \
  --queue torque-ops-agent-006-blocked \
  >"${OPS_RUN_DIR}/logs/worker-incapable.log" 2>&1 &
blocked_worker_pid="$!"

for _ in $(seq 1 100); do
  if grep -q "nats worker ready" "${OPS_RUN_DIR}/logs/worker.log" 2>/dev/null; then
    break
  fi
  if ! kill -0 "${worker_pid}" 2>/dev/null; then
    ops_fail "torque-agent nats worker exited early; see ${OPS_RUN_DIR}/logs/worker.log"
  fi
  sleep 0.1
done
grep -q "nats worker ready" "${OPS_RUN_DIR}/logs/worker.log" || ops_fail "torque-agent nats worker did not become ready"

for _ in $(seq 1 100); do
  if grep -q "nats worker ready" "${OPS_RUN_DIR}/logs/worker-incapable.log" 2>/dev/null; then
    break
  fi
  if ! kill -0 "${blocked_worker_pid}" 2>/dev/null; then
    ops_fail "incapable torque-agent nats worker exited early; see ${OPS_RUN_DIR}/logs/worker-incapable.log"
  fi
  sleep 0.1
done
grep -q "nats worker ready" "${OPS_RUN_DIR}/logs/worker-incapable.log" || ops_fail "incapable torque-agent nats worker did not become ready"

ops_log "publish durable heartbeat and compact registry"
"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --jetstream \
  --once \
  --tenant lab \
  --agent-id agent-mysql-01 \
  --target-id host/mysql-01 \
  --label role=mysql \
  --label site=lab \
  >"${OPS_RUN_DIR}/logs/heartbeat.log" 2>&1

"${repo_root}/bin/torque" ops agent registry compact \
  --nats-url "${nats_url}" \
  --tenant lab \
  --store file \
  --store-path "${registry_path}" \
  --durable "${registry_durable}" \
  --max-messages 1 \
  --wait 200ms \
  --timeout 5s \
  --format json \
  >"${OPS_RUN_DIR}/verification/registry-compact.json" 2>"${OPS_RUN_DIR}/logs/registry-compact.err"

"${repo_root}/bin/torque" ops agent status \
  --source store \
  --store file \
  --store-path "${registry_path}" \
  --tenant lab \
  --selector role=mysql \
  --format json \
  >"${OPS_RUN_DIR}/verification/status-store.json" 2>"${OPS_RUN_DIR}/logs/status-store.err"

"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --jetstream \
  --once \
  --tenant lab \
  --agent-id agent-nocap-01 \
  --target-id host/nocap-01 \
  --label role=nocap \
  --label site=lab \
  --discover-capabilities=false \
  --capability mysql.replication.verify \
  >"${OPS_RUN_DIR}/logs/heartbeat-nocap.log" 2>&1

"${repo_root}/bin/torque" ops agent registry compact \
  --nats-url "${nats_url}" \
  --tenant lab \
  --store file \
  --store-path "${registry_path}" \
  --durable "${registry_durable}" \
  --max-messages 1 \
  --wait 200ms \
  --timeout 5s \
  --format json \
  >"${OPS_RUN_DIR}/verification/registry-compact-nocap.json" 2>"${OPS_RUN_DIR}/logs/registry-compact-nocap.err"

"${repo_root}/bin/torque" ops agent status \
  --source store \
  --store file \
  --store-path "${registry_path}" \
  --tenant lab \
  --selector role=nocap \
  --format json \
  >"${OPS_RUN_DIR}/verification/status-nocap-store.json" 2>"${OPS_RUN_DIR}/logs/status-nocap-store.err"

cat >"${stack_pass_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: fleet-readiness-pass
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: ${registry_path}
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 95
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
nodes:
  - name: write-pass-marker
    kind: host.command.run
    host:
      transport: nats
      target: ${subject}
      timeout: 10s
      command: |
        printf 'fleet-pass\n' > '${pass_marker}'
YAML

cat >"${stack_block_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: fleet-readiness-block
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: ${registry_path}
    tenant: lab
    selector:
      role: missing
    requireAgents: true
    minReadyPercent: 95
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
nodes:
  - name: write-block-marker
    kind: host.command.run
    host:
      transport: nats
      target: ${subject}
      timeout: 10s
      command: |
        printf 'fleet-block\n' > '${block_marker}'
YAML

cat >"${stack_capability_block_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: fleet-capability-block
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: ${registry_path}
    tenant: lab
    selector:
      role: nocap
    requireAgents: true
    minReadyPercent: 95
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
nodes:
  - name: write-capability-block-marker
    kind: host.command.run
    host:
      transport: nats
      target: ${subject}
      timeout: 10s
      command: |
        printf 'fleet-capability-block\n' > '${capability_block_marker}'
YAML

cat >"${stack_worker_capability_block_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: worker-capability-block
runner:
  mode: fleet
  readiness:
    source: store
    store: file
    storePath: ${registry_path}
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 95
    failureBudget: 0
    staleAfter: 45s
    onInsufficientReady: block
nodes:
  - name: write-worker-capability-block-marker
    kind: host.command.run
    host:
      transport: nats
      target: ${blocked_subject}
      timeout: 10s
      command: |
        printf 'worker-capability-block\n' > '${worker_capability_block_marker}'
YAML

ops_log "apply fleet-ready stack over NATS"
TORQUE_NATS_URL="${nats_url}" "${repo_root}/bin/torque" stack apply --config "${stack_pass_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/pass-stack-apply.log" 2>"${OPS_RUN_DIR}/logs/pass-stack-apply.err"
cp "${pass_marker}" "${OPS_RUN_DIR}/verification/pass-marker.txt"
"${repo_root}/bin/torque" stack audit --config "${stack_pass_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/pass-audit.json" 2>"${OPS_RUN_DIR}/logs/pass-audit.err"
"${repo_root}/bin/torque" stack export --config "${stack_pass_dir}" --out "${OPS_RUN_DIR}/pass-stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/pass-stack-export.log" 2>"${OPS_RUN_DIR}/logs/pass-stack-export.err"

ops_log "prove insufficient readiness blocks before mutation"
set +e
TORQUE_NATS_URL="${nats_url}" "${repo_root}/bin/torque" stack apply --config "${stack_block_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/block-stack-apply.log" 2>"${OPS_RUN_DIR}/logs/block-stack-apply.err"
block_code=$?
set -e
echo "${block_code}" >"${OPS_RUN_DIR}/verification/block-exit-code.txt"
if [[ "${block_code}" -eq 0 ]]; then
  ops_fail "block stack apply unexpectedly succeeded"
fi
if [[ -e "${block_marker}" ]]; then
  touch "${OPS_RUN_DIR}/verification/block-marker-present"
  ops_fail "block marker was written despite fleet readiness gate"
fi
"${repo_root}/bin/torque" stack audit --config "${stack_block_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/block-audit.json" 2>"${OPS_RUN_DIR}/logs/block-audit.err"
"${repo_root}/bin/torque" stack export --config "${stack_block_dir}" --out "${OPS_RUN_DIR}/block-stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/block-stack-export.log" 2>"${OPS_RUN_DIR}/logs/block-stack-export.err"

ops_log "prove missing capability blocks before mutation"
set +e
TORQUE_NATS_URL="${nats_url}" "${repo_root}/bin/torque" stack apply --config "${stack_capability_block_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/capability-block-stack-apply.log" 2>"${OPS_RUN_DIR}/logs/capability-block-stack-apply.err"
capability_block_code=$?
set -e
echo "${capability_block_code}" >"${OPS_RUN_DIR}/verification/capability-block-exit-code.txt"
if [[ "${capability_block_code}" -eq 0 ]]; then
  ops_fail "capability block stack apply unexpectedly succeeded"
fi
if [[ -e "${capability_block_marker}" ]]; then
  touch "${OPS_RUN_DIR}/verification/capability-block-marker-present"
  ops_fail "capability block marker was written despite fleet capability gate"
fi
"${repo_root}/bin/torque" stack audit --config "${stack_capability_block_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/capability-block-audit.json" 2>"${OPS_RUN_DIR}/logs/capability-block-audit.err"
"${repo_root}/bin/torque" stack export --config "${stack_capability_block_dir}" --out "${OPS_RUN_DIR}/capability-block-stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/capability-block-stack-export.log" 2>"${OPS_RUN_DIR}/logs/capability-block-stack-export.err"

ops_log "prove worker-side missing capability rejects assignment"
set +e
TORQUE_NATS_URL="${nats_url}" "${repo_root}/bin/torque" stack apply --config "${stack_worker_capability_block_dir}" --yes \
  >"${OPS_RUN_DIR}/logs/worker-capability-block-stack-apply.log" 2>"${OPS_RUN_DIR}/logs/worker-capability-block-stack-apply.err"
worker_capability_block_code=$?
set -e
echo "${worker_capability_block_code}" >"${OPS_RUN_DIR}/verification/worker-capability-block-exit-code.txt"
if [[ "${worker_capability_block_code}" -eq 0 ]]; then
  ops_fail "worker capability block stack apply unexpectedly succeeded"
fi
if [[ -e "${worker_capability_block_marker}" ]]; then
  touch "${OPS_RUN_DIR}/verification/worker-capability-block-marker-present"
  ops_fail "worker capability block marker was written despite worker-side capability enforcement"
fi
"${repo_root}/bin/torque" stack audit --config "${stack_worker_capability_block_dir}" --output json --include-events --include-artifacts \
  >"${OPS_RUN_DIR}/verification/worker-capability-block-audit.json" 2>"${OPS_RUN_DIR}/logs/worker-capability-block-audit.err"
"${repo_root}/bin/torque" stack export --config "${stack_worker_capability_block_dir}" --out "${OPS_RUN_DIR}/worker-capability-block-stack-run.tgz" \
  >"${OPS_RUN_DIR}/logs/worker-capability-block-stack-export.log" 2>"${OPS_RUN_DIR}/logs/worker-capability-block-stack-export.err"

ops_log "OPS-AGENT-006 passed: ${OPS_RUN_DIR}"
