#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-FC-POSTGRES-001.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --vm-mem MIB         Firecracker memory per VM. Default: 768.
  --cleanup            Delete stack and lab resources after the run. Default.
  --no-cleanup         Leave Firecracker VMs running for debugging.
  -h, --help           Show this help.

STACK-FC-POSTGRES-001 proves a NATS-transport stack workflow that builds a
5-node PostgreSQL streaming-replication cluster with PgBouncer on every node.
The stack configures one primary, four replicas, PgBouncer, a replicated probe,
idempotent reapply, audit/export, and cleanup through NATS assignment workers
running inside Firecracker VMs.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

vm_mem_mib=768
cleanup_enabled=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --vm-mem)
      [[ $# -ge 2 ]] || ops_fail "--vm-mem requires a value"
      vm_mem_mib="$2"
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Firecracker/PostgreSQL E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

case "${vm_mem_mib}" in
  ''|*[!0-9]*) ops_fail "--vm-mem must be a positive integer" ;;
esac
[[ "${vm_mem_mib}" -gt 0 ]] || ops_fail "--vm-mem must be > 0"

ops_require_cmd go
ops_require_cmd python3
ops_require_cmd scp
ops_require_cmd ssh
ops_require_cmd tar

repo_root="$(ops_repo_root)"
ops_init_run "STACK-FC-POSTGRES-001"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-fc-postgres.XXXXXX")"
remote_root="/var/lib/torque-firecracker-postgres/${OPS_RUN_ID}"
remote_script="${remote_root}/run-postgres-pgbouncer.sh"
remote_copied=0
remote_complete=0
remote_cleanup_status="not-requested"

copy_remote_evidence() {
  if [[ "${remote_copied}" == "1" || -z "${remote_root}" ]]; then
    return 0
  fi
  mkdir -p "${OPS_RUN_DIR}/remote"
  if scp -r "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_root}/evidence/." "${OPS_RUN_DIR}/remote/" >/dev/null 2>&1; then
    remote_copied=1
    if [[ -f "${OPS_RUN_DIR}/remote/verification/receipt.json" ]]; then
      mkdir -p "${OPS_RUN_DIR}/verification"
      cp "${OPS_RUN_DIR}/remote/verification/receipt.json" "${OPS_RUN_DIR}/verification/receipt.json"
    fi
    if [[ -f "${OPS_RUN_DIR}/remote/result.json" ]]; then
      cp "${OPS_RUN_DIR}/remote/result.json" "${OPS_RUN_DIR}/result.json"
    fi
    return 0
  fi
  return 1
}

cleanup_lab_resources() {
  local status="succeeded"
  copy_remote_evidence || true
  ops_set_ssh_base_args
  if [[ "${cleanup_enabled}" == "1" ]]; then
    if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "if [ -x '${remote_script}' ]; then RUN_ROOT='${remote_root}' '${remote_script}' cleanup-only || true; fi; rm -rf '${remote_root}'"; then
      remote_cleanup_status="deleted"
    else
      remote_cleanup_status="failed"
      status="failed"
    fi
  else
    remote_cleanup_status="kept:${remote_root}"
  fi
  rm -rf "${scratch_root}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    "apiVersion=torque.dev/e2e/v1" \
    "kind=OpsCleanupReceipt" \
    "taskId=${OPS_TASK_ID}" \
    "runId=${OPS_RUN_ID}" \
    "status=${status}" \
    "remoteRoot=${remote_root}" \
    "remote=${remote_cleanup_status}" \
    "remoteEvidenceCopied=${remote_copied}" \
    "cleanupRequested=${cleanup_enabled}" \
    "finishedAt=$(ops_utc_now)"
  [[ "${status}" == "succeeded" ]]
}

finish() {
  local code=$?
  trap - EXIT
  local cleanup_code=0
  cleanup_lab_resources || cleanup_code=$?
  if [[ ${code} -eq 0 && ${cleanup_code} -ne 0 ]]; then
    code="${cleanup_code}"
  fi
  if [[ ! -f "${OPS_RUN_DIR}/verification/receipt.json" ]]; then
    mkdir -p "${OPS_RUN_DIR}/verification"
    ops_write_json_object "${OPS_RUN_DIR}/verification/receipt.json" \
      "apiVersion=torque.dev/e2e/v1" \
      "kind=StackFirecrackerPostgres001Receipt" \
      "taskId=${OPS_TASK_ID}" \
      "runId=${OPS_RUN_ID}" \
      "status=failed" \
      "reason=remote verification receipt was not copied"
    code=1
  fi
  if [[ ! -f "${OPS_RUN_DIR}/result.json" ]]; then
    ops_write_json_object "${OPS_RUN_DIR}/result.json" \
      "apiVersion=torque.dev/e2e/v1" \
      "kind=OpsLabResult" \
      "taskId=${OPS_TASK_ID}" \
      "runId=${OPS_RUN_ID}" \
      "status=failed" \
      "reason=remote result was not copied"
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/remote" "${scratch_root}/bin"
ops_write_json_object "${OPS_RUN_DIR}/metadata.json" \
  "apiVersion=torque.dev/e2e/v1" \
  "kind=OpsLabMetadata" \
  "taskId=${OPS_TASK_ID}" \
  "runId=${OPS_RUN_ID}" \
  "startedAt=${started_at}"
python3 - "${OPS_RUN_DIR}/target-snapshot.json" "${TORQUE_LAB_SSH}" <<'PY'
import json
import sys

path, lab_ssh = sys.argv[1:3]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetSnapshot",
    "targets": [
        {"id": "host/firecracker-postgres-lab", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {
            "id": "vm/postgres-00..04",
            "type": "firecracker-vm-set",
            "transport": "nats-via-firecracker",
            "count": 5,
            "subnet": "172.31.239.0/24",
            "roles": ["postgresql", "pgbouncer"],
        },
    ],
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
ops_write_json_object "${OPS_RUN_DIR}/decision.json" \
  "apiVersion=torque.dev/e2e/v1" \
  "kind=OpsDecision" \
  "taskId=${OPS_TASK_ID}" \
  "runId=${OPS_RUN_ID}" \
  "decision=allow" \
  "status=allowed" \
  "reason=explicit live Firecracker PostgreSQL PgBouncer NATS E2E confirmation"

ops_log "build linux torque binaries"
GOOS=linux GOARCH=amd64 go build -o "${scratch_root}/bin/torque" "${repo_root}/cmd/torque" >"${OPS_RUN_DIR}/build/torque.out" 2>&1
GOOS=linux GOARCH=amd64 go build -o "${scratch_root}/bin/torque-agent" "${repo_root}/cmd/torque-agent" >"${OPS_RUN_DIR}/build/torque-agent.out" 2>&1

ops_log "build linux nats-server"
nats_build_dir="${scratch_root}/nats-server-build"
mkdir -p "${nats_build_dir}"
(
  cd "${nats_build_dir}"
  go mod init torque-nats-server-build >/dev/null 2>&1
  go get github.com/nats-io/nats-server/v2@v2.10.26 >/dev/null 2>&1
  GOOS=linux GOARCH=amd64 go build -o "${scratch_root}/bin/nats-server" github.com/nats-io/nats-server/v2
) >"${OPS_RUN_DIR}/build/nats-server.out" 2>&1

ops_log "install remote PostgreSQL/PgBouncer NATS runner"
ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "mkdir -p '${remote_root}/bin'"
scp "${scratch_root}/bin/torque" "${scratch_root}/bin/torque-agent" "${scratch_root}/bin/nats-server" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_root}/bin/" >/dev/null

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat > '${remote_script}' && chmod +x '${remote_script}'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

mode="${1:-run}"
if [[ "${mode}" == "cleanup-only" && -z "${RUN_ROOT:-}" ]]; then
  RUN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
RUN_ROOT="${RUN_ROOT:?missing RUN_ROOT}"
RUN_ID="${RUN_ID:-$(basename "${RUN_ROOT}")}"
VM_MEM_MIB="${VM_MEM_MIB:-768}"
CLEANUP_ENABLED="${CLEANUP_ENABLED:-1}"

NODE_COUNT=5
SUBNET_OCTET=239
NET_PREFIX="172.31.${SUBNET_OCTET}"
GATEWAY="${NET_PREFIX}.1"
CIDR="${NET_PREFIX}.0/24"
BRIDGE_NAME="tqpg${RUN_ID//[^0-9]/}"
BRIDGE_NAME="${BRIDGE_NAME:0:12}"
TAP_PREFIX="tqp${RUN_ID//[^0-9]/}"
TAP_PREFIX="${TAP_PREFIX:0:8}"
NATS_PORT=4230

BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/firecracker-sandbox-lab/vmlinux.bin}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
TORQUE_BIN="${RUN_ROOT}/bin/torque"
AGENT_BIN="${RUN_ROOT}/bin/torque-agent"
NATS_BIN="${RUN_ROOT}/bin/nats-server"
CACHE_ROOT="${CACHE_ROOT:-/var/lib/torque-firecracker-postgres/cache}"
EVIDENCE_DIR="${RUN_ROOT}/evidence"
STACK_ROOT="${RUN_ROOT}/stack"
VM_ROOT="${RUN_ROOT}/vms"
LOG_DIR="${EVIDENCE_DIR}/logs"

node_ip() {
  printf '%s.%d' "${NET_PREFIX}" "$((10 + $1))"
}

node_name() {
  printf 'postgres-%02d' "$1"
}

subject_for() {
  printf 'torque.pg.%s.%s' "${RUN_ID//[^A-Za-z0-9]/}" "$(node_name "$1")"
}

wait_for_pids() {
  local status=0
  local pid
  for pid in "$@"; do
    if ! wait "${pid}"; then
      status=1
    fi
  done
  return "${status}"
}

cleanup_only() {
  set +e
  if [[ -f "${RUN_ROOT}/nats.pid" ]]; then
    kill "$(cat "${RUN_ROOT}/nats.pid")" 2>/dev/null
  fi
  find "${VM_ROOT}" -name pid -type f 2>/dev/null | while read -r pidfile; do
    kill "$(cat "${pidfile}")" 2>/dev/null
  done
  sleep 1
  find "${VM_ROOT}" -name pid -type f 2>/dev/null | while read -r pidfile; do
    kill -9 "$(cat "${pidfile}")" 2>/dev/null
  done
  for link in $(ip -o link show | awk -F': ' -v b="${BRIDGE_NAME}" -v t="${TAP_PREFIX}" '$2 ~ "^" b || $2 ~ "^" t {print $2}' | cut -d@ -f1 | sort -r); do
    ip link del "${link}" 2>/dev/null || true
  done
  iptables -t nat -D POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null || true
  set -e
}

if [[ "${mode}" == "cleanup-only" ]]; then
  cleanup_only
  exit 0
fi

mkdir -p "${EVIDENCE_DIR}" "${STACK_ROOT}" "${VM_ROOT}" "${LOG_DIR}" "${CACHE_ROOT}" "${EVIDENCE_DIR}/verification"

for cmd in awk cp e2fsck ip mount python3 scp ssh ssh-keygen stat truncate umount; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "missing required command: ${cmd}" >&2; exit 2; }
done
for path in "${BASE_ROOTFS}" "${KERNEL}" "${FIRECRACKER}" "${LAB_KEY}" "${TORQUE_BIN}" "${AGENT_BIN}" "${NATS_BIN}"; do
  [[ -e "${path}" ]] || { echo "missing ${path}" >&2; exit 2; }
done

mem_available_mib="$(awk '/MemAvailable:/ {print int($2 / 1024)}' /proc/meminfo)"
reserve_mib="${RESERVE_MIB:-2048}"
required_mib="$((NODE_COUNT * VM_MEM_MIB + reserve_mib))"
if (( mem_available_mib < required_mib )); then
  echo "insufficient memory: available=${mem_available_mib}MiB required=${required_mib}MiB" >&2
  exit 2
fi
cat >"${EVIDENCE_DIR}/capacity.json" <<EOF
{"apiVersion":"torque.dev/e2e/v1","kind":"FirecrackerPostgresCapacity","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"memAvailableMiB":${mem_available_mib},"reserveMiB":${reserve_mib},"vmMemMiB":${VM_MEM_MIB},"bridge":"${BRIDGE_NAME}","cidr":"${CIDR}","transport":"nats"}
EOF

cleanup_mounts() {
  local mnt="$1"
  set +e
  mountpoint -q "${mnt}/proc" && umount "${mnt}/proc"
  mountpoint -q "${mnt}/sys" && umount "${mnt}/sys"
  mountpoint -q "${mnt}/dev" && umount "${mnt}/dev"
  mountpoint -q "${mnt}/run" && umount "${mnt}/run"
  mountpoint -q "${mnt}" && umount "${mnt}"
  set -e
}

cache_key="$(
  {
    sha256sum "${BASE_ROOTFS}" "${KERNEL}" "${AGENT_BIN}"
    printf 'packages=openssh-server,postgresql,postgresql-client,pgbouncer,ca-certificates\n'
  } | sha256sum | awk '{print substr($1,1,16)}'
)"
prepared="${CACHE_ROOT}/prepared-${cache_key}.ext4"

prepare_base_image() {
  local tmp="${prepared}.tmp"
  local mnt="${CACHE_ROOT}/mnt-${cache_key}"
  rm -f "${tmp}"
  cp --reflink=auto "${BASE_ROOTFS}" "${tmp}" 2>/dev/null || cp "${BASE_ROOTFS}" "${tmp}"
  set +e
  e2fsck -fy "${tmp}" >"${LOG_DIR}/base-e2fsck.log" 2>&1
  local e=$?
  set -e
  [[ "${e}" -le 1 ]] || { cat "${LOG_DIR}/base-e2fsck.log" >&2; exit "${e}"; }
  truncate -s 5G "${tmp}"
  resize2fs "${tmp}" >"${LOG_DIR}/base-resize.log" 2>&1
  mkdir -p "${mnt}"
  mount -o loop "${tmp}" "${mnt}"
  trap 'cleanup_mounts "${mnt}"' RETURN
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  mount -t proc proc "${mnt}/proc"
  mount -t sysfs sysfs "${mnt}/sys"
  mount --bind /dev "${mnt}/dev"
  mount --bind /run "${mnt}/run"
  printf '#!/bin/sh\nexit 101\n' >"${mnt}/usr/sbin/policy-rc.d"
  chmod +x "${mnt}/usr/sbin/policy-rc.d"
  chroot "${mnt}" apt-get update >"${LOG_DIR}/apt-update.log" 2>&1
  chroot "${mnt}" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    openssh-server postgresql postgresql-client pgbouncer ca-certificates >"${LOG_DIR}/apt-install.log" 2>&1
  rm -f "${mnt}/usr/sbin/policy-rc.d"
  chroot "${mnt}" ssh-keygen -A >/dev/null 2>&1 || true
  install -m 0755 "${AGENT_BIN}" "${mnt}/usr/local/bin/torque-agent"
  cleanup_mounts "${mnt}"
  trap - RETURN
  mv "${tmp}" "${prepared}"
}

if [[ ! -s "${prepared}" ]]; then
  echo "preparing-postgres-base-image"
  prepare_base_image
else
  echo "using-cached-postgres-base-image"
fi

cleanup_only
mkdir -p "${VM_ROOT}"
ip link add name "${BRIDGE_NAME}" type bridge
ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}"
ip link set "${BRIDGE_NAME}" up
sysctl -w net.ipv4.ip_forward=1 >/dev/null
iptables -t nat -A POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null || true

configure_vm() {
  local i="$1"
  local name ip vm tap mac mnt
  name="$(node_name "${i}")"
  ip="$(node_ip "${i}")"
  vm="${VM_ROOT}/${name}"
  tap="${TAP_PREFIX}${i}"
  mac="$(printf '06:00:00:%02x:02:%02x' "${SUBNET_OCTET}" "$((10 + i))")"
  mkdir -p "${vm}"
  cp --reflink=auto "${prepared}" "${vm}/rootfs.ext4" 2>/dev/null || cp "${prepared}" "${vm}/rootfs.ext4"
  set +e
  e2fsck -fy "${vm}/rootfs.ext4" >"${vm}/e2fsck.log" 2>&1
  local e=$?
  set -e
  [[ "${e}" -le 1 ]] || { cat "${vm}/e2fsck.log" >&2; exit "${e}"; }
  mnt="${vm}/mnt"
  mkdir -p "${mnt}"
  mount -o loop "${vm}/rootfs.ext4" "${mnt}"
  printf '%s\n' "${name}" >"${mnt}/etc/hostname"
  cat >"${mnt}/etc/hosts" <<EOF
127.0.0.1 localhost
127.0.1.1 ${name}
${ip} ${name}
${NET_PREFIX}.10 postgres-00
${NET_PREFIX}.11 postgres-01
${NET_PREFIX}.12 postgres-02
${NET_PREFIX}.13 postgres-03
${NET_PREFIX}.14 postgres-04
EOF
  cat >"${mnt}/etc/network/interfaces" <<EOF
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet static
    address ${ip}
    netmask 255.255.255.0
    gateway ${GATEWAY}
EOF
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  rm -f "${mnt}/etc/machine-id" "${mnt}/var/lib/dbus/machine-id" 2>/dev/null || true
  touch "${mnt}/etc/machine-id"
  mkdir -p "${mnt}/root/.ssh" "${mnt}/run/sshd" "${mnt}/etc/systemd/system/multi-user.target.wants"
  ssh-keygen -y -f "${LAB_KEY}" >"${mnt}/root/.ssh/authorized_keys"
  chmod 0700 "${mnt}/root/.ssh"
  chmod 0600 "${mnt}/root/.ssh/authorized_keys"
  chmod 0755 "${mnt}/run/sshd"
  rm -f "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service" "${mnt}/etc/systemd/system/multi-user.target.wants/sshd.service"
  ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
  umount "${mnt}"
  ip tuntap add dev "${tap}" mode tap
  ip link set "${tap}" master "${BRIDGE_NAME}"
  ip link set "${tap}" up
  cat >"${vm}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=0 systemd.legacy_systemd_cgroup_controller=1 systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":1,"mem_size_mib":${VM_MEM_MIB}},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${tap}","guest_mac":"${mac}"}],"logger":{"log_path":"${vm}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
}

configure_pids=()
for i in $(seq 0 "$((NODE_COUNT - 1))"); do
  configure_vm "${i}" &
  configure_pids+=("$!")
done
wait_for_pids "${configure_pids[@]}"

for i in $(seq 0 "$((NODE_COUNT - 1))"); do
  name="$(node_name "${i}")"
  vm="${VM_ROOT}/${name}"
  "${FIRECRACKER}" --api-sock "${vm}/fc.sock" --config-file "${vm}/vm.json" >"${vm}/console.log" 2>&1 &
  echo $! >"${vm}/pid"
done

SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3)
ready=0
deadline=$((SECONDS + 240))
while (( SECONDS < deadline )); do
  ready=0
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    if ssh "${SSH_OPTS[@]}" "root@$(node_ip "${i}")" true >/dev/null 2>&1; then
      ready=$((ready + 1))
    fi
  done
  if (( ready == NODE_COUNT )); then
    break
  fi
  sleep 2
done
if (( ready != NODE_COUNT )); then
  echo "only ${ready}/${NODE_COUNT} VMs reached SSH readiness" >&2
  exit 1
fi

"${NATS_BIN}" -a "${GATEWAY}" -p "${NATS_PORT}" >"${LOG_DIR}/nats-server.log" 2>&1 &
echo $! >"${RUN_ROOT}/nats.pid"
python3 - "${GATEWAY}" "${NATS_PORT}" <<'PY'
import socket
import sys
import time

host, port = sys.argv[1], int(sys.argv[2])
deadline = time.time() + 20
while time.time() < deadline:
    try:
        with socket.create_connection((host, port), timeout=0.5):
            raise SystemExit(0)
    except OSError:
        time.sleep(0.1)
raise SystemExit("NATS did not become reachable")
PY

for i in $(seq 0 "$((NODE_COUNT - 1))"); do
  ip="$(node_ip "${i}")"
  name="$(node_name "${i}")"
  subject="$(subject_for "${i}")"
  ssh "${SSH_OPTS[@]}" "root@${ip}" "nohup /usr/local/bin/torque-agent nats worker --nats-url nats://${GATEWAY}:${NATS_PORT} --subject '${subject}' --queue postgres-workers --timeout 20m --shell /bin/bash --agent-id '${name}' --worker-id '${name}-worker' --tenant lab --target-id 'vm/${name}' --capability host.command.run >/tmp/torque-nats-worker.log 2>&1 &"
done
for i in $(seq 0 "$((NODE_COUNT - 1))"); do
  ip="$(node_ip "${i}")"
  deadline=$((SECONDS + 45))
  while (( SECONDS < deadline )); do
    if ssh "${SSH_OPTS[@]}" "root@${ip}" "grep -q 'nats worker ready' /tmp/torque-nats-worker.log" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  ssh "${SSH_OPTS[@]}" "root@${ip}" "grep -q 'nats worker ready' /tmp/torque-nats-worker.log" >/dev/null 2>&1 || {
    echo "worker on ${ip} did not become ready" >&2
    exit 1
  }
done

generate_stack() {
  python3 - "${STACK_ROOT}" "${RUN_ID}" "${GATEWAY}" "${NATS_PORT}" "${NODE_COUNT}" <<'PY'
from pathlib import Path
import sys

stack_root = Path(sys.argv[1])
run_id, gateway, nats_port, node_count = sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5])
net_prefix = ".".join(gateway.split(".")[:3])
primary_ip = f"{net_prefix}.10"

def subject(i: int) -> str:
    safe = "".join(ch for ch in run_id if ch.isalnum())
    return f"torque.pg.{safe}.postgres-{i:02d}"

def node_ip(i: int) -> str:
    return f"{net_prefix}.{10 + i}"

def block(lines: list[str], text: str) -> None:
    for raw in text.strip("\n").splitlines():
        lines.append("        " + raw)

def add_node(lines: list[str], name: str, target: str, command: str, needs: list[str] | None = None, timeout: str = "20m", delete_command: str = "") -> None:
    lines.extend([
        f"  - name: {name}",
        "    kind: host.command.run",
    ])
    if needs:
        lines.append("    needs: [" + ", ".join(needs) + "]")
    lines.extend([
        "    host:",
        "      transport: nats",
        f"      target: {target}",
        f"      timeout: {timeout}",
        "      command: |",
    ])
    block(lines, command)
    if delete_command.strip():
        lines.append("      deleteCommand: |")
        block(lines, delete_command)

common_functions = r'''
pg_version() {
  pg_lsclusters --no-header | awk 'NR==1 {print $1}'
}

set_setting() {
  local file="$1" key="$2" value="$3"
  if grep -Eq "^[#[:space:]]*${key}[[:space:]]*=" "${file}"; then
    sed -ri "s|^[#[:space:]]*${key}[[:space:]]*=.*|${key} = ${value}|" "${file}"
  else
    printf '%s = %s\n' "${key}" "${value}" >>"${file}"
  fi
}

ensure_hba() {
  local hba="$1"
  if ! grep -q 'torque-postgres-pgbouncer-e2e' "${hba}"; then
    cp "${hba}" "${hba}.torque-bak"
    {
      echo '# torque-postgres-pgbouncer-e2e begin'
      echo 'local all all trust'
      echo 'host all all 127.0.0.1/32 trust'
      echo 'host all all 172.31.239.0/24 trust'
      echo 'host replication replicator 172.31.239.0/24 trust'
      echo '# torque-postgres-pgbouncer-e2e end'
      cat "${hba}.torque-bak"
    } >"${hba}"
  fi
}

configure_postgres_common() {
  local ver conf hba
  ver="$(pg_version)"
  conf="/etc/postgresql/${ver}/main/postgresql.conf"
  hba="/etc/postgresql/${ver}/main/pg_hba.conf"
  set_setting "${conf}" "listen_addresses" "'*'"
  set_setting "${conf}" "wal_level" "replica"
  set_setting "${conf}" "max_wal_senders" "16"
  set_setting "${conf}" "max_replication_slots" "16"
  set_setting "${conf}" "hot_standby" "on"
  ensure_hba "${hba}"
}

ensure_pgbouncer() {
  install -d -m 0755 -o postgres -g postgres /run/pgbouncer /var/log/pgbouncer
  cat >/etc/pgbouncer/pgbouncer.ini <<'EOF'
[databases]
torque = host=127.0.0.1 port=5432 dbname=torque

[pgbouncer]
listen_addr = 0.0.0.0
listen_port = 6432
user = postgres
auth_type = trust
auth_file = /etc/pgbouncer/userlist.txt
pool_mode = transaction
max_client_conn = 100
default_pool_size = 20
ignore_startup_parameters = extra_float_digits
pidfile = /run/pgbouncer/pgbouncer.pid
logfile = /var/log/pgbouncer/pgbouncer.log
admin_users = postgres, torque_app
EOF
  cat >/etc/pgbouncer/userlist.txt <<'EOF'
"postgres" ""
"torque_app" ""
EOF
  chown postgres:postgres /etc/pgbouncer/pgbouncer.ini /etc/pgbouncer/userlist.txt
  chmod 0640 /etc/pgbouncer/pgbouncer.ini /etc/pgbouncer/userlist.txt
  if [[ -f /etc/default/pgbouncer ]]; then
    sed -ri 's/^START=.*/START=1/' /etc/default/pgbouncer
    grep -q '^START=' /etc/default/pgbouncer || echo 'START=1' >>/etc/default/pgbouncer
  fi
  systemctl enable pgbouncer >/dev/null 2>&1 || true
  if ! systemctl restart pgbouncer; then
    systemctl status pgbouncer --no-pager || true
    journalctl -u pgbouncer -n 80 --no-pager || true
    return 1
  fi
}
'''

primary_command = common_functions + f'''
set -euo pipefail
PROOF_DIR="/var/lib/torque-postgres-pgbouncer"
mkdir -p "${{PROOF_DIR}}"

configure_postgres_common
systemctl restart postgresql
for _ in $(seq 1 90); do
  if runuser -u postgres -- psql -tAc 'select 1' >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
runuser -u postgres -- psql -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'replicator') THEN
    CREATE ROLE replicator WITH REPLICATION LOGIN;
  END IF;
END$$;
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'torque_app') THEN
    CREATE ROLE torque_app WITH LOGIN;
  END IF;
END$$;
SELECT 'CREATE DATABASE torque OWNER torque_app' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'torque')\\gexec
\\c torque
CREATE TABLE IF NOT EXISTS torque_probe (
  id text PRIMARY KEY,
  payload text NOT NULL,
  observed_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE torque_probe OWNER TO torque_app;
GRANT ALL ON TABLE torque_probe TO torque_app;
SQL
ensure_pgbouncer
pg_isready -h 127.0.0.1 -p 5432
pg_isready -h 127.0.0.1 -p 6432
cat >"${{PROOF_DIR}}/primary.json" <<EOF
{{"apiVersion":"torque.dev/postgres-pgbouncer/v1","kind":"PostgresPrimaryReceipt","status":"succeeded","node":"postgres-00","ip":"{primary_ip}","pgbouncerPort":6432}}
EOF
cat "${{PROOF_DIR}}/primary.json"
'''

primary_delete = r'''
set -euo pipefail
systemctl stop pgbouncer postgresql 2>/dev/null || true
'''

def replica_command(i: int) -> str:
    ip = node_ip(i)
    return common_functions + f'''
set -euo pipefail
PRIMARY="{primary_ip}"
PROOF_DIR="/var/lib/torque-postgres-pgbouncer"
mkdir -p "${{PROOF_DIR}}"

configure_postgres_common
ver="$(pg_version)"
data_dir="/var/lib/postgresql/${{ver}}/main"
if runuser -u postgres -- psql -tAc "select pg_is_in_recovery()" 2>/dev/null | grep -q t; then
  systemctl restart postgresql
else
  systemctl stop postgresql pgbouncer 2>/dev/null || true
  rm -rf "${{data_dir}}"
  install -d -m 0700 -o postgres -g postgres "${{data_dir}}"
  runuser -u postgres -- pg_basebackup -h "${{PRIMARY}}" -D "${{data_dir}}" -U replicator -Fp -Xs -P -R -w
  chown -R postgres:postgres "${{data_dir}}"
  systemctl start postgresql
fi
for _ in $(seq 1 120); do
  if runuser -u postgres -- psql -tAc "select pg_is_in_recovery()" 2>/dev/null | grep -q t; then
    break
  fi
  sleep 2
done
runuser -u postgres -- psql -tAc "select pg_is_in_recovery()" | grep -q t
ensure_pgbouncer
pg_isready -h 127.0.0.1 -p 5432
pg_isready -h 127.0.0.1 -p 6432
cat >"${{PROOF_DIR}}/replica-{i:02d}.json" <<EOF
{{"apiVersion":"torque.dev/postgres-pgbouncer/v1","kind":"PostgresReplicaReceipt","status":"succeeded","node":"postgres-{i:02d}","ip":"{ip}","primary":"{primary_ip}","pgbouncerPort":6432}}
EOF
cat "${{PROOF_DIR}}/replica-{i:02d}.json"
'''

replica_delete = r'''
set -euo pipefail
systemctl stop pgbouncer postgresql 2>/dev/null || true
'''

verify_command = common_functions + f'''
set -euo pipefail
PROOF_DIR="/var/lib/torque-postgres-pgbouncer"
mkdir -p "${{PROOF_DIR}}"
probe_id="probe-${{HOSTNAME}}-{run_id}"
psql_primary() {{
  PGPASSWORD= psql -h 127.0.0.1 -p 6432 -U torque_app -d torque -v ON_ERROR_STOP=1 "$@"
}}
psql_primary -c "INSERT INTO torque_probe(id, payload) VALUES ('${{probe_id}}', 'nats pgbouncer proof') ON CONFLICT (id) DO UPDATE SET payload = EXCLUDED.payload, observed_at = now();"

replication_state="0"
for _ in $(seq 1 120); do
  replication_state="$(runuser -u postgres -- psql -tAc "select count(*) from pg_stat_replication where state = 'streaming';" | tr -d '[:space:]')"
  [[ "${{replication_state}}" == "4" ]] && break
  sleep 2
done

ready_nodes=0
replicated_nodes=0
pgbouncer_nodes=0
: >"${{PROOF_DIR}}/cluster-status.txt"
for node in 0 1 2 3 4; do
  ip="{net_prefix}.$((10 + node))"
  if pg_isready -h "${{ip}}" -p 5432 >/dev/null 2>&1; then
    ready_nodes=$((ready_nodes + 1))
  fi
  if pg_isready -h "${{ip}}" -p 6432 >/dev/null 2>&1; then
    pgbouncer_nodes=$((pgbouncer_nodes + 1))
  fi
  count="$(PGPASSWORD= psql -h "${{ip}}" -p 6432 -U torque_app -d torque -tAc "select count(*) from torque_probe where id='${{probe_id}}';" 2>/dev/null | tr -d '[:space:]' || true)"
  in_recovery="$(PGPASSWORD= psql -h "${{ip}}" -p 5432 -U postgres -d postgres -tAc "select pg_is_in_recovery();" 2>/dev/null | tr -d '[:space:]' || true)"
  printf 'node=postgres-%02d ip=%s postgresReady=%s pgbouncerReady=%s replicatedCount=%s inRecovery=%s\n' "${{node}}" "${{ip}}" "$(pg_isready -h "${{ip}}" -p 5432 >/dev/null 2>&1 && echo true || echo false)" "$(pg_isready -h "${{ip}}" -p 6432 >/dev/null 2>&1 && echo true || echo false)" "${{count:-0}}" "${{in_recovery}}" >>"${{PROOF_DIR}}/cluster-status.txt"
  [[ "${{count}}" == "1" ]] && replicated_nodes=$((replicated_nodes + 1))
done
cat "${{PROOF_DIR}}/cluster-status.txt"
status="succeeded"
if [[ "${{ready_nodes}}" != "5" || "${{pgbouncer_nodes}}" != "5" || "${{replicated_nodes}}" != "5" || "${{replication_state}}" != "4" ]]; then
  status="failed"
fi
cat >"${{PROOF_DIR}}/receipt.json" <<EOF
{{"apiVersion":"torque.dev/postgres-pgbouncer/v1","kind":"PostgresPgBouncerClusterReceipt","status":"${{status}}","runId":"{run_id}","nodeCount":5,"primaryIP":"{primary_ip}","replicaCount":4,"streamingReplicas":${{replication_state:-0}},"postgresReady":${{ready_nodes}},"pgbouncerReady":${{pgbouncer_nodes}},"replicatedNodes":${{replicated_nodes}},"probeId":"${{probe_id}}","transport":"nats"}}
EOF
cat "${{PROOF_DIR}}/receipt.json"
[[ "${{status}}" == "succeeded" ]]
'''

lines = [
    "apiVersion: torque.dev/v1",
    "kind: Stack",
    "name: firecracker-postgres-pgbouncer-nats",
    "runner:",
    "  concurrency: 2",
    "cli:",
    "  inferDeps: false",
    "nodes:",
]
add_node(lines, "postgres-primary", subject(0), primary_command, timeout="20m", delete_command=primary_delete)
for i in range(1, node_count):
    add_node(lines, f"postgres-replica-{i:02d}", subject(i), replica_command(i), needs=["postgres-primary"], timeout="20m", delete_command=replica_delete)
add_node(
    lines,
    "postgres-pgbouncer-verify",
    subject(0),
    verify_command,
    needs=[f"postgres-replica-{i:02d}" for i in range(1, node_count)],
    timeout="15m",
)
stack_root.mkdir(parents=True, exist_ok=True)
stack_root.joinpath("stack.yaml").write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
}

latest_run_id() {
  "${TORQUE_BIN}" stack runs --config "${STACK_ROOT}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
}

generate_stack
cp "${STACK_ROOT}/stack.yaml" "${EVIDENCE_DIR}/stack.yaml"

ops_run_stack() {
  local label="$1"
  shift
  TORQUE_NATS_URL="nats://${GATEWAY}:${NATS_PORT}" "$@" >"${LOG_DIR}/${label}.out" 2>"${LOG_DIR}/${label}.err"
}

ops_run_stack plan "${TORQUE_BIN}" stack plan --config "${STACK_ROOT}" --output json
ops_run_stack apply "${TORQUE_BIN}" stack apply --config "${STACK_ROOT}" --yes --concurrency 2 --output json
apply_run_id="$(latest_run_id)"
printf '%s\n' "${apply_run_id}" >"${EVIDENCE_DIR}/apply-run-id.txt"
ops_run_stack reapply "${TORQUE_BIN}" stack apply --config "${STACK_ROOT}" --yes --concurrency 2 --output json
reapply_run_id="$(latest_run_id)"
printf '%s\n' "${reapply_run_id}" >"${EVIDENCE_DIR}/reapply-run-id.txt"
mkdir -p "${EVIDENCE_DIR}/stack"
ops_run_stack audit "${TORQUE_BIN}" stack audit --config "${STACK_ROOT}" --run-id "${reapply_run_id}" --output json --include-artifacts
cp "${LOG_DIR}/audit.out" "${EVIDENCE_DIR}/stack/audit.json"
ops_run_stack export "${TORQUE_BIN}" stack export --config "${STACK_ROOT}" --run-id "${reapply_run_id}" --out "${EVIDENCE_DIR}/stack/stack-export.tgz"

ssh "${SSH_OPTS[@]}" "root@${NET_PREFIX}.10" "cat /var/lib/torque-postgres-pgbouncer/receipt.json" >"${EVIDENCE_DIR}/postgres-receipt.json"
ssh "${SSH_OPTS[@]}" "root@${NET_PREFIX}.10" "cat /var/lib/torque-postgres-pgbouncer/cluster-status.txt" >"${EVIDENCE_DIR}/cluster-status.txt"

python3 - "${EVIDENCE_DIR}" "${RUN_ID}" "${apply_run_id}" "${reapply_run_id}" <<'PY'
import json
import sys
import time
from pathlib import Path

root = Path(sys.argv[1])
run_id, apply_run_id, reapply_run_id = sys.argv[2:5]
receipt = json.loads((root / "postgres-receipt.json").read_text(encoding="utf-8"))
audit = json.loads((root / "stack" / "audit.json").read_text(encoding="utf-8"))
errors = []
if receipt.get("status") != "succeeded":
    errors.append("postgres cluster receipt failed")
if receipt.get("nodeCount") != 5:
    errors.append("node count mismatch")
if receipt.get("replicaCount") != 4:
    errors.append("replica count mismatch")
if receipt.get("streamingReplicas") != 4:
    errors.append("streaming replica count mismatch")
if receipt.get("postgresReady") != 5:
    errors.append("postgres readiness mismatch")
if receipt.get("pgbouncerReady") != 5:
    errors.append("pgbouncer readiness mismatch")
if receipt.get("replicatedNodes") != 5:
    errors.append("replicated node count mismatch")
if audit.get("status") != "succeeded":
    errors.append(f"stack audit status is {audit.get('status')}")
names = {artifact.get("name") for artifact in audit.get("artifacts", [])}
if "host-command-execute.json" not in names:
    errors.append("missing host-command NATS execution artifacts")
if not (root / "stack" / "stack-export.tgz").is_file():
    errors.append("missing stack export bundle")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackFirecrackerPostgres001Receipt",
    "taskId": "STACK-FC-POSTGRES-001",
    "runId": run_id,
    "status": "succeeded" if not errors else "failed",
    "applyRunId": apply_run_id,
    "reapplyRunId": reapply_run_id,
    "nodeCount": receipt.get("nodeCount"),
    "replicaCount": receipt.get("replicaCount"),
    "streamingReplicas": receipt.get("streamingReplicas"),
    "postgresReady": receipt.get("postgresReady"),
    "pgbouncerReady": receipt.get("pgbouncerReady"),
    "replicatedNodes": receipt.get("replicatedNodes"),
    "transport": receipt.get("transport"),
    "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "errors": errors,
}
out = root / "verification" / "receipt.json"
out.parent.mkdir(parents=True, exist_ok=True)
out.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY

if [[ "${CLEANUP_ENABLED}" == "1" ]]; then
  ops_run_stack delete "${TORQUE_BIN}" stack delete --config "${STACK_ROOT}" --yes --concurrency 2 --output json
fi

cat >"${EVIDENCE_DIR}/result.json" <<EOF
{"apiVersion":"torque.dev/e2e/v1","kind":"OpsLabResult","taskId":"STACK-FC-POSTGRES-001","runId":"${RUN_ID}","status":"succeeded","finishedAt":"$(date -u +"%Y-%m-%dT%H:%M:%SZ")"}
EOF

if [[ "${CLEANUP_ENABLED}" == "1" ]]; then
  cleanup_only
fi
REMOTE

ops_log "run PostgreSQL/PgBouncer NATS stack on ${TORQUE_LAB_SSH}"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
  "RUN_ROOT='${remote_root}' RUN_ID='${OPS_RUN_ID}' VM_MEM_MIB='${vm_mem_mib}' CLEANUP_ENABLED='${cleanup_enabled}' '${remote_script}' run" \
  >"${OPS_RUN_DIR}/remote/runner.out" 2>"${OPS_RUN_DIR}/remote/runner.err"
remote_complete=1
copy_remote_evidence || ops_fail "remote evidence copy failed"

ops_log "STACK-FC-POSTGRES-001 passed"
