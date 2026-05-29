#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-FC-JENKINS-PG-BACKUP-001.sh [options]

Options:
  --evidence-root DIR   Evidence root. Defaults to a temp directory.
  --env-file PATH       Shell env file staged into Jenkins and sourced by the
                        backup job. Defaults to the checked-in example file.
  --destroy-existing    Remove stale direct-VM PostgreSQL and Jenkins labs first.
  --cleanup             Delete the Jenkins VM, the base PostgreSQL lab, and the
                        temporary host SSH authorization after the run. Default.
  --no-cleanup          Leave the Jenkins VM and PostgreSQL lab running.
  -h, --help            Show this help.

STACK-FC-JENKINS-PG-BACKUP-001 boots a Jenkins controller inside a Firecracker
VM on the real `141` host, seeds it with a Torque workspace, and runs a Jenkins
job that first ensures the direct Firecracker PostgreSQL VM cluster exists and
then executes the backup-only stack
`testdata/stack/e2e/33-firecracker-jenkins-postgres-backup`. The job keeps the
typed backup evidence inside the Jenkins workspace, and this harness pulls the
console log, build API receipts, and backup artifacts back into an evidence
bundle.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
  TORQUE_JENKINS_LOCAL_PORT=18081            optional; local verification port
EOF
}

cleanup_enabled=1
destroy_existing=0
job_env_file_arg=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --env-file)
      [[ $# -ge 2 ]] || ops_fail "--env-file requires a value"
      job_env_file_arg="$2"
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Jenkins Firecracker E2E without TORQUE_OPS_E2E_CONFIRM=1"
host_ssh_identity="${TORQUE_LAB_SSH_IDENTITY:-}"
host_ssh_opts="${TORQUE_LAB_SSH_OPTS:-}"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

for cmd in curl go python3 scp ssh ssh-keygen tar; do
  ops_require_cmd "${cmd}"
done

repo_root="$(ops_repo_root)"
base_stack_root="${repo_root}/testdata/stack/e2e/18-firecracker-direct-data-services"
backup_stack_root="${repo_root}/testdata/stack/e2e/33-firecracker-jenkins-postgres-backup"
default_job_env_file="${backup_stack_root}/jenkins-job.env.example"
job_env_file="${job_env_file_arg:-${TORQUE_JENKINS_JOB_ENV_FILE:-${default_job_env_file}}}"
[[ -f "${job_env_file}" ]] || ops_fail "missing Jenkins job env file: ${job_env_file}"
set -a
# shellcheck disable=SC1090
source "${job_env_file}"
set +a
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"
if [[ -n "${host_ssh_identity}" ]]; then
  export TORQUE_LAB_SSH_IDENTITY="${host_ssh_identity}"
else
  unset TORQUE_LAB_SSH_IDENTITY
fi
if [[ -n "${host_ssh_opts}" ]]; then
  export TORQUE_LAB_SSH_OPTS="${host_ssh_opts}"
else
  unset TORQUE_LAB_SSH_OPTS
fi
remote_root="/var/lib/torque-firecracker-jenkins/postgres-backup"
jenkins_ip="172.31.236.10"
jenkins_job="torque-firecracker-postgres-backup"
jenkins_local_port="${TORQUE_JENKINS_LOCAL_PORT:-18081}"
base_select=(--release postgres-verify --include-deps)

ops_init_run "STACK-FC-JENKINS-PG-BACKUP-001"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-jenkins-pg.XXXXXX")"
workspace_root="${scratch_root}/torque-demo"
workspace_bundle="${scratch_root}/torque-demo.tgz"
host_access_key="${scratch_root}/host-access"
local_tunnel_control="/tmp/torque-jenkins-ui-${OPS_RUN_ID##*-}.ctl"
remote_workspace_bundle="${remote_root}/torque-demo.tgz"
remote_access_key="${remote_root}/host-access.key"
remote_access_pubkey="${remote_root}/host-access.pub"
remote_job_env_file="${remote_root}/jenkins-job.env"
remote_jenkins_war="${remote_root}/jenkins.war"
key_comment="torque-jenkins-firecracker-${OPS_RUN_ID}"

base_stack_applied=0
jenkins_vm_applied=0
host_access_installed=0
base_delete_status="skipped"
jenkins_delete_status="skipped"
host_access_cleanup_status="skipped"

finish() {
  local code=$?
  set +e
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ssh -S "${local_tunnel_control}" -O exit "$(ops_ssh_target "${TORQUE_LAB_SSH}")" >/dev/null 2>&1 || true
  rm -f "${local_tunnel_control}"

  if [[ "${cleanup_enabled}" == "1" && "${base_stack_applied}" == "1" ]]; then
    ops_log "delete base Firecracker PostgreSQL VM stack"
    (
      cd "${repo_root}"
      ./bin/torque stack delete --config "${base_stack_root}" "${base_select[@]}" --yes --concurrency 1 --output json
    ) >"${OPS_RUN_DIR}/cleanup/base-delete.jsonl" 2>"${OPS_RUN_DIR}/cleanup/base-delete.err"
    if [[ $? -eq 0 ]]; then
      base_delete_status="succeeded"
    else
      base_delete_status="failed"
      code=1
    fi
  elif [[ "${cleanup_enabled}" == "0" && "${base_stack_applied}" == "1" ]]; then
    base_delete_status="retained"
  fi

  if [[ "${cleanup_enabled}" == "1" && "${jenkins_vm_applied}" == "1" ]]; then
    ops_log "delete Firecracker Jenkins VM"
    ops_set_ssh_base_args
    ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
      "RUN_ROOT='${remote_root}' bash -se" <<'REMOTE' \
      >"${OPS_RUN_DIR}/cleanup/jenkins-delete.out" 2>"${OPS_RUN_DIR}/cleanup/jenkins-delete.err"
set -euo pipefail
if [[ -x "${RUN_ROOT}/jenkins-vm-lab.sh" ]]; then
  RUN_ROOT="${RUN_ROOT}" "${RUN_ROOT}/jenkins-vm-lab.sh" delete
fi
REMOTE
    if [[ $? -eq 0 ]]; then
      jenkins_delete_status="succeeded"
    else
      jenkins_delete_status="failed"
      code=1
    fi
  elif [[ "${cleanup_enabled}" == "0" && "${jenkins_vm_applied}" == "1" ]]; then
    jenkins_delete_status="retained"
  fi

  if [[ "${host_access_installed}" == "1" ]]; then
    if [[ "${cleanup_enabled}" == "1" ]]; then
      ops_log "remove temporary host SSH authorization"
      ops_set_ssh_base_args
      ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
        "KEY_COMMENT='${key_comment}' RUN_ROOT='${remote_root}' bash -se" <<'REMOTE' \
        >"${OPS_RUN_DIR}/cleanup/host-key-cleanup.out" 2>"${OPS_RUN_DIR}/cleanup/host-key-cleanup.err"
set -euo pipefail
install -d -m 700 /root/.ssh
touch /root/.ssh/authorized_keys
tmp="$(mktemp)"
grep -v " ${KEY_COMMENT}\$" /root/.ssh/authorized_keys >"${tmp}" || true
cat "${tmp}" > /root/.ssh/authorized_keys
rm -f "${tmp}" "${RUN_ROOT}/host-access.key" "${RUN_ROOT}/host-access.pub"
chmod 600 /root/.ssh/authorized_keys
REMOTE
      if [[ $? -eq 0 ]]; then
        host_access_cleanup_status="succeeded"
      else
        host_access_cleanup_status="failed"
        code=1
      fi
    else
      host_access_cleanup_status="retained"
    fi
  fi

  rm -rf "${scratch_root}"
  python3 - "${OPS_RUN_DIR}/cleanup/receipt.json" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${base_delete_status}" "${jenkins_delete_status}" "${host_access_cleanup_status}" <<'PY'
import json
import sys

path, task_id, run_id, base_status, jenkins_status, host_access_status = sys.argv[1:7]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsCleanupReceipt",
    "status": "succeeded",
    "taskId": task_id,
    "runId": run_id,
    "baseDeleteStatus": base_status,
    "jenkinsDeleteStatus": jenkins_status,
    "hostAccessCleanupStatus": host_access_status,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
  ops_scan_for_secret_material "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/redaction-report.json" || code=1
  ops_write_manifest "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/manifest.json"
  ops_export_bundle "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}"
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

destroy_existing_labs() {
  ops_set_ssh_base_args
  ops_log "destroy stale Firecracker direct PostgreSQL and Jenkins labs"
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" 'bash -se' <<'REMOTE'
set -euo pipefail

cleanup_direct_lab() {
  local root="$1"
  if [[ -x "${root}/direct-vm-lab.sh" ]]; then
    RUN_ROOT="${root}" "${root}/direct-vm-lab.sh" delete >/dev/null 2>&1 || true
  else
    for pid_file in "${root}"/vms/*/pid; do
      [[ -f "${pid_file}" ]] && kill "$(cat "${pid_file}")" >/dev/null 2>&1 || true
    done
    for tap in tqds0 tqds1 tqds2 tqds3 tqds4 tqds5; do
      ip link del "${tap}" >/dev/null 2>&1 || true
    done
    ip link set tqfcds down >/dev/null 2>&1 || true
    ip link del tqfcds type bridge >/dev/null 2>&1 || true
    iptables -t nat -D POSTROUTING -s "172.31.240.0/24" ! -o tqfcds -j MASQUERADE >/dev/null 2>&1 || true
    rm -rf "${root}"
  fi
}

cleanup_jenkins_lab() {
  local root="$1"
  if [[ -x "${root}/jenkins-vm-lab.sh" ]]; then
    RUN_ROOT="${root}" "${root}/jenkins-vm-lab.sh" delete >/dev/null 2>&1 || true
  fi
  rm -rf "${root}"
}

cleanup_direct_lab /var/lib/torque-firecracker-direct/data-services
cleanup_jenkins_lab /var/lib/torque-firecracker-jenkins/postgres-backup
REMOTE
}

build_linux_workspace() {
  ops_log "build Linux torque workspace for Jenkins"
  mkdir -p "${workspace_root}/bin"
  mkdir -p "${workspace_root}/testdata/stack/e2e/18-firecracker-direct-data-services"
  mkdir -p "${workspace_root}/testdata/stack/e2e/33-firecracker-jenkins-postgres-backup"
  (
    cd "${repo_root}"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "${workspace_root}/bin/torque" ./cmd/torque
  ) >"${OPS_RUN_DIR}/build/go-build.out" 2>"${OPS_RUN_DIR}/build/go-build.err"
  cp "${base_stack_root}/stack.yaml" "${workspace_root}/testdata/stack/e2e/18-firecracker-direct-data-services/stack.yaml"
  cp -R "${backup_stack_root}/." "${workspace_root}/testdata/stack/e2e/33-firecracker-jenkins-postgres-backup/"
  COPYFILE_DISABLE=1 tar -C "${workspace_root}" -czf "${workspace_bundle}" .
}

install_host_access_key() {
  ops_log "install temporary host SSH authorization for Jenkins job"
  ssh-keygen -q -t ed25519 -N '' -C "${key_comment}" -f "${host_access_key}" >/dev/null
  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "mkdir -p '${remote_root}'" \
    >"${OPS_RUN_DIR}/remote/ensure-remote-root.out" 2>"${OPS_RUN_DIR}/remote/ensure-remote-root.err"
  scp "${OPS_SSH_ARGS[@]}" "${host_access_key}" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_access_key}" \
    >"${OPS_RUN_DIR}/remote/scp-host-key.out" 2>"${OPS_RUN_DIR}/remote/scp-host-key.err"
  scp "${OPS_SSH_ARGS[@]}" "${host_access_key}.pub" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_access_pubkey}" \
    >"${OPS_RUN_DIR}/remote/scp-host-pubkey.out" 2>"${OPS_RUN_DIR}/remote/scp-host-pubkey.err"
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "PUBKEY_PATH='${remote_access_pubkey}' bash -se" <<'REMOTE' \
    >"${OPS_RUN_DIR}/remote/install-host-key.out" 2>"${OPS_RUN_DIR}/remote/install-host-key.err"
set -euo pipefail
install -d -m 700 /root/.ssh
touch /root/.ssh/authorized_keys
pubkey="$(cat "${PUBKEY_PATH}")"
grep -qxF "${pubkey}" /root/.ssh/authorized_keys || printf '%s\n' "${pubkey}" >> /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
REMOTE
  host_access_installed=1
}

bootstrap_jenkins_vm() {
  ops_log "bootstrap Firecracker Jenkins VM"
  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "RUN_ROOT='${remote_root}' bash -se" <<'REMOTE' \
    >"${OPS_RUN_DIR}/remote/bootstrap-jenkins-vm.out" 2>"${OPS_RUN_DIR}/remote/bootstrap-jenkins-vm.err"
set -euo pipefail
mkdir -p "${RUN_ROOT}"
cat >"${RUN_ROOT}/jenkins-vm-lab.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

mode="${1:-apply}"
RUN_ROOT="${RUN_ROOT:-/var/lib/torque-firecracker-jenkins/postgres-backup}"
SUBNET_OCTET="${SUBNET_OCTET:-236}"
BRIDGE_NAME="${BRIDGE_NAME:-tqfcjk}"
TAP_NAME="${TAP_NAME:-tqjk0}"
RUN_ID="${RUN_ID:-jenkins-postgres-backup}"
BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/firecracker-sandbox-lab/vmlinux.bin}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
CACHE_ROOT="${CACHE_ROOT:-/var/lib/torque-firecracker-jenkins/cache}"
PACKAGES="openssh-server ca-certificates curl wget tar gzip openjdk-21-jre git rsync python3 postgresql-client"
NET_PREFIX="172.31.${SUBNET_OCTET}"
VM_IP="${NET_PREFIX}.10"
GATEWAY="${NET_PREFIX}.1"
CIDR="${NET_PREFIX}.0/24"
SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3)

cleanup_run() {
  local remove_root="${1:-0}"
  set +e
  if [[ -f "${RUN_ROOT}/vm/pid" ]]; then
    kill "$(cat "${RUN_ROOT}/vm/pid")" 2>/dev/null
    sleep 1
    kill -9 "$(cat "${RUN_ROOT}/vm/pid")" 2>/dev/null
  fi
  ip link del "${TAP_NAME}" 2>/dev/null
  ip link set "${BRIDGE_NAME}" down 2>/dev/null
  ip link del "${BRIDGE_NAME}" type bridge 2>/dev/null
  iptables -t nat -D POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null
  if [[ "${remove_root}" == "1" ]]; then
    rm -rf "${RUN_ROOT}"
  else
    rm -rf "${RUN_ROOT}/vm" "${RUN_ROOT}/receipt.json" "${RUN_ROOT}/nodes.txt"
    mkdir -p "${RUN_ROOT}"
  fi
  set -e
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

prepare_base_image() {
  local cache_key prepared tmp mnt
  mkdir -p "${CACHE_ROOT}"
  cache_key="$(
    {
      sha256sum "${BASE_ROOTFS}" "${KERNEL}"
      printf 'packages=%s\n' "${PACKAGES}"
      printf 'rootfs-size=8G\n'
    } | sha256sum | awk '{print substr($1,1,16)}'
  )"
  prepared="${CACHE_ROOT}/prepared-jenkins-${cache_key}.ext4"
  if [[ -s "${prepared}" ]]; then
    echo "${prepared}"
    return
  fi
  tmp="${prepared}.tmp"
  mnt="${CACHE_ROOT}/mnt-${cache_key}"
  rm -f "${tmp}"
  cp --reflink=auto "${BASE_ROOTFS}" "${tmp}" 2>/dev/null || cp "${BASE_ROOTFS}" "${tmp}"
  set +e
  e2fsck -fy "${tmp}" >/tmp/torque-fc-jenkins-e2fsck-${cache_key}.log 2>&1
  local e=$?
  set -e
  [[ "${e}" -le 1 ]] || {
    cat /tmp/torque-fc-jenkins-e2fsck-${cache_key}.log >&2
    exit "${e}"
  }
  truncate -s 8G "${tmp}"
  resize2fs "${tmp}" >/tmp/torque-fc-jenkins-resize-${cache_key}.log 2>&1
  mkdir -p "${mnt}"
  mount -o loop "${tmp}" "${mnt}"
  cleanup_mounts() {
    set +e
    mountpoint -q "${mnt}/proc" && umount "${mnt}/proc"
    mountpoint -q "${mnt}/sys" && umount "${mnt}/sys"
    mountpoint -q "${mnt}/dev" && umount "${mnt}/dev"
    mountpoint -q "${mnt}/run" && umount "${mnt}/run"
    mountpoint -q "${mnt}" && umount "${mnt}"
  }
  trap cleanup_mounts RETURN
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  if ! grep -q 'jammy universe' "${mnt}/etc/apt/sources.list"; then
    cat >>"${mnt}/etc/apt/sources.list" <<'EOF'
deb http://archive.ubuntu.com/ubuntu jammy universe
deb http://archive.ubuntu.com/ubuntu jammy-updates main universe
deb http://security.ubuntu.com/ubuntu jammy-security main universe
EOF
  fi
  mount -t proc proc "${mnt}/proc"
  mount -t sysfs sysfs "${mnt}/sys"
  mount --bind /dev "${mnt}/dev"
  mount --bind /run "${mnt}/run"
  chroot "${mnt}" apt-get update >/tmp/torque-fc-jenkins-apt-update-${cache_key}.log 2>&1
  chroot "${mnt}" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${PACKAGES} >/tmp/torque-fc-jenkins-apt-install-${cache_key}.log 2>&1
  cleanup_mounts
  trap - RETURN
  mv "${tmp}" "${prepared}"
  echo "${prepared}"
}

write_nodes() {
  ssh "${SSH_OPTS[@]}" "root@${VM_IP}" 'printf "%s %s %s\n" "$(hostname)" "$(hostname -I | awk '\''{print $1}'\'')" "$(uname -r)"' 2>/dev/null
}

wait_ssh() {
  for _ in $(seq 1 180); do
    if ssh "${SSH_OPTS[@]}" "root@${VM_IP}" true >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

apply_vm() {
  for cmd in cp e2fsck mount resize2fs sha256sum ssh sysctl truncate umount; do
    require_cmd "${cmd}"
  done
  for path in "${BASE_ROOTFS}" "${KERNEL}" "${FIRECRACKER}" "${LAB_KEY}"; do
    [[ -e "${path}" ]] || {
      echo "missing ${path}" >&2
      exit 2
    }
  done
  mkdir -p "${RUN_ROOT}/vm"
  local prepared
  prepared="$(prepare_base_image)"

  if [[ -s "${RUN_ROOT}/receipt.json" && -f "${RUN_ROOT}/vm/pid" ]] && kill -0 "$(cat "${RUN_ROOT}/vm/pid")" 2>/dev/null; then
    if wait_ssh; then
      write_nodes >"${RUN_ROOT}/nodes.txt"
      cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-direct/v1","kind":"FirecrackerDirectVMReceipt","status":"succeeded","runId":"${RUN_ID}","nodeCount":1,"sshReady":1,"subnet":"${CIDR}","bridge":"${BRIDGE_NAME}","idempotentReuse":true}
EOF
      cat "${RUN_ROOT}/nodes.txt"
      return
    fi
  fi

  cleanup_run
  mkdir -p "${RUN_ROOT}/vm"
  ip link add name "${BRIDGE_NAME}" type bridge
  ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}"
  ip link set "${BRIDGE_NAME}" up
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
  iptables -t nat -C POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null || \
    iptables -t nat -A POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE

  vm="${RUN_ROOT}/vm"
  cp --reflink=auto "${prepared}" "${vm}/rootfs.ext4" 2>/dev/null || cp "${prepared}" "${vm}/rootfs.ext4"
  e2fsck -fy "${vm}/rootfs.ext4" >/dev/null 2>&1 || true
  mnt="${vm}/mnt"
  mkdir -p "${mnt}"
  mount -o loop "${vm}/rootfs.ext4" "${mnt}"
  printf 'jenkins-00\n' >"${mnt}/etc/hostname"
  cat >"${mnt}/etc/hosts" <<EOF
127.0.0.1 localhost
127.0.1.1 jenkins-00
${VM_IP} jenkins-00
EOF
  cat >"${mnt}/etc/network/interfaces" <<EOF
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet static
    address ${VM_IP}
    netmask 255.255.255.0
    gateway ${GATEWAY}
EOF
  mkdir -p "${mnt}/etc/systemd/network"
  cat >"${mnt}/etc/systemd/network/10-eth0.network" <<EOF
[Match]
Name=eth0

[Network]
Address=${VM_IP}/24
Gateway=${GATEWAY}
DNS=1.1.1.1
DNS=8.8.8.8
EOF
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  rm -f "${mnt}/etc/machine-id" "${mnt}/var/lib/dbus/machine-id" 2>/dev/null || true
  touch "${mnt}/etc/machine-id"
  mkdir -p "${mnt}/etc/systemd/system/multi-user.target.wants"
  ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
  umount "${mnt}"

  ip tuntap add dev "${TAP_NAME}" mode tap
  ip link set "${TAP_NAME}" master "${BRIDGE_NAME}"
  ip link set "${TAP_NAME}" up
  mac_octet="$(printf '%02x' "${SUBNET_OCTET}")"
  cat >"${vm}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=0 systemd.legacy_systemd_cgroup_controller=1 systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":2,"mem_size_mib":3072},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${TAP_NAME}","guest_mac":"06:00:00:${mac_octet}:00:0a"}],"logger":{"log_path":"${vm}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
  "${FIRECRACKER}" --api-sock "${vm}/fc.sock" --config-file "${vm}/vm.json" >"${vm}/console.log" 2>&1 &
  echo $! >"${vm}/pid"

  wait_ssh || {
    echo "jenkins VM did not become reachable over SSH" >&2
    exit 1
  }
  write_nodes >"${RUN_ROOT}/nodes.txt"
  cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-direct/v1","kind":"FirecrackerDirectVMReceipt","status":"succeeded","runId":"${RUN_ID}","nodeCount":1,"sshReady":1,"subnet":"${CIDR}","bridge":"${BRIDGE_NAME}"}
EOF
  cat "${RUN_ROOT}/nodes.txt"
}

case "${mode}" in
  apply) apply_vm ;;
  delete|cleanup) cleanup_run 1 ;;
  *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
esac
SCRIPT
chmod +x "${RUN_ROOT}/jenkins-vm-lab.sh"
RUN_ROOT="${RUN_ROOT}" "${RUN_ROOT}/jenkins-vm-lab.sh" apply
REMOTE
  jenkins_vm_applied=1
}

push_workspace_to_vm() {
  ops_log "copy Torque workspace and host access key into Jenkins VM"
  ops_set_ssh_base_args
  scp "${OPS_SSH_ARGS[@]}" "${workspace_bundle}" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_workspace_bundle}" \
    >"${OPS_RUN_DIR}/remote/scp-workspace.out" 2>"${OPS_RUN_DIR}/remote/scp-workspace.err"
  scp "${OPS_SSH_ARGS[@]}" "${job_env_file}" "$(ops_ssh_target "${TORQUE_LAB_SSH}"):${remote_job_env_file}" \
    >"${OPS_RUN_DIR}/remote/scp-job-env.out" 2>"${OPS_RUN_DIR}/remote/scp-job-env.err"
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "WORKSPACE_BUNDLE='${remote_workspace_bundle}' ACCESS_KEY='${remote_access_key}' JOB_ENV_FILE='${remote_job_env_file}' JENKINS_WAR='${remote_jenkins_war}' VM_IP='${jenkins_ip}' JENKINS_JOB='${jenkins_job}' bash -se" <<'REMOTE' \
    >"${OPS_RUN_DIR}/remote/configure-jenkins-vm.out" 2>"${OPS_RUN_DIR}/remote/configure-jenkins-vm.err"
set -euo pipefail
LAB_KEY="/opt/firecracker-sandbox-lab/lab_ssh_key"
SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5)

ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" 'install -d -m 0755 /opt/torque-demo'
ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" 'rm -rf /opt/torque-demo/*'
cat "${WORKSPACE_BUNDLE}" | ssh "${SSH_OPTS[@]}" "root@${VM_IP}" 'tar -xzf - -C /opt/torque-demo'

ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" 'id -u jenkins >/dev/null 2>&1 || useradd --system --create-home --home-dir /var/lib/jenkins --shell /bin/bash jenkins'
ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" 'install -d -o jenkins -g jenkins -m 0755 /opt/jenkins /var/cache/jenkins /var/lib/jenkins /var/log/jenkins /var/lib/jenkins/init.groovy.d /var/lib/jenkins/torque/keys'
scp -q "${SSH_OPTS[@]}" "${ACCESS_KEY}" "root@${VM_IP}:/var/lib/jenkins/torque/keys/host_ed25519"
scp -q "${SSH_OPTS[@]}" "${JOB_ENV_FILE}" "root@${VM_IP}:/var/lib/jenkins/torque/jenkins-job.env"
if [[ ! -s "${JENKINS_WAR}" ]]; then
  curl -fsSL -o "${JENKINS_WAR}" https://get.jenkins.io/war-stable/latest/jenkins.war
fi
scp -q "${SSH_OPTS[@]}" "${JENKINS_WAR}" "root@${VM_IP}:/opt/jenkins/jenkins.war"

ssh "${SSH_OPTS[@]}" "root@${VM_IP}" "JENKINS_JOB='${JENKINS_JOB}' bash -se" <<'NODE'
set -euo pipefail
install -d -o jenkins -g jenkins -m 0755 /opt/torque-demo
chown -R jenkins:jenkins /opt/torque-demo /var/cache/jenkins /var/lib/jenkins /var/log/jenkins
chmod 0600 /var/lib/jenkins/torque/keys/host_ed25519
chown jenkins:jenkins /var/lib/jenkins/torque/keys/host_ed25519
chown jenkins:jenkins /opt/jenkins/jenkins.war
chmod 0640 /var/lib/jenkins/torque/jenkins-job.env
chown jenkins:jenkins /var/lib/jenkins/torque/jenkins-job.env
cat >/var/lib/jenkins/init.groovy.d/01-disable-security.groovy <<'EOF'
import hudson.security.AuthorizationStrategy
import jenkins.model.Jenkins

def instance = Jenkins.get()
instance.setSecurityRealm(hudson.security.SecurityRealm.NO_AUTHENTICATION)
instance.setAuthorizationStrategy(AuthorizationStrategy.UNSECURED)
instance.setCrumbIssuer(null)
instance.save()
EOF
cat >/var/lib/jenkins/init.groovy.d/10-create-torque-job.groovy <<EOF
import hudson.model.FreeStyleProject
import hudson.tasks.ArtifactArchiver
import hudson.tasks.Shell
import jenkins.model.Jenkins

def instance = Jenkins.get()
def jobName = "${JENKINS_JOB}"
def shellScript = """#!/usr/bin/env bash
set -euo pipefail
bash /opt/torque-demo/testdata/stack/e2e/33-firecracker-jenkins-postgres-backup/jenkins-job.sh /var/lib/jenkins/torque/jenkins-job.env
"""
def artifactPattern = "evidence/**,torque-firecracker-postgres-backup-evidence.tgz"
def job = instance.getItem(jobName)
if (job == null) {
    job = instance.createProject(FreeStyleProject, jobName)
}
job.buildersList.clear()
job.buildersList.add(new Shell(shellScript))
job.publishersList.clear()
def archiver = new ArtifactArchiver(artifactPattern)
if (archiver.metaClass.respondsTo(archiver, "setAllowEmptyArchive", Boolean.TYPE)) {
    archiver.setAllowEmptyArchive(false)
}
if (archiver.metaClass.respondsTo(archiver, "setOnlyIfSuccessful", Boolean.TYPE)) {
    archiver.setOnlyIfSuccessful(false)
}
if (archiver.metaClass.respondsTo(archiver, "setFingerprint", Boolean.TYPE)) {
    archiver.setFingerprint(true)
}
job.publishersList.add(archiver)
job.setConcurrentBuild(false)
job.setDisabled(false)
job.save()
instance.save()
EOF
cat >/etc/systemd/system/jenkins.service <<'EOF'
[Unit]
Description=Jenkins CI
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=jenkins
Group=jenkins
Environment=JENKINS_HOME=/var/lib/jenkins
WorkingDirectory=/var/lib/jenkins
ExecStart=/usr/bin/java -Djenkins.install.runSetupWizard=false -jar /opt/jenkins/jenkins.war --httpListenAddress=0.0.0.0 --httpPort=8080 --webroot=/var/cache/jenkins/war
Restart=on-failure
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now jenkins >/dev/null
systemctl restart jenkins
NODE

for _ in $(seq 1 180); do
  if ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" 'curl -fsS http://127.0.0.1:8080/login >/dev/null'; then
    exit 0
  fi
  sleep 2
done
echo "Jenkins did not become ready inside the Firecracker VM" >&2
exit 1
REMOTE
}

open_local_jenkins_tunnel() {
  ops_log "open local SSH tunnel to Jenkins UI"
  ops_set_ssh_base_args
  ssh -S "${local_tunnel_control}" -O exit "$(ops_ssh_target "${TORQUE_LAB_SSH}")" >/dev/null 2>&1 || true
  rm -f "${local_tunnel_control}"
  ssh "${OPS_SSH_ARGS[@]}" -M -S "${local_tunnel_control}" -fN \
    -L "127.0.0.1:${jenkins_local_port}:${jenkins_ip}:8080" "$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  for _ in $(seq 1 90); do
    if curl -fsS "http://127.0.0.1:${jenkins_local_port}/login" >"${OPS_RUN_DIR}/verification/jenkins-login.html" 2>"${OPS_RUN_DIR}/verification/jenkins-login.err"; then
      return 0
    fi
    sleep 2
  done
  ops_fail "local Jenkins tunnel did not become ready"
}

trigger_and_wait_for_jenkins_build() {
  local job_api build_number build_api build_result build_json
  job_api="http://127.0.0.1:${jenkins_local_port}/job/${jenkins_job}/api/json"
  curl -fsS "${job_api}" >"${OPS_RUN_DIR}/verification/job-api.json"
  build_number="$(
    python3 - "${OPS_RUN_DIR}/verification/job-api.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], "r", encoding="utf-8"))["nextBuildNumber"])
PY
  )"
  printf '%s\n' "${build_number}" >"${OPS_RUN_DIR}/verification/build-number.txt"
  curl -fsS -X POST "http://127.0.0.1:${jenkins_local_port}/job/${jenkins_job}/build?delay=0sec" \
    -D "${OPS_RUN_DIR}/verification/build-trigger.headers" \
    -o /dev/null
  build_api="http://127.0.0.1:${jenkins_local_port}/job/${jenkins_job}/${build_number}/api/json"
  build_json="${OPS_RUN_DIR}/verification/build-api.json"
  for _ in $(seq 1 360); do
    if curl -fsS "${build_api}" >"${build_json}.tmp" 2>"${OPS_RUN_DIR}/verification/build-api.err"; then
      mv "${build_json}.tmp" "${build_json}"
      build_result="$(
        python3 - "${build_json}" <<'PY'
import json
import sys
doc = json.load(open(sys.argv[1], "r", encoding="utf-8"))
if doc.get("building"):
    print("BUILDING")
else:
    print(doc.get("result") or "UNKNOWN")
PY
      )"
      if [[ "${build_result}" != "BUILDING" ]]; then
        break
      fi
    fi
    sleep 10
  done
  curl -fsS "http://127.0.0.1:${jenkins_local_port}/job/${jenkins_job}/${build_number}/consoleText" \
    >"${OPS_RUN_DIR}/verification/console.txt" 2>"${OPS_RUN_DIR}/verification/console.err"
  [[ "${build_result}" == "SUCCESS" ]] || ops_fail "Jenkins build ${build_number} finished with result ${build_result}"
}

pull_jenkins_artifacts() {
  ops_log "pull Jenkins workspace evidence and journal"
  mkdir -p "${OPS_RUN_DIR}/remote" "${OPS_RUN_DIR}/verification/workspace"
  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "VM_IP='${jenkins_ip}' bash -se" <<'REMOTE' >"${OPS_RUN_DIR}/remote/jenkins-workspace.tgz"
set -euo pipefail
LAB_KEY="/opt/firecracker-sandbox-lab/lab_ssh_key"
SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5)
ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" \
  'tar -C /var/lib/jenkins/workspace/torque-firecracker-postgres-backup -czf - evidence torque-firecracker-postgres-backup-evidence.tgz runtime'
REMOTE
  tar -xzf "${OPS_RUN_DIR}/remote/jenkins-workspace.tgz" -C "${OPS_RUN_DIR}/verification/workspace"
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "VM_IP='${jenkins_ip}' bash -se" <<'REMOTE' \
    >"${OPS_RUN_DIR}/remote/jenkins-journal.txt" 2>"${OPS_RUN_DIR}/remote/jenkins-journal.err"
set -euo pipefail
LAB_KEY="/opt/firecracker-sandbox-lab/lab_ssh_key"
SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5)
ssh -n "${SSH_OPTS[@]}" "root@${VM_IP}" 'journalctl -u jenkins --no-pager -n 400'
REMOTE
}

write_verification_receipt() {
  python3 - "${OPS_RUN_DIR}/verification/receipt.json" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${OPS_RUN_DIR}/verification/build-api.json" \
    "${OPS_RUN_DIR}/verification/workspace/evidence/summary.json" \
    "${jenkins_local_port}" <<'PY'
import json
import sys

receipt_path, task_id, run_id, build_api_path, summary_path, port = sys.argv[1:7]
build = json.load(open(build_api_path, "r", encoding="utf-8"))
summary = json.load(open(summary_path, "r", encoding="utf-8"))
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsVerificationReceipt",
    "status": "succeeded",
    "taskId": task_id,
    "runId": run_id,
    "jenkins": {
        "jobName": build.get("fullDisplayName"),
        "buildNumber": build.get("number"),
        "result": build.get("result"),
        "url": f"http://127.0.0.1:{port}{build.get('url', '')}".replace("http://127.0.0.1:" + port + "http://", "http://"),
    },
    "baseRunId": summary.get("baseRunId"),
    "backupRunId": summary.get("backupRunId"),
    "backup": summary.get("backup", {}),
}
with open(receipt_path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
}

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/remote" "${OPS_RUN_DIR}/verification"
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
        {
            "id": "host/141",
            "type": "ssh-host",
            "transport": "ssh",
            "address": lab_ssh,
        },
        {
            "id": "vm/jenkins-00",
            "type": "firecracker-vm",
            "transport": "ssh-via-lab-host",
            "address": "172.31.236.10",
            "role": "jenkins",
        },
        {
            "id": "vm/pg-direct-00..05",
            "type": "firecracker-vm-set",
            "transport": "ssh-via-lab-host",
            "count": 6,
            "subnet": "172.31.240.0/24",
            "roles": {
                "172.31.240.10": ["postgres-primary"],
                "172.31.240.11..15": ["postgres-replica"],
            },
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
  "reason=explicit live Firecracker Jenkins plus PostgreSQL backup confirmation"

if [[ "${destroy_existing}" == "1" ]]; then
  destroy_existing_labs >"${OPS_RUN_DIR}/remote/destroy-existing.out" 2>"${OPS_RUN_DIR}/remote/destroy-existing.err"
fi

build_linux_workspace
install_host_access_key
bootstrap_jenkins_vm
push_workspace_to_vm
open_local_jenkins_tunnel
trigger_and_wait_for_jenkins_build
base_stack_applied=1
pull_jenkins_artifacts
write_verification_receipt
ops_write_json_object "${OPS_RUN_DIR}/result.json" \
  "apiVersion=torque.dev/e2e/v1" \
  "kind=OpsLabResult" \
  "taskId=${OPS_TASK_ID}" \
  "runId=${OPS_RUN_ID}" \
  "status=succeeded" \
  "finishedAt=$(ops_utc_now)"

ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/receipt.json'" >"${OPS_RUN_DIR}/remote/jenkins-vm-receipt.json"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/nodes.txt'" >"${OPS_RUN_DIR}/remote/jenkins-vm-nodes.txt"
