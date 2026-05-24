#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-LIFE-012.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Delete Firecracker labs after proof collection. Default.
  --no-cleanup         Leave Firecracker labs running for debugging.
  -h, --help           Show this help.

STACK-LIFE-012 proves Kubernetes lifecycle parity on the real Firecracker lab:

  k3s and kubeadm/upstream Kubernetes both run:
    k8s.cluster.inspect -> dynamic targetsFrom -> k8s.cert.inspect ->
    policy-gated k8s.cert.renew -> k8s.cluster.verify ->
    k8s-lifecycle-summary.json

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

cleanup_enabled=1

while [[ $# -gt 0 ]]; do
  case "$1" in
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Firecracker lifecycle parity lab without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd jq
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh
ops_require_cmd tar

repo_root="$(ops_repo_root)"
ops_init_run "STACK-LIFE-012"
started_at="$(ops_utc_now)"
torque_bin="${repo_root}/bin/torque"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-life-012.XXXXXX")"
stack_root="${scratch_root}/stacks"
mkdir -p "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/verification" "${OPS_RUN_DIR}/remote" "${OPS_RUN_DIR}/redaction" "${stack_root}"

safe_suffix="$(printf '%s' "${OPS_RUN_ID}" | tr '[:upper:]_' '[:lower:]-' | tr -cd 'a-z0-9-' | cut -c1-10)"
short_suffix="$(printf '%s' "${safe_suffix}" | cut -c1-5)"
cksum_value="$(printf '%s' "${OPS_RUN_ID}" | cksum | awk '{print $1}')"
k3s_subnet_octet="$((206 + (cksum_value % 15)))"
kubeadm_subnet_octet="$((226 + (cksum_value % 15)))"
k3s_remote_root="/var/lib/torque-stack-life-012/k3s-${safe_suffix}"
kubeadm_remote_root="/var/lib/torque-stack-life-012/kubeadm-${safe_suffix}"

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
} | ops_redact_stdin "${OPS_RUN_DIR}/redaction/probe.redacted.txt"

cleanup_remote() {
  [[ "${cleanup_enabled}" == "1" ]] || return 0
  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "set +e
for root in '${k3s_remote_root}' '${kubeadm_remote_root}'; do
  if [ -x \"\${root}/cluster-lab.sh\" ]; then
    \"\${root}/cluster-lab.sh\" delete
  fi
done
true" >"${OPS_RUN_DIR}/remote/fallback-cleanup.out" 2>"${OPS_RUN_DIR}/remote/fallback-cleanup.stderr" || true
}
trap cleanup_remote EXIT

write_stacks() {
  python3 - "${stack_root}" "${k3s_remote_root}" "${kubeadm_remote_root}" "${k3s_subnet_octet}" "${kubeadm_subnet_octet}" "${short_suffix}" <<'PY'
import sys
import textwrap
from pathlib import Path

stack_root = Path(sys.argv[1])
k3s_remote_root = sys.argv[2]
kubeadm_remote_root = sys.argv[3]
k3s_subnet = sys.argv[4]
kubeadm_subnet = sys.argv[5]
short_suffix = sys.argv[6]


def indent(value: str, spaces: int) -> str:
    pad = " " * spaces
    return "\n".join(pad + line if line else pad for line in value.rstrip("\n").splitlines())


common_remote = r'''
#!/usr/bin/env bash
set -euo pipefail

mode="${1:-apply}"
RUN_ROOT="${RUN_ROOT:?}"
DISTRO="${DISTRO:?}"
NODE_COUNT="${NODE_COUNT:-2}"
SUBNET_OCTET="${SUBNET_OCTET:?}"
BRIDGE_NAME="${BRIDGE_NAME:?}"
TAP_PREFIX="${TAP_PREFIX:?}"
RUN_ID="${RUN_ID:-stack-life-012}"
BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/firecracker-sandbox-lab/vmlinux.bin}"
K3S_BIN="${K3S_BIN:-/usr/local/bin/k3s}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
CACHE_ROOT="${CACHE_ROOT:-/var/lib/torque-stack-life-012/cache}"
NET_PREFIX="172.31.${SUBNET_OCTET}"
GATEWAY="${NET_PREFIX}.1"
SERVER_IP="${NET_PREFIX}.10"
CIDR="${NET_PREFIX}.0/24"
SSH_OPTS=(-i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=4)

node_ip() { printf '%s.%d' "${NET_PREFIX}" "$((10 + $1))"; }
node_name() { printf '%s-%02d' "${DISTRO}" "$1"; }

cleanup_run() {
  set +e
  if [[ -d "${RUN_ROOT}/vms" ]]; then
    for pid_file in "${RUN_ROOT}"/vms/*/pid; do
      [[ -f "${pid_file}" ]] && kill "$(cat "${pid_file}")" 2>/dev/null
    done
    sleep 1
    for pid_file in "${RUN_ROOT}"/vms/*/pid; do
      [[ -f "${pid_file}" ]] && kill -9 "$(cat "${pid_file}")" 2>/dev/null
    done
  fi
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    ip link del "${TAP_PREFIX}${i}" 2>/dev/null
  done
  ip link set "${BRIDGE_NAME}" down 2>/dev/null
  ip link del "${BRIDGE_NAME}" type bridge 2>/dev/null
  iptables -t nat -D POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null
  rm -rf "${RUN_ROOT}/vms" "${RUN_ROOT}/receipt.json" "${RUN_ROOT}/nodes.txt" "${RUN_ROOT}/pods.txt" "${RUN_ROOT}/kubeconfig.yaml"
  set -e
}

if [[ "${mode}" == "delete" ]]; then
  cleanup_run
  rm -rf "${RUN_ROOT}"
  exit 0
fi

require_path() {
  [[ -e "$1" ]] || { echo "missing required path: $1" >&2; exit 2; }
}

for path in "${BASE_ROOTFS}" "${KERNEL}" "${FIRECRACKER}" "${LAB_KEY}"; do
  require_path "${path}"
done
if [[ "${DISTRO}" == "k3s" ]]; then
  require_path "${K3S_BIN}"
fi

prepare_base_image() {
  mkdir -p "${CACHE_ROOT}"
  local key prepared tmp mnt
  key="$(
    {
      sha256sum "${BASE_ROOTFS}" "${KERNEL}"
      if [[ "${DISTRO}" == "k3s" ]]; then sha256sum "${K3S_BIN}"; fi
      printf 'distro=%s\n' "${DISTRO}"
      printf 'generation=stack-life-012-v1\n'
    } | sha256sum | awk '{print substr($1,1,16)}'
  )"
  prepared="${CACHE_ROOT}/prepared-${DISTRO}-${key}.ext4"
  if [[ -s "${prepared}" ]]; then
    echo "${prepared}"
    return
  fi
  tmp="${prepared}.tmp"
  mnt="${CACHE_ROOT}/mnt-${DISTRO}-${key}"
  rm -f "${tmp}"
  cp --reflink=auto "${BASE_ROOTFS}" "${tmp}" 2>/dev/null || cp "${BASE_ROOTFS}" "${tmp}"
  set +e
  e2fsck -fy "${tmp}" >"${CACHE_ROOT}/e2fsck-${DISTRO}-${key}.log" 2>&1
  local fsck_code=$?
  set -e
  [[ "${fsck_code}" -le 1 ]] || { cat "${CACHE_ROOT}/e2fsck-${DISTRO}-${key}.log" >&2; exit "${fsck_code}"; }
  truncate -s 12G "${tmp}"
  resize2fs "${tmp}" >"${CACHE_ROOT}/resize-${DISTRO}-${key}.log" 2>&1
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
  fail_prepare() {
    cleanup_mounts
    rm -f "${tmp}"
  }
  trap cleanup_mounts RETURN
  trap fail_prepare ERR
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions timeout:2 attempts:3\n' >"${mnt}/etc/resolv.conf"
  mount -t proc proc "${mnt}/proc"
  mount -t sysfs sysfs "${mnt}/sys"
  mount --bind /dev "${mnt}/dev"
  mount --bind /run "${mnt}/run"
  chroot "${mnt}" apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 update >"${CACHE_ROOT}/apt-update-${DISTRO}-${key}.log" 2>&1
  chroot "${mnt}" env DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 install -y --no-install-recommends \
    openssh-server ca-certificates curl gpg jq tar gzip xz-utils iptables conntrack ipset ethtool socat ebtables containerd >"${CACHE_ROOT}/apt-install-base-${DISTRO}-${key}.log" 2>&1
  if [[ "${DISTRO}" == "kubeadm" ]]; then
    mkdir -p "${mnt}/etc/apt/keyrings"
    chroot "${mnt}" sh -ec 'curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.30/deb/Release.key | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg'
    printf 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.30/deb/ /\n' >"${mnt}/etc/apt/sources.list.d/kubernetes.list"
    chroot "${mnt}" apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 update >"${CACHE_ROOT}/apt-update-kubernetes-${key}.log" 2>&1
    chroot "${mnt}" env DEBIAN_FRONTEND=noninteractive apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 install -y --no-install-recommends kubelet kubeadm kubectl >"${CACHE_ROOT}/apt-install-kubernetes-${key}.log" 2>&1
    mkdir -p "${mnt}/etc/containerd"
    chroot "${mnt}" sh -ec 'containerd config default > /etc/containerd/config.toml'
    sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' "${mnt}/etc/containerd/config.toml"
    chroot "${mnt}" systemctl enable containerd kubelet ssh >/dev/null 2>&1 || true
  else
    install -m 0755 "${K3S_BIN}" "${mnt}/usr/local/bin/k3s"
    chroot "${mnt}" systemctl enable ssh >/dev/null 2>&1 || true
  fi
  chroot "${mnt}" update-alternatives --set iptables /usr/sbin/iptables-legacy >/dev/null 2>&1 || true
  chroot "${mnt}" update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy >/dev/null 2>&1 || true
  cleanup_mounts
  trap - RETURN ERR
  mv "${tmp}" "${prepared}"
  echo "${prepared}"
}

write_k3s_service() {
  local role="$1" ip="$2"
  if [[ "${role}" == "server" ]]; then
    cat <<EOF
[Unit]
Description=Lightweight Kubernetes
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
Environment=K3S_TOKEN=stack-life-012-token
ExecStart=/usr/local/bin/k3s server --cluster-init --node-ip ${ip} --advertise-address ${ip} --bind-address 0.0.0.0 --tls-san ${ip} --tls-san 127.0.0.1 --flannel-iface eth0 --flannel-backend host-gw --disable-kube-proxy --disable-network-policy --write-kubeconfig-mode 0644 --disable traefik --disable servicelb --disable metrics-server --disable coredns --disable local-storage --kubelet-arg=fail-cgroupv1=false
KillMode=process
Delegate=yes
LimitNOFILE=1048576
TasksMax=infinity
Restart=always
RestartSec=5s
TimeoutStartSec=0
[Install]
WantedBy=multi-user.target
EOF
  else
    cat <<EOF
[Unit]
Description=Lightweight Kubernetes Agent
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
Environment=K3S_TOKEN=stack-life-012-token
ExecStart=/usr/local/bin/k3s agent --server https://${SERVER_IP}:6443 --node-ip ${ip} --flannel-iface eth0 --kubelet-arg=fail-cgroupv1=false
KillMode=process
Delegate=yes
LimitNOFILE=1048576
TasksMax=infinity
Restart=always
RestartSec=5s
TimeoutStartSec=0
[Install]
WantedBy=multi-user.target
EOF
  fi
}

configure_vm() {
  local prepared="$1" i="$2" vm="$3" name="$4" ip="$5"
  cp --reflink=auto "${prepared}" "${vm}/rootfs.ext4" 2>/dev/null || cp "${prepared}" "${vm}/rootfs.ext4"
  e2fsck -fy "${vm}/rootfs.ext4" >/dev/null 2>&1 || true
  local mnt="${vm}/mnt"
  mkdir -p "${mnt}"
  mount -o loop "${vm}/rootfs.ext4" "${mnt}"
  printf '%s\n' "${name}" >"${mnt}/etc/hostname"
  cat >"${mnt}/etc/hosts" <<EOF
127.0.0.1 localhost
127.0.1.1 ${name}
${NET_PREFIX}.10 ${DISTRO}-00
${NET_PREFIX}.11 ${DISTRO}-01
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
  rm -rf "${mnt}/var/lib/rancher/k3s" "${mnt}/var/lib/kubelet" "${mnt}/etc/kubernetes" "${mnt}/run/k3s" "${mnt}/run/flannel" 2>/dev/null || true
  mkdir -p "${mnt}/etc/systemd/system/multi-user.target.wants"
  ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
  if [[ "${DISTRO}" == "k3s" ]]; then
    local role="agent"
    [[ "${i}" -eq 0 ]] && role="server"
    write_k3s_service "${role}" "${ip}" >"${mnt}/etc/systemd/system/k3s.service"
    ln -sf /etc/systemd/system/k3s.service "${mnt}/etc/systemd/system/multi-user.target.wants/k3s.service"
  fi
  umount "${mnt}"
}

start_firecracker_vms() {
  local prepared="$1"
  ip link add "${BRIDGE_NAME}" type bridge
  ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}"
  ip link set "${BRIDGE_NAME}" up
  iptables -t nat -A POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE
  mkdir -p "${RUN_ROOT}/vms"
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    local name ip vm tap mac
    name="$(node_name "${i}")"
    ip="$(node_ip "${i}")"
    vm="${RUN_ROOT}/vms/${name}"
    tap="${TAP_PREFIX}${i}"
    mac="$(printf '02:FC:%02X:%02X:%02X:%02X' "${SUBNET_OCTET}" "$((10+i))" "$((i/256))" "$((i%256))")"
    mkdir -p "${vm}"
    configure_vm "${prepared}" "${i}" "${vm}" "${name}" "${ip}"
    ip tuntap add dev "${tap}" mode tap
    ip link set "${tap}" master "${BRIDGE_NAME}"
    ip link set "${tap}" up
    cat >"${vm}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=0 systemd.legacy_systemd_cgroup_controller=1 cgroup_memory=1 cgroup_enable=memory cgroup_enable=cpuset systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":2,"mem_size_mib":2048},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${tap}","guest_mac":"${mac}"}],"logger":{"log_path":"${vm}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
    "${FIRECRACKER}" --api-sock "${vm}/fc.sock" --config-file "${vm}/vm.json" >"${vm}/console.log" 2>&1 &
    echo $! >"${vm}/pid"
    echo "started ${name} ${ip}"
  done
}

wait_ssh() {
  local ip="$1"
  for _ in $(seq 1 120); do
    if ssh "${SSH_OPTS[@]}" "root@${ip}" true >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

bootstrap_kubeadm() {
  wait_ssh "${SERVER_IP}" || { echo "kubeadm server ssh not ready" >&2; exit 1; }
  for i in $(seq 1 "$((NODE_COUNT - 1))"); do
    wait_ssh "$(node_ip "${i}")" || { echo "kubeadm worker ${i} ssh not ready" >&2; exit 1; }
  done
  ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" "cat >/root/bootstrap-kubeadm.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SERVER_IP="${1:?}"
swapoff -a || true
sed -i '/ swap / s/^/#/' /etc/fstab 2>/dev/null || true
cat >/etc/modules-load.d/k8s.conf <<MOD
overlay
br_netfilter
MOD
modprobe overlay || true
modprobe br_netfilter || true
cat >/etc/sysctl.d/99-kubernetes-cri.conf <<SYS
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
net.ipv4.ip_forward=1
SYS
sysctl --system >/dev/null
systemctl restart containerd
systemctl enable --now kubelet
kubeadm reset -f >/dev/null 2>&1 || true
write_simple_cni() {
  rm -rf /etc/cni/net.d
  mkdir -p /etc/cni/net.d
  cat >/etc/cni/net.d/10-torque-bridge.conflist <<'CNI'
{
  "cniVersion": "0.3.1",
  "name": "torque-bridge",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "cni0",
      "isDefaultGateway": true,
      "ipMasq": true,
      "ipam": {
        "type": "host-local",
        "subnet": "10.244.0.0/16",
        "routes": [{"dst": "0.0.0.0/0"}]
      }
    },
    {
      "type": "portmap",
      "capabilities": {"portMappings": true}
    }
  ]
}
CNI
}
write_simple_cni
systemctl restart containerd
kubeadm init --pod-network-cidr=10.244.0.0/16 --apiserver-advertise-address="${SERVER_IP}" --skip-phases=addon/kube-proxy,addon/coredns --ignore-preflight-errors=all
mkdir -p /root/.kube
cp /etc/kubernetes/admin.conf /root/.kube/config
kubectl --kubeconfig /etc/kubernetes/admin.conf taint nodes --all node-role.kubernetes.io/control-plane- || true
EOF
  ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" "chmod +x /root/bootstrap-kubeadm.sh && /root/bootstrap-kubeadm.sh '${SERVER_IP}'" >"${RUN_ROOT}/kubeadm-init.log" 2>&1
  local join_cmd
  join_cmd="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" "kubeadm token create --print-join-command")"
  for i in $(seq 1 "$((NODE_COUNT - 1))"); do
    local ip
    ip="$(node_ip "${i}")"
    ssh "${SSH_OPTS[@]}" "root@${ip}" "cat >/root/join-kubeadm.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
swapoff -a || true
modprobe overlay || true
modprobe br_netfilter || true
sysctl -w net.bridge.bridge-nf-call-iptables=1 net.bridge.bridge-nf-call-ip6tables=1 net.ipv4.ip_forward=1 >/dev/null
systemctl restart containerd
systemctl enable --now kubelet
kubeadm reset -f >/dev/null 2>&1 || true
write_simple_cni() {
  rm -rf /etc/cni/net.d
  mkdir -p /etc/cni/net.d
  cat >/etc/cni/net.d/10-torque-bridge.conflist <<'CNI'
{
  "cniVersion": "0.3.1",
  "name": "torque-bridge",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "cni0",
      "isDefaultGateway": true,
      "ipMasq": true,
      "ipam": {
        "type": "host-local",
        "subnet": "10.244.0.0/16",
        "routes": [{"dst": "0.0.0.0/0"}]
      }
    },
    {
      "type": "portmap",
      "capabilities": {"portMappings": true}
    }
  ]
}
CNI
}
write_simple_cni
systemctl restart containerd
${join_cmd} --ignore-preflight-errors=all
EOF
    ssh "${SSH_OPTS[@]}" "root@${ip}" "chmod +x /root/join-kubeadm.sh && /root/join-kubeadm.sh" >"${RUN_ROOT}/kubeadm-join-${i}.log" 2>&1
  done
}

wait_ready() {
  local kubectl_cmd="$1"
  for attempt in $(seq 1 360); do
    nodes_text="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" "${kubectl_cmd} get nodes -o wide --no-headers 2>/dev/null" || true)"
    printf '%s\n' "${nodes_text}" >"${RUN_ROOT}/nodes.txt"
    ready_count="$(printf '%s\n' "${nodes_text}" | awk '$2=="Ready"{c++} END{print c+0}')"
    if [[ "${ready_count}" -ge "${NODE_COUNT}" ]]; then
      pods_json="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" "${kubectl_cmd} get pods -A -o json 2>/dev/null" || true)"
      printf '%s\n' "${pods_json}" >"${RUN_ROOT}/pods.json"
      ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" "${kubectl_cmd} get pods -A -o wide || true" >"${RUN_ROOT}/pods.txt" 2>&1 || true
      unhealthy_pods="$(
        jq -r '
          (.items // [])
          | map(select(.status.phase != "Succeeded"))
          | map(select(
              .status.phase != "Running" or
              (((.status.containerStatuses // []) | length) > 0 and
               ((.status.containerStatuses // []) | map(select(.ready == true)) | length) < ((.status.containerStatuses // []) | length))
            ))
          | .[]
          | "\(.metadata.namespace)/\(.metadata.name):\(.status.phase)"
        ' "${RUN_ROOT}/pods.json" 2>/dev/null || true
      )"
      if [[ -z "${unhealthy_pods}" ]]; then
        return 0
      fi
    fi
    if (( attempt % 30 == 0 )); then
      echo "waiting ${DISTRO} cluster attempt=${attempt} ready=${ready_count}/${NODE_COUNT}"
      cat "${RUN_ROOT}/nodes.txt"
      if [[ -n "${unhealthy_pods:-}" ]]; then
        printf 'waiting for healthy pods:\n%s\n' "${unhealthy_pods}"
      fi
    fi
    sleep 2
  done
  return 1
}

mkdir -p "${RUN_ROOT}"
if [[ -s "${RUN_ROOT}/receipt.json" ]] && jq -e --arg distro "${DISTRO}" '.status=="succeeded" and .distro==$distro' "${RUN_ROOT}/receipt.json" >/dev/null 2>&1; then
  echo "${DISTRO} cluster already ready"
  cat "${RUN_ROOT}/receipt.json"
  exit 0
fi

cleanup_run
prepared="$(prepare_base_image)"
start_firecracker_vms "${prepared}"

if [[ "${DISTRO}" == "kubeadm" ]]; then
  bootstrap_kubeadm
  wait_ready "kubectl --kubeconfig /etc/kubernetes/admin.conf" || {
    ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'journalctl -u kubelet --no-pager -n 200 || true' >"${RUN_ROOT}/kubelet-journal.txt" 2>&1 || true
    echo "kubeadm cluster failed; see ${RUN_ROOT}" >&2
    exit 1
  }
  ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'cat /etc/kubernetes/admin.conf' >"${RUN_ROOT}/kubeconfig.yaml"
else
  wait_ssh "${SERVER_IP}" || { echo "k3s server ssh not ready" >&2; exit 1; }
  wait_ready "/usr/local/bin/k3s kubectl" || {
    ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'journalctl -u k3s --no-pager -n 200 || true' >"${RUN_ROOT}/k3s-journal.txt" 2>&1 || true
    echo "k3s cluster failed; see ${RUN_ROOT}" >&2
    exit 1
  }
  ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'cat /etc/rancher/k3s/k3s.yaml' | sed "s#https://0.0.0.0:6443#https://${SERVER_IP}:6443#g" >"${RUN_ROOT}/kubeconfig.yaml"
fi

ready_count="$(awk '$2=="Ready"{c++} END{print c+0}' "${RUN_ROOT}/nodes.txt")"
cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/stack-life/v1","kind":"KubernetesLifecycleParityLab","status":"succeeded","distro":"${DISTRO}","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"readyCount":${ready_count},"serverIP":"${SERVER_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}"}
EOF
cat "${RUN_ROOT}/receipt.json"
'''


def stack_yaml(distro: str, remote_root: str, subnet: str, suffix: str, node_count: int) -> str:
    bridge = f"tqlife{distro[:2]}{suffix}"
    tap = f"tl{distro[:2]}{suffix}"
    if distro == "kubeadm":
        provider = "kubeadm"
        service = ""
        namespaces = "[kube-system]"
        inspect_roles = "[control-plane]"
        renew_before = "8760h"
        force_once = "stack-life-012-kubeadm"
    else:
        provider = "auto"
        service = "service: k3s"
        namespaces = "[kube-system]"
        inspect_roles = "[control-plane]"
        renew_before = "720h"
        force_once = "stack-life-012-k3s"
    remote_apply = f'''
set -euo pipefail
RUN_ROOT="{remote_root}"
mkdir -p "${{RUN_ROOT}}"
cat >"${{RUN_ROOT}}/cluster-lab.sh" <<'REMOTE'
{common_remote.strip()}
REMOTE
chmod +x "${{RUN_ROOT}}/cluster-lab.sh"
DISTRO="{distro}" NODE_COUNT="{node_count}" SUBNET_OCTET="{subnet}" BRIDGE_NAME="{bridge}" TAP_PREFIX="{tap}" RUN_ROOT="${{RUN_ROOT}}" RUN_ID="stack-life-012-{distro}" "${{RUN_ROOT}}/cluster-lab.sh" apply
'''
    remote_delete = f'''
set +e
RUN_ROOT="{remote_root}"
if [ -x "${{RUN_ROOT}}/cluster-lab.sh" ]; then
  DISTRO="{distro}" NODE_COUNT="{node_count}" SUBNET_OCTET="{subnet}" BRIDGE_NAME="{bridge}" TAP_PREFIX="{tap}" RUN_ROOT="${{RUN_ROOT}}" RUN_ID="stack-life-012-{distro}" "${{RUN_ROOT}}/cluster-lab.sh" delete
fi
'''
    return f'''apiVersion: torque.dev/v1
kind: Stack
name: stack-life-012-{distro}
cli:
  inferDeps: false
runner:
  concurrency: 1
nodes:
  - name: {distro}-bootstrap
    kind: host.command.run
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      timeout: 90m
      command: |
{indent(remote_apply, 8)}
      deleteCommand: |
{indent(remote_delete, 8)}

  - name: {distro}-cluster-inspect
    kind: k8s.cluster.inspect
    needs: [{distro}-bootstrap]
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_SSH
        timeout: 20m
        kubeconfig: {remote_root}/kubeconfig.yaml
        namespaces: {namespaces}

  - name: {distro}-cert-inspect
    kind: k8s.cert.inspect
    needs: [{distro}-cluster-inspect]
    kubernetes:
      provider: {provider}
      certificates:
        renewBefore: {renew_before}
        order: control-plane-first
        batchSize: 1
        healthCheckCommand: |
          set -euo pipefail
          for attempt in $(seq 1 90); do
            if kubectl --kubeconfig {remote_root}/kubeconfig.yaml get --raw=/readyz >/tmp/torque-stack-life-012-{distro}-readyz.out 2>&1; then
              ready="$(kubectl --kubeconfig {remote_root}/kubeconfig.yaml get nodes --no-headers | awk '$2=="Ready"{{c++}} END{{print c+0}}')"
              if [ "${{ready}}" -ge {node_count} ]; then
                printf 'ready=%s\\n' "${{ready}}"
                exit 0
              fi
            fi
            sleep 2
          done
          cat /tmp/torque-stack-life-012-{distro}-readyz.out >&2 || true
          exit 1
        targetsFrom:
          sourceNode: {distro}-cluster-inspect
          roles: {inspect_roles}
          transport: ssh
          targetEnv: TORQUE_LAB_SSH
          timeout: 20m
          {service}
          nodeAddressTemplate: "root@{{{{ .InternalIP }}}}"
          nodeIdentityFile: /opt/firecracker-sandbox-lab/lab_ssh_key

  - name: {distro}-cert-renew
    kind: k8s.cert.renew
    needs: [{distro}-cert-inspect]
    kubernetes:
      provider: {provider}
      certificates:
        renewBefore: {renew_before}
        force: true
        forceOnceId: {force_once}
        statePath: {remote_root}/lifecycle/cert-renewal-state.json
        order: control-plane-first
        batchSize: 1
        healthCheckCommand: |
          set -euo pipefail
          for attempt in $(seq 1 90); do
            if kubectl --kubeconfig {remote_root}/kubeconfig.yaml get --raw=/readyz >/tmp/torque-stack-life-012-{distro}-readyz.out 2>&1; then
              ready="$(kubectl --kubeconfig {remote_root}/kubeconfig.yaml get nodes --no-headers | awk '$2=="Ready"{{c++}} END{{print c+0}}')"
              if [ "${{ready}}" -ge {node_count} ]; then
                printf 'ready=%s\\n' "${{ready}}"
                exit 0
              fi
            fi
            sleep 2
          done
          cat /tmp/torque-stack-life-012-{distro}-readyz.out >&2 || true
          exit 1
        policy:
          maxUnavailable: 1
          requireFreshInspect: true
          maxInspectAge: 15m
          requireHealthyInspect: true
          requireSupportedProvider: true
          maintenanceWindow:
            start: "00:00"
            end: "23:59"
            timeZone: UTC
          appProbes:
            - id: {distro}-before-renew-readyz
              command: |
                set -euo pipefail
                kubectl --kubeconfig {remote_root}/kubeconfig.yaml get --raw=/readyz
              expect: ok
        targetsFrom:
          sourceNode: {distro}-cluster-inspect
          roles: {inspect_roles}
          transport: ssh
          targetEnv: TORQUE_LAB_SSH
          timeout: 30m
          {service}
          nodeAddressTemplate: "root@{{{{ .InternalIP }}}}"
          nodeIdentityFile: /opt/firecracker-sandbox-lab/lab_ssh_key

  - name: {distro}-cluster-verify
    kind: k8s.cluster.verify
    needs: [{distro}-cert-renew]
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_SSH
        timeout: 20m
        kubeconfig: {remote_root}/kubeconfig.yaml
        minReadyNodes: {node_count}
        namespaces: {namespaces}
        nodesCommand: |
          set -euo pipefail
          out=/tmp/torque-stack-life-012-{distro}-verify-nodes.json
          for attempt in $(seq 1 120); do
            if kubectl --kubeconfig {remote_root}/kubeconfig.yaml get nodes -o json >"${{out}}" 2>/tmp/torque-stack-life-012-{distro}-verify-nodes.err; then
              total="$(jq '(.items // []) | length' "${{out}}")"
              ready="$(jq '[.items[]? | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length' "${{out}}")"
              if [ "${{total}}" -ge {node_count} ] && [ "${{ready}}" -ge {node_count} ]; then
                cat "${{out}}"
                exit 0
              fi
              printf 'waiting for Ready nodes: ready=%s total=%s\\n' "${{ready}}" "${{total}}" >&2
            else
              cat /tmp/torque-stack-life-012-{distro}-verify-nodes.err >&2 || true
            fi
            sleep 2
          done
          cat "${{out}}" 2>/dev/null || kubectl --kubeconfig {remote_root}/kubeconfig.yaml get nodes -o json
        podsCommand: |
          set -euo pipefail
          ns="{{{{namespace}}}}"
          out="/tmp/torque-stack-life-012-{distro}-verify-pods-${{ns}}.json"
          for attempt in $(seq 1 120); do
            if kubectl --kubeconfig {remote_root}/kubeconfig.yaml -n "${{ns}}" get pods -o json >"${{out}}" 2>/tmp/torque-stack-life-012-{distro}-verify-pods.err; then
              bad="$(
                jq -r '
                  (.items // [])
                  | map(select(.status.phase != "Succeeded"))
                  | map(select(
                      .status.phase != "Running" or
                      (((.status.containerStatuses // []) | length) > 0 and
                       ((.status.containerStatuses // []) | map(select(.ready == true)) | length) < ((.status.containerStatuses // []) | length))
                    ))
                  | .[]
                  | "\(.metadata.namespace)/\(.metadata.name):\(.status.phase)"
                ' "${{out}}"
              )"
              if [ -z "${{bad}}" ]; then
                cat "${{out}}"
                exit 0
              fi
              printf 'waiting for healthy pods in %s:\\n%s\\n' "${{ns}}" "${{bad}}" >&2
            else
              cat /tmp/torque-stack-life-012-{distro}-verify-pods.err >&2 || true
            fi
            sleep 2
          done
          cat "${{out}}" 2>/dev/null || kubectl --kubeconfig {remote_root}/kubeconfig.yaml -n "${{ns}}" get pods -o json
        apiCommand: |
          set -euo pipefail
          out=/tmp/torque-stack-life-012-{distro}-verify-api.out
          for attempt in $(seq 1 120); do
            if kubectl --kubeconfig {remote_root}/kubeconfig.yaml version --request-timeout=10s >"${{out}}" 2>&1 && \
               kubectl --kubeconfig {remote_root}/kubeconfig.yaml get --raw=/readyz >>"${{out}}" 2>&1; then
              cat "${{out}}"
              exit 0
            fi
            sleep 2
          done
          cat "${{out}}" >&2 || true
          exit 1
        stableIterations: 2
        stableInterval: 5s
        appProbes:
          - id: {distro}-readyz
            command: |
              set -euo pipefail
              kubectl --kubeconfig {remote_root}/kubeconfig.yaml get --raw=/readyz
            expect: ok
'''


for distro, remote_root, subnet, node_count in (
    ("k3s", k3s_remote_root, k3s_subnet, 2),
    ("kubeadm", kubeadm_remote_root, kubeadm_subnet, 2),
):
    root = stack_root / distro
    root.mkdir(parents=True, exist_ok=True)
    (root / "stack.yaml").write_text(stack_yaml(distro, remote_root, subnet, short_suffix, node_count), encoding="utf-8")
PY
}

run_stack() {
  local distro="$1"
  local command="$2"
  local run_id="$3"
  local root="${stack_root}/${distro}"
  ops_log "${distro}: stack ${command} (${run_id})"
  "${torque_bin}" stack "${command}" \
    --config "${root}" \
    --run-id "${run_id}" \
    --yes \
    >"${OPS_RUN_DIR}/stack/${distro}-${command}-${run_id}.jsonl" \
    2>"${OPS_RUN_DIR}/stack/${distro}-${command}-${run_id}.stderr"
}

audit_export_run() {
  local distro="$1"
  local label="$2"
  local run_id="$3"
  local root="${stack_root}/${distro}"
  ops_log "${distro}: audit/export ${label} (${run_id})"
  "${torque_bin}" stack audit \
    --config "${root}" \
    --run-id "${run_id}" \
    --output json \
    --include-artifacts \
    >"${OPS_RUN_DIR}/stack/${distro}-${label}-audit.json"
  "${torque_bin}" stack export \
    --config "${root}" \
    --run-id "${run_id}" \
    --out "${OPS_RUN_DIR}/stack/${distro}-${label}-export.tgz" \
    >"${OPS_RUN_DIR}/stack/${distro}-${label}-export.out"
}

extract_summary() {
  local distro="$1"
  local label="$2"
  local audit_path="${OPS_RUN_DIR}/stack/${distro}-${label}-audit.json"
  local out_path="${OPS_RUN_DIR}/verification/${distro}-${label}-summary-check.json"
  python3 - "${audit_path}" "${out_path}" "${distro}" <<'PY'
import json
import sys
from pathlib import Path

audit_path = Path(sys.argv[1])
out_path = Path(sys.argv[2])
distro = sys.argv[3]
audit = json.loads(audit_path.read_text(encoding="utf-8"))
summary_artifact = None
for artifact in audit.get("artifacts", []):
    if artifact.get("nodeId") == f"k8s.cluster.verify/{distro}-cluster-verify" and artifact.get("name") == "k8s-lifecycle-summary.json":
        summary_artifact = artifact
        break
if not summary_artifact:
    raise SystemExit(f"missing {distro} k8s-lifecycle-summary.json")
summary = json.loads(summary_artifact.get("body") or "{}")
inspect = summary.get("inspect") or {}
policy = summary.get("policy") or {}
policy_inspect = policy.get("inspect") or {}
effective = policy_inspect.get("effectiveCertificateRenewal") or policy_inspect.get("certificateRenewal") or {}
cert = summary.get("certificateRenew") or {}
verify = summary.get("verify") or {}
app_gate = summary.get("applicationGate") or {}
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLife012SummaryCheck",
    "distro": distro,
    "runId": audit.get("runId"),
    "status": summary.get("status"),
    "summaryArtifact": {
        "nodeId": summary_artifact.get("nodeId"),
        "name": summary_artifact.get("name"),
        "sha256": summary_artifact.get("sha256"),
        "sizeBytes": summary_artifact.get("sizeBytes"),
    },
    "sourceArtifactCount": len(summary.get("sourceArtifacts") or []),
    "inspectDistribution": ((inspect.get("provider") or {}).get("distribution")),
    "effectiveRenewalProvider": effective.get("provider"),
    "policyStatus": policy.get("status"),
    "applicationGateStatus": app_gate.get("status"),
    "applicationGateBeforeCount": len(app_gate.get("beforeProbes") or []),
    "applicationGateAfterCount": len(app_gate.get("afterProbes") or []),
    "derivedTargets": ((cert.get("targetsFrom") or {}).get("derivedCount")),
    "certificateRenewStatus": cert.get("status"),
    "certificateRenewMessage": cert.get("message"),
    "targets": [
        {
            "id": target.get("id"),
            "role": target.get("role"),
            "checkpointStatus": target.get("checkpointStatus"),
            "checkpointPhase": target.get("checkpointPhase"),
            "skippedReason": target.get("skippedReason"),
        }
        for target in cert.get("targets") or []
    ],
    "readyNodes": verify.get("readyNodes"),
    "totalNodes": verify.get("totalNodes"),
    "appProbes": [
        {
            "id": probe.get("id"),
            "matched": probe.get("matched"),
            "stdoutDigest": ((probe.get("receipt") or {}).get("stdoutDigest")),
        }
        for probe in verify.get("appProbes") or []
    ],
}
errors = []
if doc["status"] != "succeeded":
    errors.append(f"summary status is {doc['status']!r}")
if doc["policyStatus"] not in ("allowed", "override-approved"):
    errors.append(f"policy status is {doc['policyStatus']!r}")
if doc["applicationGateStatus"] != "passed":
    errors.append(f"application gate status is {doc['applicationGateStatus']!r}")
if doc["applicationGateBeforeCount"] < 1 or doc["applicationGateAfterCount"] < 1:
    errors.append(f"application gate probe counts invalid: before={doc['applicationGateBeforeCount']} after={doc['applicationGateAfterCount']}")
if doc["inspectDistribution"] != distro:
    errors.append(f"expected inspect distribution {distro!r}, got {doc['inspectDistribution']!r}")
if doc["effectiveRenewalProvider"] != distro:
    errors.append(f"expected effective provider {distro!r}, got {doc['effectiveRenewalProvider']!r}")
if distro == "kubeadm" and doc["derivedTargets"] != 1:
    errors.append(f"expected 1 kubeadm control-plane target, got {doc['derivedTargets']}")
if distro == "k3s" and doc["derivedTargets"] != 1:
    errors.append(f"expected 1 k3s control-plane target, got {doc['derivedTargets']}")
if not doc["appProbes"] or not all(item.get("matched") for item in doc["appProbes"]):
    errors.append("verify app probes did not all match")
if not doc["readyNodes"] or doc["readyNodes"] < 1:
    errors.append(f"ready node count invalid: {doc['readyNodes']}")
doc["checkStatus"] = "failed" if errors else "succeeded"
if errors:
    doc["errors"] = errors
out_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
}

write_parity_report() {
  python3 - "${OPS_RUN_DIR}" "${started_at}" "${cleanup_enabled}" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
started_at = sys.argv[2]
cleanup_enabled = sys.argv[3] == "1"
checks = {}
for distro in ("k3s", "kubeadm"):
    create = json.loads((run_dir / "verification" / f"{distro}-create-summary-check.json").read_text(encoding="utf-8"))
    rerun = json.loads((run_dir / "verification" / f"{distro}-rerun-summary-check.json").read_text(encoding="utf-8"))
    checks[distro] = {"create": create, "rerun": rerun}
errors = []
for distro, pair in checks.items():
    if pair["create"].get("checkStatus") != "succeeded":
        errors.append(f"{distro} create summary failed")
    if pair["rerun"].get("checkStatus") != "succeeded":
        errors.append(f"{distro} rerun summary failed")
    if pair["create"].get("inspectDistribution") != distro:
        errors.append(f"{distro} distribution mismatch")
    if pair["create"].get("effectiveRenewalProvider") != distro:
        errors.append(f"{distro} effective provider mismatch")
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLife012ParityReport",
    "status": "failed" if errors else "succeeded",
    "startedAt": started_at,
    "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "cleanupRequested": cleanup_enabled,
    "clusters": checks,
    "errors": errors,
}
(run_dir / "verification" / "parity-report.json").write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
}

write_standard_artifacts() {
  local cleanup_status="$1"
  local cleanup_performed="$2"
  python3 - \
    "${OPS_RUN_DIR}" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${started_at}" \
    "${TORQUE_LAB_SSH}" \
    "${k3s_remote_root}" \
    "${kubeadm_remote_root}" \
    "${cleanup_status}" \
    "${cleanup_performed}" \
    "${OPS_BUNDLE_PATH}" <<'PY'
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
    k3s_remote_root,
    kubeadm_remote_root,
    cleanup_status,
    cleanup_performed,
    bundle_path,
) = sys.argv[1:11]
run = Path(run_dir)
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def load(rel: str) -> dict:
    path = run / rel
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}

parity = load("verification/parity-report.json")

def write(rel: str, doc: dict) -> None:
    path = run / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

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
        {"id": "lab/firecracker-host", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {"id": "cluster/k3s", "type": "kubernetes", "distribution": "k3s", "remoteRoot": k3s_remote_root},
        {"id": "cluster/kubeadm", "type": "kubernetes", "distribution": "kubeadm", "remoteRoot": kubeadm_remote_root},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-kubernetes-and-k3s-lifecycle-parity-lab",
    "status": "succeeded",
    "evidence": {
        "k3sRemoteRoot": k3s_remote_root,
        "kubeadmRemoteRoot": kubeadm_remote_root,
        "parityReport": "verification/parity-report.json",
    },
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLife012Receipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if parity.get("status") == "succeeded" else "failed",
    "parityReport": "verification/parity-report.json",
    "clusters": sorted((parity.get("clusters") or {}).keys()),
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": cleanup_status,
    "cleanupPerformed": cleanup_performed == "true",
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if parity.get("status") == "succeeded" and cleanup_status == "succeeded" else "failed",
    "finishedAt": finished_at,
    "bundle": bundle_path,
})
if parity.get("status") != "succeeded":
    raise SystemExit("parity report failed")
PY
}

write_stacks
cp -R "${stack_root}" "${OPS_RUN_DIR}/stack/generated"

ops_log "build torque"
make -C "${repo_root}" -s build

for distro in k3s kubeadm; do
  ops_log "${distro}: plan"
  "${torque_bin}" stack plan --config "${stack_root}/${distro}" --output json >"${OPS_RUN_DIR}/stack/${distro}-plan.json" 2>"${OPS_RUN_DIR}/stack/${distro}-plan.stderr"
done

for distro in k3s kubeadm; do
  create_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-${distro}-create"
  rerun_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-${distro}-rerun"
  run_stack "${distro}" apply "${create_run_id}"
  audit_export_run "${distro}" create "${create_run_id}"
  extract_summary "${distro}" create
  run_stack "${distro}" apply "${rerun_run_id}"
  audit_export_run "${distro}" rerun "${rerun_run_id}"
  extract_summary "${distro}" rerun
done

write_parity_report

if [[ "${cleanup_enabled}" == "1" ]]; then
  for distro in kubeadm k3s; do
    run_stack "${distro}" delete "${OPS_TASK_ID}-${OPS_RUN_ID}-${distro}-delete" || true
  done
fi

write_standard_artifacts "succeeded" "$([[ "${cleanup_enabled}" == "1" ]] && printf true || printf false)"
ops_scan_for_secret_material "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/redaction-report.json"
ops_write_manifest "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/manifest.json"
ops_export_bundle "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}"
ops_validate_evidence_contract "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}"

ops_log "STACK-LIFE-012 evidence: ${OPS_RUN_DIR}"
ops_log "STACK-LIFE-012 bundle: ${OPS_BUNDLE_PATH}"
