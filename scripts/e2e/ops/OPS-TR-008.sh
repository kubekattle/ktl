#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-TR-008.sh [options]

Options:
  --counts LIST             Comma-separated VM counts. Default: 1,10,100.
  --vm-mem MIB              Firecracker memory per VM. Default: 192.
  --destroy-existing-labs   Remove existing Torque Firecracker labs first.
  --evidence-root DIR       Evidence root. Defaults to a temp directory.
  --cleanup                 Clean benchmark VMs and remote scratch. Default.
  --no-cleanup              Leave benchmark lab running for debugging.
  -h, --help                Show this help.

OPS-TR-008 benchmarks the same typed host.file.ensure module over SSH and NATS
inside Firecracker VMs on the real lab host. It records total duration, node
p50/p95, transport operation count, and proof bundle size for changed and noop
runs at each requested target count.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

counts="1,10,100"
vm_mem_mib=192
destroy_existing_labs=0
cleanup_enabled=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --counts)
      [[ $# -ge 2 ]] || ops_fail "--counts requires a value"
      counts="$2"
      shift 2
      ;;
    --vm-mem)
      [[ $# -ge 2 ]] || ops_fail "--vm-mem requires a value"
      vm_mem_mib="$2"
      shift 2
      ;;
    --destroy-existing-labs)
      destroy_existing_labs=1
      shift
      ;;
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Firecracker benchmark without TORQUE_OPS_E2E_CONFIRM=1"
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
ops_init_run "OPS-TR-008"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-tr-008.XXXXXX")"
remote_root="/var/lib/torque-ops-tr-008/${OPS_RUN_ID}"
remote_script="${remote_root}/run-benchmark.sh"
remote_complete=0
remote_copied=0

cleanup_lab_resources() {
  local status="succeeded"
  local remote_status="not-requested"
  if [[ -n "${remote_root}" ]]; then
    mkdir -p "${OPS_RUN_DIR}/remote"
    if scp -r "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_root}/evidence/." "${OPS_RUN_DIR}/remote/" >/dev/null 2>&1; then
      remote_copied=1
    elif [[ "${remote_complete}" == "1" ]]; then
      remote_status="copy-failed"
      status="failed"
    fi
  fi
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    ops_set_ssh_base_args
    if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "if [ -x '${remote_root}/run-benchmark.sh' ]; then '${remote_root}/run-benchmark.sh' cleanup-only || true; fi; rm -rf '${remote_root}'"; then
      remote_status="deleted"
    else
      remote_status="failed"
      status="failed"
    fi
  else
    remote_status="kept:${remote_root}"
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.vm,lab.ssh-linux" \
    remoteRoot="${remote_root}" \
    remoteEvidenceCopied="${remote_copied}" \
    remote="${remote_status}" \
    cleanupRequested="${cleanup_enabled}" \
    finishedAt="$(ops_utc_now)"
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

ops_log "build linux torque binaries"
GOOS=linux GOARCH=amd64 go build -o "${scratch_root}/bin/torque" "${repo_root}/cmd/torque" >"${OPS_RUN_DIR}/build/torque.out" 2>&1
GOOS=linux GOARCH=amd64 go build -o "${scratch_root}/bin/torque-agent" "${repo_root}/cmd/torque-agent" >"${OPS_RUN_DIR}/build/torque-agent.out" 2>&1

if ! [[ -x "${scratch_root}/bin/nats-server" ]]; then
  ops_log "build linux nats-server"
  nats_build_dir="${scratch_root}/nats-server-build"
  mkdir -p "${nats_build_dir}"
  (
    cd "${nats_build_dir}"
    go mod init torque-nats-server-build >/dev/null 2>&1
    go get github.com/nats-io/nats-server/v2@v2.10.26 >/dev/null 2>&1
    GOOS=linux GOARCH=amd64 go build -o "${scratch_root}/bin/nats-server" github.com/nats-io/nats-server/v2
  ) >"${OPS_RUN_DIR}/build/nats-server.out" 2>&1
fi

ops_log "install remote benchmark runner"
ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "mkdir -p '${remote_root}/bin' '${remote_root}/modules'"
scp "${scratch_root}/bin/torque" "${scratch_root}/bin/torque-agent" "${scratch_root}/bin/nats-server" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_root}/bin/" >/dev/null
scp "${repo_root}/testdata/modules/torque.host/modules/file_ensure.py" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_root}/modules/file_ensure.py" >/dev/null

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat > '${remote_script}' && chmod +x '${remote_script}'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

mode="${1:-run}"
if [[ "${mode}" == "cleanup-only" && -z "${RUN_ROOT:-}" ]]; then
  RUN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
RUN_ROOT="${RUN_ROOT:?missing RUN_ROOT}"
RUN_ID="${RUN_ID:-$(basename "${RUN_ROOT}")}"
COUNTS="${COUNTS:-1}"
VM_MEM_MIB="${VM_MEM_MIB:-512}"
DESTROY_EXISTING_LABS="${DESTROY_EXISTING_LABS:-0}"
CLEANUP_ENABLED="${CLEANUP_ENABLED:-1}"

BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/firecracker-sandbox-lab/vmlinux.bin}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
TORQUE_BIN="${RUN_ROOT}/bin/torque"
AGENT_BIN="${RUN_ROOT}/bin/torque-agent"
NATS_BIN="${RUN_ROOT}/bin/nats-server"
MODULE_PATH="${RUN_ROOT}/modules/file_ensure.py"
EVIDENCE_DIR="${RUN_ROOT}/evidence"
STACK_ROOT="${RUN_ROOT}/stacks"
VM_ROOT="${RUN_ROOT}/vms"
LOG_DIR="${EVIDENCE_DIR}/logs"
NATS_PORT="${NATS_PORT:-4229}"
SUBNET_OCTET="${SUBNET_OCTET:-238}"
NET_PREFIX="172.30.${SUBNET_OCTET}"
GATEWAY="${NET_PREFIX}.1"
CIDR="${NET_PREFIX}.0/24"
BRIDGE_NAME="tqb${RUN_ID//[^0-9]/}"
BRIDGE_NAME="${BRIDGE_NAME:0:12}"
TAP_PREFIX="tqt${RUN_ID//[^0-9]/}"
TAP_PREFIX="${TAP_PREFIX:0:8}"

node_ip() {
  local i="$1"
  printf '%s.%d' "${NET_PREFIX}" "$((10 + i))"
}

node_name() {
  local i="$1"
  printf 'bench-%03d' "${i}"
}

subject_for() {
  local i="$1"
  printf 'torque.bench.%s.vm%d' "${RUN_ID//[^A-Za-z0-9]/}" "${i}"
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

destroy_existing_labs() {
  set +e
  for pid in $(pgrep -f '^/usr/local/bin/firecracker --api-sock /var/lib/torque' 2>/dev/null); do
    kill "${pid}" 2>/dev/null
  done
  sleep 1
  for pid in $(pgrep -f '^/usr/local/bin/firecracker --api-sock /var/lib/torque' 2>/dev/null); do
    kill -9 "${pid}" 2>/dev/null
  done
  for link in $(ip -o link show | awk -F': ' '/^(.*: )?(tq|fc|tap|vmtap|br-torque|torque)/ {print $2}' | cut -d@ -f1 | sort -r); do
    ip link del "${link}" 2>/dev/null || true
  done
  rm -rf /var/lib/torque-firecracker-gitlab /var/lib/torque-firecracker-k8s /var/lib/torque-firecracker-mysql /var/lib/torque-stack-life-012 /var/lib/torque-ops-host-001
  set -e
}

if [[ "${mode}" == "cleanup-only" ]]; then
  cleanup_only
  exit 0
fi

mkdir -p "${EVIDENCE_DIR}" "${STACK_ROOT}" "${VM_ROOT}" "${LOG_DIR}"

if [[ "${DESTROY_EXISTING_LABS}" == "1" ]]; then
  destroy_existing_labs
fi

for cmd in awk cp e2fsck ip mount python3 scp ssh ssh-keygen stat umount; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "missing required command: ${cmd}" >&2; exit 2; }
done
for path in "${BASE_ROOTFS}" "${KERNEL}" "${FIRECRACKER}" "${LAB_KEY}" "${TORQUE_BIN}" "${AGENT_BIN}" "${NATS_BIN}" "${MODULE_PATH}"; do
  [[ -e "${path}" ]] || { echo "missing ${path}" >&2; exit 2; }
done

IFS=',' read -r -a raw_counts <<<"${COUNTS}"
counts=()
max_requested=0
for raw in "${raw_counts[@]}"; do
  raw="${raw//[[:space:]]/}"
  [[ "${raw}" =~ ^[0-9]+$ ]] || { echo "invalid count ${raw}" >&2; exit 2; }
  [[ "${raw}" -gt 0 ]] || { echo "count must be > 0" >&2; exit 2; }
  counts+=("${raw}")
  (( raw > max_requested )) && max_requested="${raw}"
done

mem_available_mib="$(awk '/MemAvailable:/ {print int($2 / 1024)}' /proc/meminfo)"
reserve_mib="${RESERVE_MIB:-2048}"
if (( mem_available_mib <= reserve_mib )); then
  max_by_mem=0
else
  max_by_mem="$(((mem_available_mib - reserve_mib) / VM_MEM_MIB))"
fi
max_to_boot="${max_requested}"
if (( max_by_mem < max_to_boot )); then
  max_to_boot="${max_by_mem}"
fi
if (( max_to_boot < 1 )); then
  echo "insufficient memory for one VM: available=${mem_available_mib}MiB reserve=${reserve_mib}MiB vm=${VM_MEM_MIB}MiB" >&2
  exit 2
fi

cat >"${EVIDENCE_DIR}/capacity.json" <<EOF
{"apiVersion":"torque.dev/e2e/v1","kind":"FirecrackerBenchmarkCapacity","runId":"${RUN_ID}","requestedCounts":"${COUNTS}","maxRequested":${max_requested},"maxToBoot":${max_to_boot},"memAvailableMiB":${mem_available_mib},"reserveMiB":${reserve_mib},"vmMemMiB":${VM_MEM_MIB},"bridge":"${BRIDGE_NAME}","cidr":"${CIDR}"}
EOF

cleanup_only
mkdir -p "${VM_ROOT}"
ip link add name "${BRIDGE_NAME}" type bridge
ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}"
ip link set "${BRIDGE_NAME}" up
iptables -t nat -A POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null || true

configure_vm() {
  local i="$1"
  local name ip vm tap mac mnt
  name="$(node_name "${i}")"
  ip="$(node_ip "${i}")"
  vm="${VM_ROOT}/${name}"
  tap="${TAP_PREFIX}${i}"
  mac="$(printf '02:FC:%02X:%02X:%02X:%02X' "${SUBNET_OCTET}" "$((10 + i))" "$((i / 256))" "$((i % 256))")"
  mkdir -p "${vm}"
  cp --reflink=auto "${BASE_ROOTFS}" "${vm}/rootfs.ext4" 2>/dev/null || cp "${BASE_ROOTFS}" "${vm}/rootfs.ext4"
  set +e
  e2fsck -fy "${vm}/rootfs.ext4" >"${vm}/e2fsck.log" 2>&1
  local fsck_status="$?"
  set -e
  [[ "${fsck_status}" -le 1 ]] || { cat "${vm}/e2fsck.log" >&2; exit "${fsck_status}"; }
  mnt="${vm}/mnt"
  mkdir -p "${mnt}"
  mount -o loop "${vm}/rootfs.ext4" "${mnt}"
  printf '%s\n' "${name}" >"${mnt}/etc/hostname"
  cat >"${mnt}/etc/hosts" <<EOF
127.0.0.1 localhost
127.0.1.1 ${name}
${ip} ${name}
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
  rm -f "${mnt}/etc/machine-id" "${mnt}/var/lib/dbus/machine-id" 2>/dev/null || true
  touch "${mnt}/etc/machine-id"
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  mkdir -p "${mnt}/root/.ssh" "${mnt}/run/sshd" "${mnt}/etc/systemd/system/multi-user.target.wants"
  ssh-keygen -y -f "${LAB_KEY}" >"${mnt}/root/.ssh/authorized_keys"
  chmod 0700 "${mnt}/root/.ssh"
  chmod 0600 "${mnt}/root/.ssh/authorized_keys"
  chmod 0755 "${mnt}/run/sshd"
  rm -f "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service" "${mnt}/etc/systemd/system/multi-user.target.wants/sshd.service"
  cat >"${mnt}/etc/systemd/system/torque-ssh.service" <<'EOF'
[Unit]
Description=Torque benchmark SSH server
After=network.target
Wants=network.target

[Service]
Type=simple
RuntimeDirectory=sshd
ExecStart=/usr/sbin/sshd -D -e
TimeoutStartSec=300
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
EOF
  ln -sf ../torque-ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/torque-ssh.service"
  umount "${mnt}"

  ip tuntap add dev "${tap}" mode tap
  ip link set "${tap}" master "${BRIDGE_NAME}"
  ip link set "${tap}" up
  cat >"${vm}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=0 systemd.legacy_systemd_cgroup_controller=1 systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":1,"mem_size_mib":${VM_MEM_MIB}},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${tap}","guest_mac":"${mac}"}],"logger":{"log_path":"${vm}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
}

SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3)

configure_pids=()
for i in $(seq 0 "$((max_to_boot - 1))"); do
  configure_vm "${i}" &
  configure_pids+=("$!")
  if (( (i + 1) % 8 == 0 )); then
    wait_for_pids "${configure_pids[@]}"
    configure_pids=()
  fi
done
if (( ${#configure_pids[@]} > 0 )); then
  wait_for_pids "${configure_pids[@]}"
fi

for i in $(seq 0 "$((max_to_boot - 1))"); do
  name="$(node_name "${i}")"
  vm="${VM_ROOT}/${name}"
  "${FIRECRACKER}" --api-sock "${vm}/fc.sock" --config-file "${vm}/vm.json" >"${vm}/console.log" 2>&1 &
  echo $! >"${vm}/pid"
done

ready=0
deadline=$((SECONDS + 180))
while (( SECONDS < deadline )); do
  ready=0
  for i in $(seq 0 "$((max_to_boot - 1))"); do
    ip="$(node_ip "${i}")"
    if ssh "${SSH_OPTS[@]}" "root@${ip}" true >/dev/null 2>&1; then
      ready=$((ready + 1))
    fi
  done
  if (( ready == max_to_boot )); then
    break
  fi
  sleep 2
done
if (( ready != max_to_boot )); then
  echo "only ${ready}/${max_to_boot} VMs reached SSH readiness" >&2
  exit 1
fi

chmod_pids=()
for i in $(seq 0 "$((max_to_boot - 1))"); do
  ip="$(node_ip "${i}")"
  scp "${SSH_OPTS[@]}" "${AGENT_BIN}" "root@${ip}:/usr/local/bin/torque-agent" >/dev/null
  ssh "${SSH_OPTS[@]}" "root@${ip}" "chmod 0755 /usr/local/bin/torque-agent" &
  chmod_pids+=("$!")
  if (( (i + 1) % 16 == 0 )); then
    wait_for_pids "${chmod_pids[@]}"
    chmod_pids=()
  fi
done
if (( ${#chmod_pids[@]} > 0 )); then
  wait_for_pids "${chmod_pids[@]}"
fi

"${NATS_BIN}" -a "${GATEWAY}" -p "${NATS_PORT}" >"${LOG_DIR}/nats-server.log" 2>&1 &
echo $! >"${RUN_ROOT}/nats.pid"
python3 - "${GATEWAY}" "${NATS_PORT}" <<'PY'
import socket, sys, time
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

for i in $(seq 0 "$((max_to_boot - 1))"); do
  ip="$(node_ip "${i}")"
  subject="$(subject_for "${i}")"
  ssh "${SSH_OPTS[@]}" "root@${ip}" "nohup /usr/local/bin/torque-agent nats worker --nats-url nats://${GATEWAY}:${NATS_PORT} --subject '${subject}' --queue host-file-bench --timeout 30s >/tmp/torque-nats-worker.log 2>&1 &"
done
for i in $(seq 0 "$((max_to_boot - 1))"); do
  ip="$(node_ip "${i}")"
  deadline=$((SECONDS + 30))
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
  local transport="$1"
  local count="$2"
  local stack_dir="$3"
  local concurrency="$4"
  python3 - "${transport}" "${count}" "${stack_dir}" "${RUN_ID}" "${MODULE_PATH}" "${LAB_KEY}" "${GATEWAY}" "${NATS_PORT}" "${concurrency}" <<'PY'
from pathlib import Path
import sys

transport, count, stack_dir, run_id, module_path, lab_key, gateway, nats_port, concurrency = sys.argv[1:10]
count = int(count)
root = Path(stack_dir)
root.mkdir(parents=True, exist_ok=True)
lines = [
    "apiVersion: torque.dev/v1",
    "kind: Stack",
    f"name: host-file-{transport}-{count}",
    "runner:",
    f"  concurrency: {concurrency}",
    "cli:",
    "  inferDeps: false",
    "nodes:",
]
for i in range(count):
    ip = f"172.30.{gateway.split('.')[2]}.{10 + i}"
    subject = f"torque.bench.{''.join(ch for ch in run_id if ch.isalnum())}.vm{i}"
    lines.extend([
        f"  - name: file-{i:03d}",
        "    kind: host.file.ensure",
        "    module:",
        "      source: oci://example.test/torque-modules/host",
        "      version: 0.1.0",
        f"      command: [\"python3\", \"{module_path}\"]",
        "      timeout: 2m",
        "      input:",
        f"        transport: {transport}",
        "        timeoutSeconds: 30",
        f"        path: /tmp/torque-host-file-bench-{run_id}-{transport}-{i:03d}.txt",
        "        content: |",
        f"          torque host.file.ensure benchmark {transport} {run_id} {i:03d}",
        "        mode: \"0644\"",
    ])
    if transport == "ssh":
        lines.extend([
            f"        target: root@{ip}",
            f"        identityFile: {lab_key}",
            "        sshOptions:",
            "          - -o",
            "          - BatchMode=yes",
            "          - -o",
            "          - StrictHostKeyChecking=no",
            "          - -o",
            "          - UserKnownHostsFile=/dev/null",
            "          - -o",
            "          - ConnectTimeout=5",
        ])
    else:
        lines.extend([
            f"        target: {subject}",
            f"        natsUrl: nats://{gateway}:{nats_port}",
        ])
root.joinpath("stack.yaml").write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
}

clear_files() {
  local count="$1"
  local pids=()
  for i in $(seq 0 "$((count - 1))"); do
    ip="$(node_ip "${i}")"
    ssh "${SSH_OPTS[@]}" "root@${ip}" "rm -f /tmp/torque-host-file-bench-${RUN_ID}-ssh-${i}.txt /tmp/torque-host-file-bench-${RUN_ID}-nats-${i}.txt /tmp/torque-host-file-bench-${RUN_ID}-ssh-$(printf '%03d' "${i}").txt /tmp/torque-host-file-bench-${RUN_ID}-nats-$(printf '%03d' "${i}").txt" &
    pids+=("$!")
    if (( (i + 1) % 32 == 0 )); then
      wait_for_pids "${pids[@]}"
      pids=()
    fi
  done
  if (( ${#pids[@]} > 0 )); then
    wait_for_pids "${pids[@]}"
  fi
}

latest_run_id() {
  local stack_dir="$1"
  "${TORQUE_BIN}" stack runs --config "${stack_dir}" --limit 1 | awk 'NR==2 {print $1}'
}

write_metrics() {
  local transport="$1"
  local count="$2"
  local mode="$3"
  local total_ms="$4"
  local stack_dir="$5"
  local run_id="$6"
  local audit="$7"
  local bundle="$8"
  local output="$9"
  python3 - "${transport}" "${count}" "${mode}" "${total_ms}" "${run_id}" "${audit}" "${bundle}" "${output}" <<'PY'
import json
import re
import statistics
import sys
import datetime as dt
from pathlib import Path

transport, count, mode, total_ms, run_id, audit_path, bundle_path, output = sys.argv[1:9]
count = int(count)
total_ms = int(total_ms)
d = json.load(open(audit_path, encoding="utf-8"))

def parse_ts(value):
    match = re.match(r"^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})?$", value)
    if not match:
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    base, fraction, zone = match.groups()
    zone = "+00:00" if zone in (None, "Z") else zone
    if fraction:
        return dt.datetime.fromisoformat(f"{base}.{fraction[:6].ljust(6, '0')}{zone}")
    return dt.datetime.fromisoformat(base + zone)

running = {}
durations = []
for event in d.get("events", []):
    node = event.get("nodeId")
    if not node:
        continue
    if event.get("type") == "NODE_RUNNING":
        running[node] = parse_ts(event["ts"])
    elif event.get("type") in ("NODE_SUCCEEDED", "NODE_FAILED") and node in running:
        durations.append((parse_ts(event["ts"]) - running[node]).total_seconds() * 1000)

def percentile(values, pct):
    if not values:
        return 0
    values = sorted(values)
    idx = int(round((pct / 100) * (len(values) - 1)))
    return int(values[idx])

operation_count = 0
changed = 0
noop = 0
for artifact in d.get("artifacts", []):
    if artifact.get("name") != "module-resource.json":
        continue
    body = artifact.get("body")
    if not body:
        continue
    receipt = json.loads(body)
    for phase in receipt.get("phases", []):
        evidence = phase.get("evidence") or {}
        if "operation" in evidence:
            operation_count += 1
        for key in ("observe", "apply", "delete", "verify"):
            if key in evidence:
                operation_count += 1
        if phase.get("phase") == "apply":
            if phase.get("status") == "noop":
                noop += 1
            if phase.get("changed"):
                changed += 1

bundle = Path(bundle_path)
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "HostFileTransportBenchmarkResult",
    "transport": transport,
    "targetCount": count,
    "mode": mode,
    "runId": run_id,
    "status": d.get("summary", {}).get("status", d.get("status")),
    "totalMillis": total_ms,
    "nodeDurationP50Millis": percentile(durations, 50),
    "nodeDurationP95Millis": percentile(durations, 95),
    "nodeDurationMaxMillis": int(max(durations) if durations else 0),
    "nodeDurationMeanMillis": int(statistics.mean(durations) if durations else 0),
    "transportOperationCount": operation_count,
    "changedNodes": changed,
    "noopNodes": noop,
    "proofBundleBytes": bundle.stat().st_size if bundle.exists() else 0,
    "artifactCount": len(d.get("artifacts", [])),
    "eventCount": len(d.get("events", [])),
}
Path(output).parent.mkdir(parents=True, exist_ok=True)
Path(output).write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY
}

run_one() {
  local transport="$1"
  local count="$2"
  local mode="$3"
  local stack_dir="${STACK_ROOT}/${transport}-${count}"
  local concurrency=32
  (( count < concurrency )) && concurrency="${count}"
  generate_stack "${transport}" "${count}" "${stack_dir}" "${concurrency}"
  if [[ "${mode}" == "changed" ]]; then
    clear_files "${count}"
  fi
  local log="${LOG_DIR}/${transport}-${count}-${mode}.log"
  local err="${LOG_DIR}/${transport}-${count}-${mode}.err"
  local start_ns end_ns total_ms run_id audit bundle metrics
  start_ns="$(date +%s%N)"
  "${TORQUE_BIN}" stack apply --config "${stack_dir}" --yes >"${log}" 2>"${err}"
  end_ns="$(date +%s%N)"
  total_ms="$(((end_ns - start_ns) / 1000000))"
  run_id="$(latest_run_id "${stack_dir}")"
  audit="${EVIDENCE_DIR}/audits/${transport}-${count}-${mode}.json"
  bundle="${EVIDENCE_DIR}/bundles/${transport}-${count}-${mode}.tgz"
  metrics="${EVIDENCE_DIR}/metrics/${transport}-${count}-${mode}.json"
  mkdir -p "$(dirname "${audit}")" "$(dirname "${bundle}")" "$(dirname "${metrics}")"
  "${TORQUE_BIN}" stack audit --config "${stack_dir}" --run-id "${run_id}" --output json --events -1 --include-artifacts >"${audit}"
  "${TORQUE_BIN}" stack export --config "${stack_dir}" --run-id "${run_id}" --out "${bundle}" >/dev/null
  write_metrics "${transport}" "${count}" "${mode}" "${total_ms}" "${stack_dir}" "${run_id}" "${audit}" "${bundle}" "${metrics}"
}

for count in "${counts[@]}"; do
  if (( count > max_to_boot )); then
    mkdir -p "${EVIDENCE_DIR}/metrics"
    cat >"${EVIDENCE_DIR}/metrics/skipped-${count}.json" <<EOF
{"apiVersion":"torque.dev/e2e/v1","kind":"HostFileTransportBenchmarkSkip","targetCount":${count},"status":"skipped","reason":"capacity","maxToBoot":${max_to_boot}}
EOF
    continue
  fi
  for transport in ssh nats; do
    run_one "${transport}" "${count}" changed
    run_one "${transport}" "${count}" noop
  done
done

python3 - "${EVIDENCE_DIR}" "${RUN_ID}" "${COUNTS}" "${max_to_boot}" <<'PY'
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
run_id, counts, max_to_boot = sys.argv[2], sys.argv[3], int(sys.argv[4])
items = []
for path in sorted((root / "metrics").glob("*.json")):
    items.append(json.loads(path.read_text(encoding="utf-8")))
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "HostFileTransportBenchmarkSummary",
    "runId": run_id,
    "requestedCounts": counts,
    "maxToBoot": max_to_boot,
    "results": items,
}
(root / "summary.json").write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

if [[ "${CLEANUP_ENABLED}" == "1" ]]; then
  cleanup_only
fi
REMOTE

ops_log "run Firecracker SSH/NATS benchmark on ${TORQUE_LAB_SSH}"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
  "RUN_ROOT='${remote_root}' RUN_ID='${OPS_RUN_ID}' COUNTS='${counts}' VM_MEM_MIB='${vm_mem_mib}' DESTROY_EXISTING_LABS='${destroy_existing_labs}' CLEANUP_ENABLED='${cleanup_enabled}' '${remote_script}' run" \
  >"${OPS_RUN_DIR}/remote/benchmark.out" 2>"${OPS_RUN_DIR}/remote/benchmark.err"
remote_complete=1

scp -r "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_root}/evidence/." "${OPS_RUN_DIR}/remote/" >/dev/null
remote_copied=1

python3 - "${OPS_RUN_DIR}/remote/summary.json" "${OPS_RUN_DIR}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${started_at}" "$(ops_utc_now)" "${TORQUE_LAB_SSH}" "${counts}" "${vm_mem_mib}" <<'PY'
import json
import sys
from pathlib import Path

summary_path, run_dir, task_id, run_id, started_at, finished_at, lab_ssh, counts, vm_mem_mib = sys.argv[1:10]
run_dir = Path(run_dir)
summary = json.loads(Path(summary_path).read_text(encoding="utf-8"))

def write(rel, doc):
    path = run_dir / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

statuses = [item.get("status") for item in summary.get("results", []) if item.get("kind") == "HostFileTransportBenchmarkResult"]
all_succeeded = bool(statuses) and all(status == "succeeded" for status in statuses)
transport_counts = sorted({f"{item.get('transport')}:{item.get('targetCount')}:{item.get('mode')}" for item in summary.get("results", []) if item.get("kind") == "HostFileTransportBenchmarkResult"})
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTransportBenchmarkReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if all_succeeded else "failed",
    "startedAt": started_at,
    "finishedAt": finished_at,
    "summary": summary,
}
write("result.json", doc)
write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["lab.vm", "lab.ssh-linux", "lab.nats"],
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "targets": [{
        "id": "firecracker-transport-benchmark",
        "uri": lab_ssh,
        "role": "firecracker-host",
        "requestedCounts": counts,
        "maxToBoot": summary.get("maxToBoot"),
        "vmMemMiB": int(vm_mem_mib),
        "transports": ["ssh", "nats"],
    }],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "decision": "run-benchmark",
    "status": "succeeded" if all_succeeded else "failed",
    "reason": "host.file.ensure transport benchmark completed",
    "checks": transport_counts,
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "status": "succeeded" if all_succeeded else "failed",
    "finishedAt": finished_at,
    "checks": [{
        "name": "ssh-and-nats-host-file-benchmark",
        "status": "succeeded" if all_succeeded else "failed",
        "resultCount": len(statuses),
    }],
})
if not all_succeeded:
    raise SystemExit(1)
PY
