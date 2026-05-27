#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-FC-KAFKA-RABBITMQ-001.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --destroy-existing   Remove any existing Kafka/RabbitMQ Firecracker labs first.
  --cleanup            Delete Kafka/RabbitMQ lab resources after the run. Default.
  --no-cleanup         Leave both labs, NATS, and workers running.
  -h, --help           Show this help.

STACK-FC-KAFKA-RABBITMQ-001 proves two side-by-side NATS-dispatched
Firecracker labs on the SSH host: a five-node Kafka cluster and a five-node
RabbitMQ cluster. It starts a lab-host NATS server and two
torque-agent NATS workers, plans/applies/reapplies both stackfiles through
transport: nats, audits/exports the runs, and records redacted evidence.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

cleanup_enabled=1
destroy_existing=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --destroy-existing)
      destroy_existing=1
      shift
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Firecracker Kafka/RabbitMQ E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd scp
ops_require_cmd ssh
ops_require_cmd tar

repo_root="$(ops_repo_root)"
kafka_stack_root="${repo_root}/testdata/stack/e2e/26-firecracker-kafka-nats-cluster"
rabbitmq_stack_root="${repo_root}/testdata/stack/e2e/27-firecracker-rabbitmq-nats-cluster"

ops_init_run "STACK-FC-KAFKA-RABBITMQ-001"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-fc-kafka-rabbitmq.XXXXXX")"
subject_suffix="$(printf '%s' "${OPS_RUN_ID}" | tr '[:upper:]_' '[:lower:]-' | tr -cd 'a-z0-9-' | cut -c1-32)"
cksum_value="$(printf '%s' "${OPS_RUN_ID}" | cksum | awk '{print $1}')"
remote_nats_port="${TORQUE_LAB_NATS_PORT:-$((4300 + (cksum_value % 1000)))}"
local_nats_port="$(
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
remote_root="/var/lib/torque-firecracker-k8s/nats-kafka-rabbitmq/${OPS_RUN_ID}"
remote_logs="${remote_root}/logs"
remote_bin="${remote_root}/bin"
remote_nats_url="nats://127.0.0.1:${remote_nats_port}"
nats_url="nats://127.0.0.1:${local_nats_port}"
kafka_subject="${TORQUE_KAFKA_NATS_LAB_SUBJECT:-torque.lab.kafka.${subject_suffix}}"
rabbitmq_subject="${TORQUE_RABBITMQ_NATS_LAB_SUBJECT:-torque.lab.rabbitmq.${subject_suffix}}"
tunnel_control="/tmp/tq-nats-${subject_suffix}.ctl"
kafka_apply_run_id=""
kafka_reapply_run_id=""
rabbitmq_apply_run_id=""
rabbitmq_reapply_run_id=""
kafka_stack_applied=0
rabbitmq_stack_applied=0
tunnel_started=0
control_plane_started=0

ops_set_ssh_base_args

remote_exec() {
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "$@"
}

remote_cat() {
  local remote_path="$1"
  local local_path="$2"
  mkdir -p "$(dirname "${local_path}")"
  remote_exec "test -f '${remote_path}' && cat '${remote_path}'" >"${local_path}" 2>/dev/null || true
}

wait_for_local_nats() {
  python3 - "${local_nats_port}" <<'PY'
import socket
import sys
import time

port = int(sys.argv[1])
deadline = time.time() + 20
last = None
while time.time() < deadline:
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=0.2):
            sys.exit(0)
    except OSError as exc:
        last = exc
        time.sleep(0.2)
raise SystemExit(f"NATS tunnel did not become reachable: {last}")
PY
}

build_binaries() {
  mkdir -p "${OPS_RUN_DIR}/build" "${scratch_root}/bin"
  ops_log "build local torque binary"
  make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1

  ops_log "build linux torque-agent"
  GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${scratch_root}/bin/torque-agent" "${repo_root}/cmd/torque-agent" >"${OPS_RUN_DIR}/build/torque-agent.out" 2>&1

  ops_log "build linux nats-server"
  nats_build_dir="${scratch_root}/nats-server-build"
  mkdir -p "${nats_build_dir}"
  (
    cd "${nats_build_dir}"
    go mod init torque-nats-server-build >/dev/null 2>&1
    go get github.com/nats-io/nats-server/v2@v2.10.26 >/dev/null 2>&1
    GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${scratch_root}/bin/nats-server" github.com/nats-io/nats-server/v2
  ) >"${OPS_RUN_DIR}/build/nats-server.out" 2>&1
}

copy_remote_binary() {
  local local_path="$1"
  local remote_path="$2"
  local name
  name="$(basename "${local_path}")"
  for attempt in 1 2 3; do
    if scp "${OPS_SSH_ARGS[@]}" "${local_path}" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_path}" >"${OPS_RUN_DIR}/remote/scp-${name}-${attempt}.out" 2>"${OPS_RUN_DIR}/remote/scp-${name}-${attempt}.stderr"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

install_remote_control_plane() {
  ops_log "install NATS control plane on lab host"
  remote_exec "mkdir -p '${remote_bin}' '${remote_logs}' '${remote_root}/nats'"
  copy_remote_binary "${scratch_root}/bin/torque-agent" "${remote_bin}/torque-agent"
  copy_remote_binary "${scratch_root}/bin/nats-server" "${remote_bin}/nats-server"
}

destroy_existing_labs() {
  [[ "${destroy_existing}" == "1" ]] || return 0
  ops_log "destroy existing Kafka/RabbitMQ Firecracker labs"
  remote_exec "set +e
cleanup_lab() {
  local root=\"\$1\"
  local subnet=\"\$2\"
  local bridge=\"\$3\"
  local tap_prefix=\"\$4\"
  local run_id=\"\$5\"
  if [ -x \"\${root}/bootstrap-firecracker-k3s.sh\" ]; then
    RUN_ROOT=\"\${root}\" NODE_COUNT=5 SUBNET_OCTET=\"\${subnet}\" BRIDGE_NAME=\"\${bridge}\" TAP_PREFIX=\"\${tap_prefix}\" RUN_ID=\"\${run_id}\" \"\${root}/bootstrap-firecracker-k3s.sh\" delete
  fi
  for p in \"\${root}\"/vms/*/pid; do [ -f \"\${p}\" ] && kill \"\$(cat \"\${p}\")\" 2>/dev/null; done
  sleep 1
  for p in \"\${root}\"/vms/*/pid; do [ -f \"\${p}\" ] && kill -9 \"\$(cat \"\${p}\")\" 2>/dev/null; done
  for i in \$(seq 0 4); do ip link del \"\${tap_prefix}\${i}\" 2>/dev/null; done
  ip link set \"\${bridge}\" down 2>/dev/null
  ip link del \"\${bridge}\" type bridge 2>/dev/null
  iptables -t nat -D POSTROUTING -s \"172.31.\${subnet}.0/24\" ! -o \"\${bridge}\" -j MASQUERADE 2>/dev/null
  rm -rf \"\${root}\"
}
cleanup_lab /var/lib/torque-firecracker-k8s/kafka-cluster 233 tqfckafka tqkf kafka-cluster
cleanup_lab /var/lib/torque-firecracker-k8s/rabbitmq-cluster 234 tqfcrmq tqrmq rabbitmq-cluster
true" >"${OPS_RUN_DIR}/remote/destroy-existing.out" 2>"${OPS_RUN_DIR}/remote/destroy-existing.stderr"
}

start_remote_control_plane() {
  ops_log "start remote NATS server and workers"
  remote_exec "set -euo pipefail
if [ -f '${remote_root}/nats-server.pid' ]; then kill \"\$(cat '${remote_root}/nats-server.pid')\" 2>/dev/null || true; fi
if [ -f '${remote_root}/kafka-worker.pid' ]; then kill \"\$(cat '${remote_root}/kafka-worker.pid')\" 2>/dev/null || true; fi
if [ -f '${remote_root}/rabbitmq-worker.pid' ]; then kill \"\$(cat '${remote_root}/rabbitmq-worker.pid')\" 2>/dev/null || true; fi
nohup '${remote_bin}/nats-server' -js -sd '${remote_root}/nats' -a 127.0.0.1 -p '${remote_nats_port}' >'${remote_logs}/nats-server.log' 2>&1 &
echo \$! > '${remote_root}/nats-server.pid'
for i in \$(seq 1 100); do
  if (echo > /dev/tcp/127.0.0.1/${remote_nats_port}) >/dev/null 2>&1; then
    break
  fi
  sleep 0.2
done
(echo > /dev/tcp/127.0.0.1/${remote_nats_port}) >/dev/null 2>&1
nohup '${remote_bin}/torque-agent' nats worker --nats-url '${remote_nats_url}' --subject '${kafka_subject}' --queue torque-lab-kafka --timeout 90m --shell /bin/bash --agent-id 'kafka-${subject_suffix}' --worker-id 'kafka-${subject_suffix}-a' --tenant lab --target-id host/firecracker-kafka --capability host.command.run >'${remote_logs}/kafka-worker.log' 2>&1 &
echo \$! > '${remote_root}/kafka-worker.pid'
nohup '${remote_bin}/torque-agent' nats worker --nats-url '${remote_nats_url}' --subject '${rabbitmq_subject}' --queue torque-lab-rabbitmq --timeout 90m --shell /bin/bash --agent-id 'rabbitmq-${subject_suffix}' --worker-id 'rabbitmq-${subject_suffix}-a' --tenant lab --target-id host/firecracker-rabbitmq --capability host.command.run >'${remote_logs}/rabbitmq-worker.log' 2>&1 &
echo \$! > '${remote_root}/rabbitmq-worker.pid'"
  control_plane_started=1

  remote_exec "set -euo pipefail
for log in '${remote_logs}/kafka-worker.log' '${remote_logs}/rabbitmq-worker.log'; do
  for i in \$(seq 1 120); do
    if grep -q 'nats worker ready' \"\${log}\" 2>/dev/null; then
      break
    fi
    sleep 0.5
  done
  grep -q 'nats worker ready' \"\${log}\"
done"
}

start_nats_tunnel() {
  ops_log "open local tunnel to remote NATS"
  ssh "${OPS_SSH_ARGS[@]}" \
    -M -S "${tunnel_control}" \
    -fN -L "127.0.0.1:${local_nats_port}:127.0.0.1:${remote_nats_port}" \
    "$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  tunnel_started=1
  wait_for_local_nats
}

stop_nats_tunnel() {
  [[ "${tunnel_started}" == "1" ]] || return 0
  ssh -S "${tunnel_control}" -O exit "$(ops_ssh_target "${TORQUE_LAB_SSH}")" >/dev/null 2>&1 || true
  tunnel_started=0
}

stop_remote_control_plane() {
  [[ "${control_plane_started}" == "1" ]] || return 0
  remote_exec "set +e
for pid_file in '${remote_root}/kafka-worker.pid' '${remote_root}/rabbitmq-worker.pid' '${remote_root}/nats-server.pid'; do
  [ -f \"\${pid_file}\" ] && kill \"\$(cat \"\${pid_file}\")\" 2>/dev/null
done
sleep 1
for pid_file in '${remote_root}/kafka-worker.pid' '${remote_root}/rabbitmq-worker.pid' '${remote_root}/nats-server.pid'; do
  [ -f \"\${pid_file}\" ] && kill -9 \"\$(cat \"\${pid_file}\")\" 2>/dev/null
done
rm -rf '${remote_root}'
true" >"${OPS_RUN_DIR}/remote/stop-control-plane.out" 2>"${OPS_RUN_DIR}/remote/stop-control-plane.stderr" || true
  control_plane_started=0
}

latest_stack_run_id() {
  local stack_root="$1"
  (
    cd "${repo_root}"
    ./bin/torque stack runs --config "${stack_root}" --output json --limit 1
  ) | python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
}

run_service_stack() {
  local service="$1"
  local stack_root="$2"
  local subject_env="$3"
  local subject="$4"
  local service_dir="${OPS_RUN_DIR}/stack/${service}"
  mkdir -p "${service_dir}"

  ops_log "plan ${service} NATS stack"
  (
    cd "${repo_root}"
    env TORQUE_NATS_URL="${nats_url}" "${subject_env}=${subject}" \
      ./bin/torque stack plan --config "${stack_root}" --output json
  ) >"${service_dir}/plan.json" 2>"${service_dir}/plan.stderr"

  ops_log "apply ${service} NATS stack"
  (
    cd "${repo_root}"
    env TORQUE_NATS_URL="${nats_url}" "${subject_env}=${subject}" \
      ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
  ) >"${service_dir}/apply.jsonl" 2>"${service_dir}/apply.stderr"

  case "${service}" in
    kafka) kafka_stack_applied=1 ;;
    rabbitmq) rabbitmq_stack_applied=1 ;;
  esac

  local apply_run_id
  apply_run_id="$(latest_stack_run_id "${stack_root}")"
  [[ -n "${apply_run_id}" ]] || ops_fail "failed to discover ${service} apply run ID"
  printf '%s\n' "${apply_run_id}" >"${service_dir}/apply-run-id.txt"
  case "${service}" in
    kafka) kafka_apply_run_id="${apply_run_id}" ;;
    rabbitmq) rabbitmq_apply_run_id="${apply_run_id}" ;;
  esac

  ops_log "reapply ${service} NATS stack for idempotence"
  (
    cd "${repo_root}"
    env TORQUE_NATS_URL="${nats_url}" "${subject_env}=${subject}" \
      ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
  ) >"${service_dir}/reapply.jsonl" 2>"${service_dir}/reapply.stderr"

  local reapply_run_id
  reapply_run_id="$(latest_stack_run_id "${stack_root}")"
  [[ -n "${reapply_run_id}" ]] || ops_fail "failed to discover ${service} reapply run ID"
  printf '%s\n' "${reapply_run_id}" >"${service_dir}/reapply-run-id.txt"
  case "${service}" in
    kafka) kafka_reapply_run_id="${reapply_run_id}" ;;
    rabbitmq) rabbitmq_reapply_run_id="${reapply_run_id}" ;;
  esac

  ops_log "audit and export ${service} NATS stack"
  (
    cd "${repo_root}"
    ./bin/torque stack audit --config "${stack_root}" --run-id "${reapply_run_id}" --output json --include-artifacts
  ) >"${service_dir}/audit.json" 2>"${service_dir}/audit.stderr"
  (
    cd "${repo_root}"
    ./bin/torque stack export --config "${stack_root}" --run-id "${reapply_run_id}" --out "${service_dir}/stack-export.tgz"
  ) >"${service_dir}/export.out" 2>"${service_dir}/export.stderr"
}

delete_service_stack() {
  local service="$1"
  local stack_root="$2"
  local subject_env="$3"
  local subject="$4"
  local service_dir="${OPS_RUN_DIR}/cleanup/${service}"
  mkdir -p "${service_dir}"
  (
    cd "${repo_root}"
    env TORQUE_NATS_URL="${nats_url}" "${subject_env}=${subject}" \
      ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
  ) >"${service_dir}/delete.jsonl" 2>"${service_dir}/delete.stderr"
}

collect_remote_evidence() {
  mkdir -p "${OPS_RUN_DIR}/remote/kafka" "${OPS_RUN_DIR}/remote/rabbitmq" "${OPS_RUN_DIR}/remote/control"
  remote_cat "/var/lib/torque-firecracker-k8s/kafka-cluster/receipt.json" "${OPS_RUN_DIR}/remote/kafka/receipt.json"
  remote_cat "/var/lib/torque-firecracker-k8s/kafka-cluster/nodes.txt" "${OPS_RUN_DIR}/remote/kafka/nodes.txt"
  remote_cat "/var/lib/torque-firecracker-k8s/kafka-cluster/pods.txt" "${OPS_RUN_DIR}/remote/kafka/pods.txt"
  remote_cat "/var/lib/torque-firecracker-k8s/rabbitmq-cluster/receipt.json" "${OPS_RUN_DIR}/remote/rabbitmq/receipt.json"
  remote_cat "/var/lib/torque-firecracker-k8s/rabbitmq-cluster/nodes.txt" "${OPS_RUN_DIR}/remote/rabbitmq/nodes.txt"
  remote_cat "/var/lib/torque-firecracker-k8s/rabbitmq-cluster/pods.txt" "${OPS_RUN_DIR}/remote/rabbitmq/pods.txt"
  remote_cat "${remote_logs}/nats-server.log" "${OPS_RUN_DIR}/remote/control/nats-server.log"
  remote_cat "${remote_logs}/kafka-worker.log" "${OPS_RUN_DIR}/remote/control/kafka-worker.log"
  remote_cat "${remote_logs}/rabbitmq-worker.log" "${OPS_RUN_DIR}/remote/control/rabbitmq-worker.log"
}

write_standard_artifacts() {
  local code="$1"
  local cleanup_status="$2"
  local cleanup_performed="$3"
  python3 - \
    "${OPS_RUN_DIR}" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${started_at}" \
    "${TORQUE_LAB_SSH}" \
    "${remote_root}" \
    "${nats_url}" \
    "${remote_nats_url}" \
    "${kafka_subject}" \
    "${rabbitmq_subject}" \
    "${kafka_apply_run_id}" \
    "${kafka_reapply_run_id}" \
    "${rabbitmq_apply_run_id}" \
    "${rabbitmq_reapply_run_id}" \
    "${cleanup_status}" \
    "${cleanup_performed}" \
    "${code}" <<'PY'
import json
import sys
import time
from pathlib import Path

(
    run_dir,
    task_id,
    run_id,
    started_at,
    lab_ssh,
    remote_root,
    nats_url,
    remote_nats_url,
    kafka_subject,
    rabbitmq_subject,
    kafka_apply_run_id,
    kafka_reapply_run_id,
    rabbitmq_apply_run_id,
    rabbitmq_reapply_run_id,
    cleanup_status,
    cleanup_performed,
    exit_code,
) = sys.argv[1:18]

run = Path(run_dir)
code = int(exit_code)

def load(rel: str) -> dict:
    path = run / rel
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}

def write(rel: str, doc: dict) -> None:
    path = run / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

kafka_receipt = load("remote/kafka/receipt.json")
rabbitmq_receipt = load("remote/rabbitmq/receipt.json")
kafka_audit = load("stack/kafka/audit.json")
rabbitmq_audit = load("stack/rabbitmq/audit.json")

checks = {
    "natsTransport": bool(nats_url and kafka_subject and rabbitmq_subject),
    "kafkaClusterReceipt": kafka_receipt.get("status") == "succeeded",
    "rabbitmqClusterReceipt": rabbitmq_receipt.get("status") == "succeeded",
    "kafkaReadyNodes": int(kafka_receipt.get("readyCount") or 0),
    "rabbitmqReadyNodes": int(rabbitmq_receipt.get("readyCount") or 0),
    "kafkaAuditStatus": kafka_audit.get("status"),
    "rabbitmqAuditStatus": rabbitmq_audit.get("status"),
    "kafkaExportExists": (run / "stack" / "kafka" / "stack-export.tgz").is_file(),
    "rabbitmqExportExists": (run / "stack" / "rabbitmq" / "stack-export.tgz").is_file(),
}
verification_ok = (
    code == 0
    and checks["natsTransport"]
    and checks["kafkaClusterReceipt"]
    and checks["rabbitmqClusterReceipt"]
    and checks["kafkaReadyNodes"] == 5
    and checks["rabbitmqReadyNodes"] == 5
    and checks["kafkaAuditStatus"] == "succeeded"
    and checks["rabbitmqAuditStatus"] == "succeeded"
    and checks["kafkaExportExists"]
    and checks["rabbitmqExportExists"]
)
overall_ok = verification_ok and cleanup_status == "succeeded"
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "host": lab_ssh,
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "host/firecracker-lab", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {"id": "nats/lab-control-plane", "type": "nats", "transport": "ssh-tunnel", "url": nats_url, "remoteUrl": remote_nats_url},
        {"id": "worker/kafka", "type": "torque-agent", "transport": "nats", "subject": kafka_subject, "capability": "host.command.run"},
        {"id": "worker/rabbitmq", "type": "torque-agent", "transport": "nats", "subject": rabbitmq_subject, "capability": "host.command.run"},
        {"id": "cluster/kafka-firecracker-k3s", "type": "kubernetes", "nodeCount": 5, "subnet": "172.31.233.0/24"},
        {"id": "cluster/rabbitmq-firecracker-k3s", "type": "kubernetes", "nodeCount": 5, "subnet": "172.31.234.0/24"},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-nats-dispatched-firecracker-kafka-rabbitmq-change",
    "status": "succeeded" if overall_ok else "blocked",
    "evidence": {
        "kafkaApplyRunId": kafka_apply_run_id,
        "kafkaReapplyRunId": kafka_reapply_run_id,
        "rabbitmqApplyRunId": rabbitmq_apply_run_id,
        "rabbitmqReapplyRunId": rabbitmq_reapply_run_id,
        "remoteRoot": remote_root,
        "checks": checks,
    },
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if verification_ok else "failed",
    "checks": checks,
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if cleanup_status == "succeeded" else "failed",
    "cleanupPerformed": cleanup_performed == "true",
    "remoteRoot": remote_root,
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if overall_ok else "failed",
    "finishedAt": finished_at,
    "kafkaApplyRunId": kafka_apply_run_id,
    "kafkaReapplyRunId": kafka_reapply_run_id,
    "rabbitmqApplyRunId": rabbitmq_apply_run_id,
    "rabbitmqReapplyRunId": rabbitmq_reapply_run_id,
    "kafkaReadyNodes": checks["kafkaReadyNodes"],
    "rabbitmqReadyNodes": checks["rabbitmqReadyNodes"],
    "kafkaSubject": kafka_subject,
    "rabbitmqSubject": rabbitmq_subject,
    "remoteRoot": remote_root,
    "cleanupStatus": cleanup_status,
})
PY
}

finish() {
  local code=$?
  local cleanup_status="succeeded"
  local cleanup_performed="false"
  trap - EXIT
  set +e
  mkdir -p "${OPS_RUN_DIR}/cleanup" "${OPS_RUN_DIR}/remote"
  collect_remote_evidence
  if [[ "${cleanup_enabled}" == "1" ]]; then
    cleanup_performed="true"
    if [[ "${rabbitmq_stack_applied}" == "1" ]]; then
      ops_log "delete RabbitMQ NATS stack"
      delete_service_stack rabbitmq "${rabbitmq_stack_root}" TORQUE_RABBITMQ_NATS_LAB_SUBJECT "${rabbitmq_subject}" || cleanup_status="failed"
    fi
    if [[ "${kafka_stack_applied}" == "1" ]]; then
      ops_log "delete Kafka NATS stack"
      delete_service_stack kafka "${kafka_stack_root}" TORQUE_KAFKA_NATS_LAB_SUBJECT "${kafka_subject}" || cleanup_status="failed"
    fi
    collect_remote_evidence
    stop_remote_control_plane
  fi
  stop_nats_tunnel
  write_standard_artifacts "${code}" "${cleanup_status}" "${cleanup_performed}"
  if [[ "${cleanup_status}" != "succeeded" ]]; then
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

mkdir -p "${OPS_RUN_DIR}/stack/kafka" "${OPS_RUN_DIR}/stack/rabbitmq" "${OPS_RUN_DIR}/remote"

build_binaries
install_remote_control_plane
destroy_existing_labs
start_remote_control_plane
start_nats_tunnel

run_service_stack kafka "${kafka_stack_root}" TORQUE_KAFKA_NATS_LAB_SUBJECT "${kafka_subject}"
run_service_stack rabbitmq "${rabbitmq_stack_root}" TORQUE_RABBITMQ_NATS_LAB_SUBJECT "${rabbitmq_subject}"
collect_remote_evidence
