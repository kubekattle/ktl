#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-TR-007.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --nats-url URL         Reuse an existing NATS server instead of starting one.
  --cleanup              Remove local scratch. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-TR-007 proves the first SSH/NATS bridge slice locally: start NATS, start a
torque-agent NATS worker, run mysql.replication.verify with transport:
nats-mesh, audit the resulting stack artifacts, and export redacted evidence.
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
ops_init_run "OPS-TR-007"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-tr-007.XXXXXX")"
stack_dir="${scratch_root}/stack"
fake_bin="${scratch_root}/bin"
subject="torque.e2e.assign.mysql.${OPS_RUN_ID//[^A-Za-z0-9]/}"
nats_pid=""
worker_pid=""
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
  ops_write_json_object \
    "${OPS_RUN_DIR}/result.json" \
    "status=$([[ ${code} -eq 0 ]] && echo passed || echo failed)" \
    "taskId=${OPS_TASK_ID}" \
    "runId=${OPS_RUN_ID}" \
    "startedAt=${started_at}" \
    "finishedAt=$(ops_utc_now)" \
    "natsUrl=${nats_url}" \
    "subject=${subject}" \
    "cleanup=${cleanup_status}"
  ops_scan_for_secret_material "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/secret-scan.json" >/dev/null 2>&1 || code=1
  ops_write_manifest "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/manifest.json"
  ops_export_bundle "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}"
  echo "evidence=${OPS_RUN_DIR}"
  echo "bundle=${OPS_BUNDLE_PATH}"
  exit "${code}"
}
trap finish EXIT

cd "${repo_root}"
make build build-agent

mkdir -p "${stack_dir}" "${fake_bin}" "${OPS_RUN_DIR}"

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
  nats-server -a 127.0.0.1 -p "${nats_port}" >"${OPS_RUN_DIR}/nats-server.log" 2>&1 &
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

cat >"${fake_bin}/ssh" <<'SH'
#!/bin/sh
last=""
for arg in "$@"; do
  last="$arg"
done
case "$last" in
  *SELECT*COUNT*) printf '1\n' ;;
  *wsrep_cluster_size*) printf 'wsrep_cluster_size\t2\n' ;;
  *wsrep_local_state_comment*) printf 'wsrep_local_state_comment\tSynced\n' ;;
  *) exit 0 ;;
esac
SH
chmod +x "${fake_bin}/ssh"

cat >"${stack_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: nats-mysql-verify
nodes:
  - name: mysql-verify
    kind: mysql.replication.verify
    mysql:
      transport: nats-mesh
      target: ${subject}
      timeout: 10s
      database: torque_ops
      probeTable: replication_probe
      probeId: nats-e2e
      statusPath: ${scratch_root}/mysql-status.txt
      expectedClusterSize: 2
      expectedReplicatedNodes: 2
      stableAttempts: 1
      stableInterval: 1ms
      nodes:
        - id: mysql-00
          address: 10.0.0.10
        - id: mysql-01
          address: 10.0.0.11
YAML

PATH="${fake_bin}:${PATH}" "${repo_root}/bin/torque-agent" nats worker \
  --nats-url "${nats_url}" \
  --subject "${subject}" \
  --queue torque-ops-tr-007 \
  >"${OPS_RUN_DIR}/worker.log" 2>&1 &
worker_pid="$!"

for _ in $(seq 1 100); do
  if grep -q "nats worker ready" "${OPS_RUN_DIR}/worker.log" 2>/dev/null; then
    break
  fi
  if ! kill -0 "${worker_pid}" 2>/dev/null; then
    ops_fail "torque-agent nats worker exited early; see ${OPS_RUN_DIR}/worker.log"
  fi
  sleep 0.1
done
grep -q "nats worker ready" "${OPS_RUN_DIR}/worker.log" || ops_fail "torque-agent nats worker did not become ready"

PATH="${fake_bin}:${PATH}" TORQUE_NATS_URL="${nats_url}" \
  "${repo_root}/bin/torque" stack apply --config "${stack_dir}" --yes \
  >"${OPS_RUN_DIR}/stack-apply.log" 2>"${OPS_RUN_DIR}/stack-apply.err"

"${repo_root}/bin/torque" stack audit --config "${stack_dir}" --output json --include-artifacts \
  >"${OPS_RUN_DIR}/stack-audit.json"

grep -q '"replicatedNodes": 2' "${OPS_RUN_DIR}/stack-audit.json" || ops_fail "audit did not prove replicatedNodes=2"
grep -q 'nats.request' "${OPS_RUN_DIR}/stack-audit.json" || ops_fail "audit did not include nats.request evidence"

"${repo_root}/bin/torque" stack export --config "${stack_dir}" --out "${OPS_RUN_DIR}/stack-run.tgz" \
  >"${OPS_RUN_DIR}/stack-export.log" 2>"${OPS_RUN_DIR}/stack-export.err"

ops_log "OPS-TR-007 passed: ${OPS_RUN_DIR}"
