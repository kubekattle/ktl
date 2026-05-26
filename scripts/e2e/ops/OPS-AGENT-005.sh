#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-005.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing JetStream-enabled NATS server.
  --etcd-endpoint URL    Reuse an existing etcd endpoint.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-005 proves durable agent registry compaction: publish
torque-agent heartbeats through JetStream, consume them with a durable registry
consumer, write compact status into etcd, and read it back through
torque ops agent status --source store.
EOF
}

cleanup_enabled=1
external_nats_url=""
external_etcd_endpoint=""

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
    --etcd-endpoint)
      [[ $# -ge 2 ]] || ops_fail "--etcd-endpoint requires a value"
      external_etcd_endpoint="$2"
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
ops_init_run "OPS-AGENT-005"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-005.XXXXXX")"
nats_pid=""
etcd_pid=""
nats_url="${external_nats_url}"
etcd_endpoint="${external_etcd_endpoint}"
cleanup_status="pending"

finish() {
  local code=$?
  trap - EXIT
  set +e
  if [[ -n "${nats_pid}" ]]; then
    kill "${nats_pid}" 2>/dev/null
    wait "${nats_pid}" 2>/dev/null
  fi
  if [[ -n "${etcd_pid}" ]]; then
    kill "${etcd_pid}" 2>/dev/null
    wait "${etcd_pid}" 2>/dev/null
  fi
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    cleanup_status="removed"
  else
    cleanup_status="kept:${scratch_root}"
  fi

  python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${etcd_endpoint}" "${cleanup_status}" "${code}" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
nats_url = sys.argv[5]
etcd_endpoint = sys.argv[6]
cleanup_status = sys.argv[7]
exit_code = int(sys.argv[8])
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

compact = load("verification/registry-compact.json")
status = load("verification/status-store.json")
errors = []
if not compact:
    errors.append("missing registry compact result")
if not status:
    errors.append("missing store-backed status snapshot")
if compact and compact.get("stored") != 2:
    errors.append("registry compact stored count must be 2")
if compact and compact.get("lastSequence", 0) < 2:
    errors.append("registry compact lastSequence must be at least 2")
if status and status.get("summary", {}).get("total") != 2:
    errors.append("store-backed status summary.total must be 2")
if status and status.get("summary", {}).get("ready") != 2:
    errors.append("store-backed status summary.ready must be 2")
mysql = [agent for agent in status.get("agents", []) if agent.get("agentId") == "agent-mysql-01"]
if not mysql:
    errors.append("store-backed status missing agent-mysql-01")
elif not mysql[0].get("evidenceOffset", {}).get("sequence"):
    errors.append("agent-mysql-01 missing evidenceOffset.sequence")
run_status = "succeeded" if exit_code == 0 and not errors else "failed"

write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["local.nats.jetstream", "local.etcd", "ops.agent.registry"],
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "nats/local-jetstream", "type": "nats-jetstream", "url": nats_url, "stream": "TORQUE_AGENT_EVENTS"},
        {"id": "etcd/local", "type": "etcd", "endpoint": etcd_endpoint, "prefix": "/torque/e2e"},
        {"id": "agent/agent-mysql-01", "type": "torque-agent", "tenant": "lab", "labels": {"role": "mysql", "site": "lab"}},
        {"id": "agent/agent-web-01", "type": "torque-agent", "tenant": "lab", "labels": {"role": "web", "site": "lab"}},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "compact-jetstream-heartbeats-to-etcd-registry",
    "status": "succeeded" if run_status == "succeeded" else "blocked",
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": run_status,
    "compact": compact,
    "summary": status.get("summary"),
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
    "etcdEndpoint": etcd_endpoint,
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/logs" "${OPS_RUN_DIR}/verification" "${scratch_root}/bin"

ops_log "build torque and torque-agent"
make -C "${repo_root}" -s build build-agent >"${OPS_RUN_DIR}/build/make-build.out" 2>&1

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

if [[ -z "${etcd_endpoint}" ]]; then
  if command -v etcd >/dev/null 2>&1; then
    etcd_bin="$(command -v etcd)"
  else
    ops_log "build local etcd"
    etcd_build_dir="${scratch_root}/etcd-build"
    mkdir -p "${etcd_build_dir}"
    (
      cd "${etcd_build_dir}"
      go mod init torque-etcd-build >/dev/null 2>&1
      go get go.etcd.io/etcd/server/v3@v3.6.5 >/dev/null 2>&1
      go build -o "${scratch_root}/bin/etcd" go.etcd.io/etcd/server/v3
    ) >"${OPS_RUN_DIR}/build/etcd.out" 2>&1
    etcd_bin="${scratch_root}/bin/etcd"
  fi
  etcd_client_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
  etcd_peer_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
  etcd_endpoint="http://127.0.0.1:${etcd_client_port}"
  "${etcd_bin}" \
    --name torque-agent-005 \
    --data-dir "${scratch_root}/etcd" \
    --listen-client-urls "${etcd_endpoint}" \
    --advertise-client-urls "${etcd_endpoint}" \
    --listen-peer-urls "http://127.0.0.1:${etcd_peer_port}" \
    --initial-advertise-peer-urls "http://127.0.0.1:${etcd_peer_port}" \
    --initial-cluster "torque-agent-005=http://127.0.0.1:${etcd_peer_port}" \
    --initial-cluster-token torque-agent-005 \
    --initial-cluster-state new \
    --logger zap \
    >"${OPS_RUN_DIR}/logs/etcd.log" 2>&1 &
  etcd_pid="$!"
fi

python3 - "${nats_url}" "${etcd_endpoint}" <<'PY'
import socket
import sys
import time
from urllib.parse import urlparse

for raw in sys.argv[1:]:
    url = urlparse(raw)
    host = url.hostname or "127.0.0.1"
    port = url.port
    deadline = time.time() + 20
    last = None
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.2):
                break
        except OSError as exc:
            last = exc
            time.sleep(0.1)
    else:
        raise SystemExit(f"{raw} did not become reachable: {last}")
PY

"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --jetstream \
  --stream TORQUE_AGENT_EVENTS \
  --once \
  --tenant lab \
  --agent-id agent-mysql-01 \
  --target-id host/mysql-01 \
  --label role=mysql \
  --label site=lab \
  --capability host.file.ensure \
  --capability mysql.replication.verify \
  --slots 4 \
  >"${OPS_RUN_DIR}/logs/agent-mysql-01.log" 2>&1

"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --jetstream \
  --stream TORQUE_AGENT_EVENTS \
  --once \
  --tenant lab \
  --agent-id agent-web-01 \
  --target-id host/web-01 \
  --label role=web \
  --label site=lab \
  --capability host.file.ensure \
  --slots 2 \
  >"${OPS_RUN_DIR}/logs/agent-web-01.log" 2>&1

"${repo_root}/bin/torque" ops agent registry compact \
  --nats-url "${nats_url}" \
  --tenant lab \
  --stream TORQUE_AGENT_EVENTS \
  --durable torque-agent-registry-e2e \
  --max-messages 2 \
  --timeout 10s \
  --store etcd \
  --etcd-endpoints "${etcd_endpoint}" \
  --etcd-prefix /torque/e2e \
  --format json \
  >"${OPS_RUN_DIR}/verification/registry-compact.json" 2>"${OPS_RUN_DIR}/verification/registry-compact.err"

"${repo_root}/bin/torque" ops agent status \
  --source store \
  --store etcd \
  --etcd-endpoints "${etcd_endpoint}" \
  --etcd-prefix /torque/e2e \
  --tenant lab \
  --format json \
  >"${OPS_RUN_DIR}/verification/status-store.json" 2>"${OPS_RUN_DIR}/verification/status-store.err"

"${repo_root}/bin/torque" ops agent status \
  --source store \
  --store etcd \
  --etcd-endpoints "${etcd_endpoint}" \
  --etcd-prefix /torque/e2e \
  --tenant lab \
  --selector role=mysql \
  >"${OPS_RUN_DIR}/verification/status-store-mysql-table.txt" 2>"${OPS_RUN_DIR}/verification/status-store-mysql-table.err"

python3 - "${OPS_RUN_DIR}/verification/registry-compact.json" "${OPS_RUN_DIR}/verification/status-store.json" <<'PY'
import json
import sys
from pathlib import Path

compact = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
status = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
assert compact["stored"] == 2, compact
assert compact["lastSequence"] >= 2, compact
assert status["summary"]["total"] == 2, status
assert status["summary"]["ready"] == 2, status
agents = {agent["agentId"]: agent for agent in status["agents"]}
assert agents["agent-mysql-01"]["evidenceOffset"]["stream"] == "TORQUE_AGENT_EVENTS", agents
assert agents["agent-mysql-01"]["evidenceOffset"]["sequence"] >= 1, agents
PY

grep -q 'agent-mysql-01' "${OPS_RUN_DIR}/verification/status-store-mysql-table.txt"
grep -q 'nats heartbeat published' "${OPS_RUN_DIR}/logs/agent-mysql-01.log"

ops_log "OPS-AGENT-005 passed: ${OPS_RUN_DIR}"
