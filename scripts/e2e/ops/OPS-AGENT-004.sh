#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-AGENT-004.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing NATS server instead of starting one.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-AGENT-004 proves the first NATS fleet control-plane slice locally: start
NATS, publish live torque-agent heartbeats, collect them with
torque ops agent status, and export a redacted evidence bundle.
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
ops_init_run "OPS-AGENT-004"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-agent-004.XXXXXX")"
agent_mysql_pid=""
agent_web_pid=""
nats_pid=""
nats_url="${external_nats_url}"
cleanup_status="pending"

finish() {
  local code=$?
  trap - EXIT
  set +e
  if [[ -n "${agent_mysql_pid}" ]]; then
    kill "${agent_mysql_pid}" 2>/dev/null
    wait "${agent_mysql_pid}" 2>/dev/null
  fi
  if [[ -n "${agent_web_pid}" ]]; then
    kill "${agent_web_pid}" 2>/dev/null
    wait "${agent_web_pid}" 2>/dev/null
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

  python3 - "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "${nats_url}" "${cleanup_status}" "${code}" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
nats_url = sys.argv[5]
cleanup_status = sys.argv[6]
exit_code = int(sys.argv[7])
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

all_status = load("verification/status-all.json")
mysql_status = load("verification/status-mysql.json")
errors = []
if not all_status:
    errors.append("missing all-agent status snapshot")
if not mysql_status:
    errors.append("missing mysql selector status snapshot")
if all_status and all_status.get("summary", {}).get("total") != 2:
    errors.append("all-agent summary.total must be 2")
if all_status and all_status.get("summary", {}).get("ready") != 2:
    errors.append("all-agent summary.ready must be 2")
if mysql_status and mysql_status.get("summary", {}).get("total") != 1:
    errors.append("mysql selector summary.total must be 1")
if mysql_status and mysql_status.get("agents", [{}])[0].get("agentId") != "agent-mysql-01":
    errors.append("mysql selector must return agent-mysql-01")
status = "succeeded" if exit_code == 0 and not errors else "failed"

write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["local.nats", "ops.agent"],
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "nats/local", "type": "nats", "url": nats_url},
        {"id": "agent/agent-mysql-01", "type": "torque-agent", "tenant": "lab", "labels": {"role": "mysql", "site": "lab"}},
        {"id": "agent/agent-web-01", "type": "torque-agent", "tenant": "lab", "labels": {"role": "web", "site": "lab"}},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "collect-agent-heartbeats-over-nats",
    "status": "succeeded" if status == "succeeded" else "blocked",
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "allSummary": all_status.get("summary"),
    "mysqlSummary": mysql_status.get("summary"),
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
    "status": status,
    "finishedAt": finished_at,
    "natsUrl": nats_url,
})
if status != "succeeded":
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
    ops_log "build local nats-server"
    nats_build_dir="${scratch_root}/nats-server-build"
    mkdir -p "${nats_build_dir}"
    (
      cd "${nats_build_dir}"
      go mod init torque-nats-server-build >/dev/null 2>&1
      go get github.com/nats-io/nats-server/v2@v2.10.26 >/dev/null 2>&1
      go build -o "${scratch_root}/bin/nats-server" github.com/nats-io/nats-server/v2
    ) >"${OPS_RUN_DIR}/build/nats-server.out" 2>&1
    nats_bin="${scratch_root}/bin/nats-server"
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
  "${nats_bin}" -a 127.0.0.1 -p "${nats_port}" >"${OPS_RUN_DIR}/logs/nats-server.log" 2>&1 &
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

"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --tenant lab \
  --agent-id agent-mysql-01 \
  --target-id host/mysql-01 \
  --label role=mysql \
  --label site=lab \
  --capability host.file.ensure \
  --capability mysql.replication.verify \
  --interval 100ms \
  --slots 4 \
  >"${OPS_RUN_DIR}/logs/agent-mysql-01.log" 2>&1 &
agent_mysql_pid="$!"

"${repo_root}/bin/torque-agent" nats heartbeat \
  --nats-url "${nats_url}" \
  --tenant lab \
  --agent-id agent-web-01 \
  --target-id host/web-01 \
  --label role=web \
  --label site=lab \
  --capability host.file.ensure \
  --interval 100ms \
  --slots 2 \
  >"${OPS_RUN_DIR}/logs/agent-web-01.log" 2>&1 &
agent_web_pid="$!"

for log in "${OPS_RUN_DIR}/logs/agent-mysql-01.log" "${OPS_RUN_DIR}/logs/agent-web-01.log"; do
  for _ in $(seq 1 100); do
    if grep -q "nats heartbeat published" "${log}" 2>/dev/null; then
      break
    fi
    if [[ "${log}" == *mysql* ]] && ! kill -0 "${agent_mysql_pid}" 2>/dev/null; then
      ops_fail "mysql heartbeat agent exited early; see ${log}"
    fi
    if [[ "${log}" == *web* ]] && ! kill -0 "${agent_web_pid}" 2>/dev/null; then
      ops_fail "web heartbeat agent exited early; see ${log}"
    fi
    sleep 0.1
  done
  grep -q "nats heartbeat published" "${log}" || ops_fail "heartbeat agent did not publish; see ${log}"
done

"${repo_root}/bin/torque" ops agent status \
  --nats-url "${nats_url}" \
  --tenant lab \
  --timeout 1500ms \
  --format json \
  >"${OPS_RUN_DIR}/verification/status-all.json" 2>"${OPS_RUN_DIR}/verification/status-all.err"

"${repo_root}/bin/torque" ops agent status \
  --nats-url "${nats_url}" \
  --tenant lab \
  --selector role=mysql \
  --timeout 1500ms \
  --format json \
  >"${OPS_RUN_DIR}/verification/status-mysql.json" 2>"${OPS_RUN_DIR}/verification/status-mysql.err"

"${repo_root}/bin/torque" ops agent status \
  --nats-url "${nats_url}" \
  --tenant lab \
  --selector role=mysql \
  --timeout 500ms \
  >"${OPS_RUN_DIR}/verification/status-mysql-table.txt" 2>"${OPS_RUN_DIR}/verification/status-mysql-table.err"

python3 - "${OPS_RUN_DIR}/verification/status-all.json" "${OPS_RUN_DIR}/verification/status-mysql.json" <<'PY'
import json
import sys
from pathlib import Path

all_status = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
mysql_status = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
assert all_status["apiVersion"] == "torque.dev/agent-registry/v1"
assert all_status["kind"] == "AgentStatusSnapshot"
assert all_status["summary"]["total"] == 2, all_status
assert all_status["summary"]["ready"] == 2, all_status
assert mysql_status["summary"]["total"] == 1, mysql_status
assert mysql_status["agents"][0]["agentId"] == "agent-mysql-01", mysql_status
assert mysql_status["agents"][0]["labels"]["role"] == "mysql", mysql_status
PY

grep -q 'agent-mysql-01' "${OPS_RUN_DIR}/verification/status-mysql-table.txt"
grep -q 'torque.v1.agent.heartbeat.lab.' "${OPS_RUN_DIR}/logs/agent-mysql-01.log"

ops_log "OPS-AGENT-004 passed: ${OPS_RUN_DIR}"
