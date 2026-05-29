#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-apply}"

RUN_ROOT="${RUN_ROOT:-/var/lib/kubeadm-firecracker-ha}"
CACHE_ROOT="${CACHE_ROOT:-/var/cache/kubeadm-firecracker-ha}"
CONTROL_PLANE_COUNT="${CONTROL_PLANE_COUNT:-3}"
WORKER_COUNT="${WORKER_COUNT:-0}"
NODE_COUNT="$((CONTROL_PLANE_COUNT + WORKER_COUNT))"

SUBNET_PREFIX="${SUBNET_PREFIX:-198.19.0}"
BRIDGE_NAME="${BRIDGE_NAME:-k8sha198}"
TAP_PREFIX="${TAP_PREFIX:-k8sha198}"
FIRECRACKER_BIN="${FIRECRACKER_BIN:-/usr/local/bin/firecracker}"
FIRECRACKER_ARCH="${FIRECRACKER_ARCH:-$(uname -m)}"
FIRECRACKER_CI_VERSION="${FIRECRACKER_CI_VERSION:-}"
KERNEL_PATH="${KERNEL_PATH:-}"
BASE_ROOTFS_PATH="${BASE_ROOTFS_PATH:-}"
ROOTFS_SQUASHFS_PATH="${ROOTFS_SQUASHFS_PATH:-}"
ROOTFS_SIZE_GIB="${ROOTFS_SIZE_GIB:-12}"

GUEST_SSH_KEY="${GUEST_SSH_KEY:-${CACHE_ROOT}/lab_ssh_key}"
GUEST_SSH_PUB="${GUEST_SSH_KEY}.pub"
SSH_OPTS=(-i "${GUEST_SSH_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ConnectTimeout=5)

KUBERNETES_MINOR="${KUBERNETES_MINOR:-v1.35}"
POD_CIDR="${POD_CIDR:-10.244.0.0/16}"
SERVICE_CIDR="${SERVICE_CIDR:-10.96.0.0/12}"
CNI_PLUGINS_VERSION="${CNI_PLUGINS_VERSION:-v1.3.0}"
NETWORK_PLUGIN="${NETWORK_PLUGIN:-cilium}"
CILIUM_VERSION="${CILIUM_VERSION:-v1.19.4}"
CILIUM_CLI_VERSION="${CILIUM_CLI_VERSION:-}"
CILIUM_CONNECTIVITY_TEST="${CILIUM_CONNECTIVITY_TEST:-0}"
FLANNEL_MANIFEST_URL="${FLANNEL_MANIFEST_URL:-https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml}"

HAPROXY_IMAGE="${HAPROXY_IMAGE:-haproxy:3.2.19-alpine}"
API_LB_IP="${API_LB_IP:-${SUBNET_PREFIX}.5}"
API_LB_PORT="${API_LB_PORT:-6443}"
API_LB_ENDPOINT="${API_LB_IP}:${API_LB_PORT}"
API_LB_CONTAINER_NAME="${API_LB_CONTAINER_NAME:-kubeadm-ha-api-lb-${BRIDGE_NAME}}"

CONTROL_PLANE_MEM_MIB="${CONTROL_PLANE_MEM_MIB:-2048}"
WORKER_MEM_MIB="${WORKER_MEM_MIB:-1536}"
VCPU_COUNT="${VCPU_COUNT:-2}"

GATEWAY="${SUBNET_PREFIX}.1"
PRIMARY_CONTROL_PLANE_IP="${SUBNET_PREFIX}.10"
CIDR="${SUBNET_PREFIX}.0/24"
ARTIFACT_PREFIX="/var/log/kubeadm-ha-lab"
APPLY_ACTIVE="0"

if [[ -z "${KERNEL_PATH}" && -f /opt/firecracker-sandbox-lab/vmlinux.bin ]]; then
  KERNEL_PATH="/opt/firecracker-sandbox-lab/vmlinux.bin"
fi
if [[ -z "${BASE_ROOTFS_PATH}" && -f /opt/firecracker-sandbox-lab/rootfs.ext4 ]]; then
  BASE_ROOTFS_PATH="/opt/firecracker-sandbox-lab/rootfs.ext4"
fi

if [[ "${MODE}" != "apply" && "${MODE}" != "delete" && "${MODE}" != "status" ]]; then
  echo "usage: $0 [apply|delete|status]" >&2
  exit 2
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "run this script on the Linux lab host" >&2
  exit 2
fi

if [[ "${EUID}" -ne 0 ]]; then
  echo "run this script as root" >&2
  exit 2
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

validate_int() {
  local name="$1"
  local value="$2"
  [[ "${value}" =~ ^[0-9]+$ ]] || {
    echo "${name} must be a non-negative integer, got: ${value}" >&2
    exit 2
  }
}

validate_config() {
  validate_int CONTROL_PLANE_COUNT "${CONTROL_PLANE_COUNT}"
  validate_int WORKER_COUNT "${WORKER_COUNT}"
  validate_int NODE_COUNT "${NODE_COUNT}"
  validate_int ROOTFS_SIZE_GIB "${ROOTFS_SIZE_GIB}"
  validate_int CONTROL_PLANE_MEM_MIB "${CONTROL_PLANE_MEM_MIB}"
  validate_int WORKER_MEM_MIB "${WORKER_MEM_MIB}"
  validate_int VCPU_COUNT "${VCPU_COUNT}"

  if (( CONTROL_PLANE_COUNT != 3 )); then
    echo "CONTROL_PLANE_COUNT must be 3 for this stacked-etcd lab" >&2
    exit 2
  fi
  if (( WORKER_COUNT < 0 )); then
    echo "WORKER_COUNT must be zero or greater" >&2
    exit 2
  fi
  if (( NODE_COUNT < CONTROL_PLANE_COUNT )); then
    echo "NODE_COUNT must be at least CONTROL_PLANE_COUNT" >&2
    exit 2
  fi
  case "${NETWORK_PLUGIN}" in
    cilium|flannel)
      ;;
    *)
      echo "NETWORK_PLUGIN must be either cilium or flannel" >&2
      exit 2
      ;;
  esac
  case "${CILIUM_CONNECTIVITY_TEST}" in
    0|1)
      ;;
    *)
      echo "CILIUM_CONNECTIVITY_TEST must be 0 or 1" >&2
      exit 2
      ;;
  esac
}

node_role() {
  if (( $1 < CONTROL_PLANE_COUNT )); then
    echo "control-plane"
  else
    echo "worker"
  fi
}

node_ip() {
  printf '%s.%d' "${SUBNET_PREFIX}" "$((10 + $1))"
}

node_name() {
  printf 'k8s-%02d' "$1"
}

node_tap() {
  printf '%s%d' "${TAP_PREFIX}" "$1"
}

node_mac() {
  local idx="$1"
  printf '06:36:00:00:00:%02x' "$((16 + idx))"
}

node_mem() {
  if [[ "$(node_role "$1")" == "control-plane" ]]; then
    echo "${CONTROL_PLANE_MEM_MIB}"
  else
    echo "${WORKER_MEM_MIB}"
  fi
}

cni_arch() {
  case "${FIRECRACKER_ARCH}" in
    x86_64) echo "amd64" ;;
    aarch64) echo "arm64" ;;
    *)
      echo "unsupported CNI architecture: ${FIRECRACKER_ARCH}" >&2
      exit 2
      ;;
  esac
}

ensure_guest_ssh_key() {
  if [[ -f "${GUEST_SSH_KEY}" && -f "${GUEST_SSH_PUB}" ]]; then
    return
  fi
  mkdir -p "$(dirname "${GUEST_SSH_KEY}")"
  rm -f "${GUEST_SSH_KEY}" "${GUEST_SSH_PUB}"
  ssh-keygen -q -t ed25519 -N "" -f "${GUEST_SSH_KEY}" >/dev/null
}

detect_firecracker_ci_version() {
  local version
  version="$("${FIRECRACKER_BIN}" --version 2>/dev/null | awk '{print $2}')"
  [[ -n "${version}" ]] || {
    echo "unable to determine Firecracker version from ${FIRECRACKER_BIN}" >&2
    exit 2
  }
  printf '%s\n' "${version%.*}"
}

download_firecracker_assets() {
  local ci_version
  local kernel_key
  local ubuntu_key
  local download_dir="${CACHE_ROOT}/downloads"

  if [[ -n "${KERNEL_PATH}" ]]; then
    [[ -f "${KERNEL_PATH}" ]] || {
      echo "missing kernel path: ${KERNEL_PATH}" >&2
      exit 2
    }
  fi
  if [[ -n "${BASE_ROOTFS_PATH}" ]]; then
    [[ -f "${BASE_ROOTFS_PATH}" ]] || {
      echo "missing base rootfs path: ${BASE_ROOTFS_PATH}" >&2
      exit 2
    }
  fi
  if [[ -n "${ROOTFS_SQUASHFS_PATH}" ]]; then
    [[ -f "${ROOTFS_SQUASHFS_PATH}" ]] || {
      echo "missing rootfs squashfs path: ${ROOTFS_SQUASHFS_PATH}" >&2
      exit 2
    }
  fi
  if [[ -n "${KERNEL_PATH}" && ( -n "${BASE_ROOTFS_PATH}" || -n "${ROOTFS_SQUASHFS_PATH}" ) ]]; then
    return
  fi

  mkdir -p "${download_dir}"

  ci_version="${FIRECRACKER_CI_VERSION:-$(detect_firecracker_ci_version)}"

  if [[ -z "${KERNEL_PATH}" ]]; then
    kernel_key="$(
      curl -fsSL "https://s3.amazonaws.com/spec.ccfc.min?prefix=firecracker-ci/${ci_version}/${FIRECRACKER_ARCH}/vmlinux-&list-type=2" |
        grep -oP "(?<=<Key>)(firecracker-ci/${ci_version}/${FIRECRACKER_ARCH}/vmlinux-[0-9]+\.[0-9]+\.[0-9]{1,3})(?=</Key>)" |
        sort -V | tail -1
    )"
    [[ -n "${kernel_key}" ]] || {
      echo "unable to resolve Firecracker kernel asset for ${ci_version}/${FIRECRACKER_ARCH}" >&2
      exit 2
    }
    KERNEL_PATH="${download_dir}/$(basename "${kernel_key}")"
    if [[ ! -f "${KERNEL_PATH}" ]]; then
      curl -fsSL "https://s3.amazonaws.com/spec.ccfc.min/${kernel_key}" -o "${KERNEL_PATH}"
    fi
  fi

  if [[ -z "${BASE_ROOTFS_PATH}" && -z "${ROOTFS_SQUASHFS_PATH}" ]]; then
    ubuntu_key="$(
      curl -fsSL "https://s3.amazonaws.com/spec.ccfc.min?prefix=firecracker-ci/${ci_version}/${FIRECRACKER_ARCH}/ubuntu-&list-type=2" |
        grep -oP "(?<=<Key>)(firecracker-ci/${ci_version}/${FIRECRACKER_ARCH}/ubuntu-[0-9]+\.[0-9]+\.squashfs)(?=</Key>)" |
        sort -V | tail -1
    )"
    [[ -n "${ubuntu_key}" ]] || {
      echo "unable to resolve Firecracker Ubuntu rootfs asset for ${ci_version}/${FIRECRACKER_ARCH}" >&2
      exit 2
    }
    ROOTFS_SQUASHFS_PATH="${download_dir}/$(basename "${ubuntu_key}")"
    if [[ ! -f "${ROOTFS_SQUASHFS_PATH}" ]]; then
      curl -fsSL "https://s3.amazonaws.com/spec.ccfc.min/${ubuntu_key}" -o "${ROOTFS_SQUASHFS_PATH}"
    fi
  fi
}

ensure_base_rootfs() {
  if [[ -n "${BASE_ROOTFS_PATH}" ]]; then
    [[ -f "${BASE_ROOTFS_PATH}" ]] || {
      echo "missing base rootfs path: ${BASE_ROOTFS_PATH}" >&2
      exit 2
    }
    return
  fi

  local base_key
  local base_dir="${CACHE_ROOT}/base"
  local tmp_dir
  local tmp_img
  mkdir -p "${base_dir}"
  base_key="$(
    {
      sha256sum "${ROOTFS_SQUASHFS_PATH}"
      sha256sum "${GUEST_SSH_PUB}"
    } | sha256sum | awk '{print substr($1,1,16)}'
  )"
  BASE_ROOTFS_PATH="${base_dir}/ubuntu-${base_key}.ext4"
  if [[ -s "${BASE_ROOTFS_PATH}" ]]; then
    return
  fi

  tmp_dir="${base_dir}/ubuntu-${base_key}.rootfs"
  tmp_img="${BASE_ROOTFS_PATH}.tmp"
  rm -rf "${tmp_dir}"
  rm -f "${tmp_img}"
  mkdir -p "${tmp_dir}"

  unsquashfs -d "${tmp_dir}/rootfs" "${ROOTFS_SQUASHFS_PATH}" >/dev/null
  mkdir -p "${tmp_dir}/rootfs/root/.ssh"
  chmod 700 "${tmp_dir}/rootfs/root/.ssh"
  cp "${GUEST_SSH_PUB}" "${tmp_dir}/rootfs/root/.ssh/authorized_keys"
  chmod 600 "${tmp_dir}/rootfs/root/.ssh/authorized_keys"
  if [[ -f "${tmp_dir}/rootfs/etc/ssh/sshd_config" ]]; then
    sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' "${tmp_dir}/rootfs/etc/ssh/sshd_config" || true
    sed -i 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' "${tmp_dir}/rootfs/etc/ssh/sshd_config" || true
    sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' "${tmp_dir}/rootfs/etc/ssh/sshd_config" || true
  fi
  chown -R root:root "${tmp_dir}/rootfs"

  truncate -s 4G "${tmp_img}"
  mkfs.ext4 -d "${tmp_dir}/rootfs" -F "${tmp_img}" >/dev/null
  mv "${tmp_img}" "${BASE_ROOTFS_PATH}"
  rm -rf "${tmp_dir}"
}

prepare_base_image() {
  local key
  local prepared
  local tmp
  local mnt
  local cni_arch_value
  local pause_image
  local prepare_failed="0"

  cni_arch_value="$(cni_arch)"
  key="$(
    {
      sha256sum "${BASE_ROOTFS_PATH}" "${KERNEL_PATH}" "${GUEST_SSH_PUB}"
      printf 'kubernetes_minor=%s\n' "${KUBERNETES_MINOR}"
      printf 'cni_plugins=%s\n' "${CNI_PLUGINS_VERSION}"
      printf 'rootfs_size_gib=%s\n' "${ROOTFS_SIZE_GIB}"
      printf 'generation=kubeadm-firecracker-ha-v1\n'
    } | sha256sum | awk '{print substr($1,1,16)}'
  )"
  prepared="${CACHE_ROOT}/prepared-${key}.ext4"
  if [[ -s "${prepared}" ]]; then
    PREPARED_ROOTFS_PATH="${prepared}"
    return
  fi

  tmp="${prepared}.tmp"
  mnt="${CACHE_ROOT}/mnt-${key}"
  rm -f "${tmp}"
  cp --reflink=auto "${BASE_ROOTFS_PATH}" "${tmp}" 2>/dev/null || cp "${BASE_ROOTFS_PATH}" "${tmp}"
  set +e
  e2fsck -fy "${tmp}" >"${CACHE_ROOT}/e2fsck-${key}.log" 2>&1
  local fsck_code=$?
  set -e
  [[ "${fsck_code}" -le 1 ]] || {
    cat "${CACHE_ROOT}/e2fsck-${key}.log" >&2
    return "${fsck_code}"
  }
  truncate -s "${ROOTFS_SIZE_GIB}G" "${tmp}"
  resize2fs "${tmp}" >"${CACHE_ROOT}/resize-${key}.log" 2>&1

  mkdir -p "${mnt}"
  mount -o loop "${tmp}" "${mnt}"
  cleanup_mounts() {
    set +e
    if mountpoint -q "${mnt}/dev/pts"; then
      umount "${mnt}/dev/pts" 2>/dev/null || umount -l "${mnt}/dev/pts" 2>/dev/null || true
    fi
    if mountpoint -q "${mnt}/proc"; then
      umount "${mnt}/proc" 2>/dev/null || umount -l "${mnt}/proc" 2>/dev/null || true
    fi
    if mountpoint -q "${mnt}/sys"; then
      umount "${mnt}/sys" 2>/dev/null || umount -l "${mnt}/sys" 2>/dev/null || true
    fi
    if mountpoint -q "${mnt}/dev"; then
      umount "${mnt}/dev" 2>/dev/null || umount -l "${mnt}/dev" 2>/dev/null || true
    fi
    if mountpoint -q "${mnt}/run"; then
      umount "${mnt}/run" 2>/dev/null || umount -l "${mnt}/run" 2>/dev/null || true
    fi
    if mountpoint -q "${mnt}"; then
      umount "${mnt}" 2>/dev/null || umount -l "${mnt}" 2>/dev/null || true
    fi
    if [[ "${prepare_failed}" == "1" ]]; then
      rm -f "${tmp}"
    fi
  }
  fail_prepare() {
    prepare_failed="1"
    cleanup_mounts
    rm -f "${tmp}"
  }
  cleanup_mounts_return() {
    local status="$?"
    cleanup_mounts
    return "${status}"
  }
  trap cleanup_mounts_return RETURN
  trap fail_prepare ERR

  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions timeout:2 attempts:3\n' >"${mnt}/etc/resolv.conf"
  chmod 1777 "${mnt}/tmp"
  mkdir -p "${mnt}/dev/pts" "${mnt}/var/cache/apt/archives/partial" "${mnt}/var/lib/apt/lists/partial" "${mnt}/var/log/apt"
  touch "${mnt}/var/log/dpkg.log"
  mkdir -p "${mnt}/root/.ssh"
  chmod 700 "${mnt}/root/.ssh"
  cp "${GUEST_SSH_PUB}" "${mnt}/root/.ssh/authorized_keys"
  chmod 600 "${mnt}/root/.ssh/authorized_keys"
  chown -R root:root "${mnt}/root/.ssh"
  if [[ -f "${mnt}/etc/ssh/sshd_config" ]]; then
    sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin yes/' "${mnt}/etc/ssh/sshd_config" || true
    sed -i 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' "${mnt}/etc/ssh/sshd_config" || true
    sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' "${mnt}/etc/ssh/sshd_config" || true
  fi
  mount -t proc proc "${mnt}/proc"
  mount -t sysfs sysfs "${mnt}/sys"
  mount --bind /dev "${mnt}/dev"
  mount -t devpts devpts "${mnt}/dev/pts"
  mount --bind /run "${mnt}/run"

  if ! chroot "${mnt}" /usr/bin/apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 update >"${CACHE_ROOT}/apt-update-${key}.log" 2>&1; then
    cat "${CACHE_ROOT}/apt-update-${key}.log" >&2
    prepare_failed="1"
    return 1
  fi
  if ! DEBIAN_FRONTEND=noninteractive chroot "${mnt}" /usr/bin/apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 \
    -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install -y --no-install-recommends \
    apt-transport-https ca-certificates conntrack containerd curl ebtables etcd-client ethtool gpg iptables ipset jq openssh-server socat tar xz-utils >"${CACHE_ROOT}/apt-install-base-${key}.log" 2>&1; then
    cat "${CACHE_ROOT}/apt-install-base-${key}.log" >&2
    prepare_failed="1"
    return 1
  fi

  mkdir -p "${mnt}/etc/apt/keyrings"
  mkdir -p "${mnt}/etc/apt/sources.list.d"
  if ! chroot "${mnt}" /usr/bin/curl -fsSL "https://pkgs.k8s.io/core:/stable:/${KUBERNETES_MINOR}/deb/Release.key" |
    chroot "${mnt}" /usr/bin/gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg; then
    echo "failed to install Kubernetes apt keyring inside guest rootfs" >&2
    prepare_failed="1"
    return 1
  fi
  printf 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/%s/deb/ /\n' "${KUBERNETES_MINOR}" >"${mnt}/etc/apt/sources.list.d/kubernetes.list"
  if ! chroot "${mnt}" /usr/bin/apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 update >"${CACHE_ROOT}/apt-update-kubernetes-${key}.log" 2>&1; then
    cat "${CACHE_ROOT}/apt-update-kubernetes-${key}.log" >&2
    prepare_failed="1"
    return 1
  fi
  if ! DEBIAN_FRONTEND=noninteractive chroot "${mnt}" /usr/bin/apt-get -o Acquire::Retries=5 -o Acquire::http::Timeout=20 -o Acquire::https::Timeout=20 \
    -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold install -y kubelet kubeadm kubectl >"${CACHE_ROOT}/apt-install-kubernetes-${key}.log" 2>&1; then
    cat "${CACHE_ROOT}/apt-install-kubernetes-${key}.log" >&2
    prepare_failed="1"
    return 1
  fi
  chroot "${mnt}" /usr/bin/apt-mark hold kubelet kubeadm kubectl >/dev/null 2>&1 || true

  mkdir -p "${mnt}/opt/cni/bin"
  if ! chroot "${mnt}" /usr/bin/curl -fsSL "https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/cni-plugins-linux-${cni_arch_value}-${CNI_PLUGINS_VERSION}.tgz" |
    chroot "${mnt}" /usr/bin/tar -C /opt/cni/bin -xz; then
    echo "failed to install CNI plugins inside guest rootfs" >&2
    prepare_failed="1"
    return 1
  fi

  pause_image="$(chroot "${mnt}" /usr/bin/kubeadm config images list 2>/dev/null | awk '/pause/{print $1; exit}')"
  mkdir -p "${mnt}/etc/containerd"
  if ! chroot "${mnt}" /usr/bin/containerd config default >"${mnt}/etc/containerd/config.toml"; then
    echo "failed to render containerd config inside guest rootfs" >&2
    prepare_failed="1"
    return 1
  fi
  sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' "${mnt}/etc/containerd/config.toml"
  if [[ -n "${pause_image}" ]]; then
    sed -i "s#sandbox_image = \".*\"#sandbox_image = \"${pause_image}\"#" "${mnt}/etc/containerd/config.toml"
  fi

  prepare_failed="0"
  mkdir -p "${mnt}/etc/modules-load.d" "${mnt}/etc/sysctl.d" "${mnt}/etc/cloud"
  cat >"${mnt}/etc/modules-load.d/kubernetes.conf" <<EOF
overlay
br_netfilter
EOF
  cat >"${mnt}/etc/sysctl.d/99-kubernetes.conf" <<EOF
net.bridge.bridge-nf-call-iptables=1
net.bridge.bridge-nf-call-ip6tables=1
net.ipv4.ip_forward=1
EOF
  touch "${mnt}/etc/cloud/cloud-init.disabled" 2>/dev/null || true

  chroot "${mnt}" /usr/bin/update-alternatives --set iptables /usr/sbin/iptables-legacy >/dev/null 2>&1 || true
  chroot "${mnt}" /usr/bin/update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy >/dev/null 2>&1 || true
  mkdir -p "${mnt}/etc/systemd/system/multi-user.target.wants"
  ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
  ln -sf /lib/systemd/system/systemd-networkd.service "${mnt}/etc/systemd/system/multi-user.target.wants/systemd-networkd.service"

  cleanup_mounts
  trap - RETURN ERR
  mv "${tmp}" "${prepared}"
  PREPARED_ROOTFS_PATH="${prepared}"
}

configure_vm() {
  local idx="$1"
  local vm_dir="$2"
  local ip
  local name
  local mnt
  cleanup_vm_mount() {
    set +e
    if mountpoint -q "${mnt}"; then
      umount "${mnt}" 2>/dev/null || umount -l "${mnt}" 2>/dev/null || true
    fi
  }
  cleanup_vm_mount_return() {
    local status="$?"
    cleanup_vm_mount
    return "${status}"
  }
  ip="$(node_ip "${idx}")"
  name="$(node_name "${idx}")"

  cp --reflink=auto "${PREPARED_ROOTFS_PATH}" "${vm_dir}/rootfs.ext4" 2>/dev/null || cp "${PREPARED_ROOTFS_PATH}" "${vm_dir}/rootfs.ext4"
  e2fsck -fy "${vm_dir}/rootfs.ext4" >/dev/null 2>&1 || true
  mnt="${vm_dir}/mnt"
  mkdir -p "${mnt}"
  mount -o loop "${vm_dir}/rootfs.ext4" "${mnt}"
  trap cleanup_vm_mount_return RETURN

  printf '%s\n' "${name}" >"${mnt}/etc/hostname"
  {
    printf '127.0.0.1 localhost\n'
    printf '127.0.1.1 %s\n' "${name}"
    printf '%s api-lb\n' "${API_LB_IP}"
    for j in $(seq 0 "$((NODE_COUNT - 1))"); do
      printf '%s %s\n' "$(node_ip "${j}")" "$(node_name "${j}")"
    done
  } >"${mnt}/etc/hosts"

  mkdir -p "${mnt}/etc/systemd/network" "${mnt}/etc/systemd/system/multi-user.target.wants" "${mnt}/etc/cloud"
  cat >"${mnt}/etc/systemd/network/20-eth0.network" <<EOF
[Match]
Name=eth0

[Network]
Address=${ip}/24
Gateway=${GATEWAY}
DNS=1.1.1.1
DNS=8.8.8.8
EOF

  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  rm -f "${mnt}/etc/machine-id" "${mnt}/var/lib/dbus/machine-id" 2>/dev/null || true
  touch "${mnt}/etc/machine-id"
  rm -rf "${mnt}/etc/kubernetes" "${mnt}/var/lib/etcd" "${mnt}/var/lib/cni" "${mnt}/var/lib/kubelet" "${mnt}/var/lib/containerd" "${mnt}/etc/cni/net.d"
  mkdir -p "${mnt}/var/lib/containerd" "${mnt}/etc/cni/net.d"
  touch "${mnt}/etc/cloud/cloud-init.disabled" 2>/dev/null || true
  ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
  ln -sf /lib/systemd/system/systemd-networkd.service "${mnt}/etc/systemd/system/multi-user.target.wants/systemd-networkd.service"

  cleanup_vm_mount
  trap - RETURN
}

boot_node() {
  local idx="$1"
  local vm_dir="${RUN_ROOT}/nodes/node${idx}"
  local tap
  local mac
  local mem
  tap="$(node_tap "${idx}")"
  mac="$(node_mac "${idx}")"
  mem="$(node_mem "${idx}")"

  mkdir -p "${vm_dir}"
  configure_vm "${idx}" "${vm_dir}"
  ip tuntap add dev "${tap}" mode tap 2>/dev/null || true
  ip link set "${tap}" master "${BRIDGE_NAME}"
  ip link set "${tap}" up

  cat >"${vm_dir}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL_PATH}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm_dir}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":${VCPU_COUNT},"mem_size_mib":${mem}},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${tap}","guest_mac":"${mac}"}],"logger":{"log_path":"${vm_dir}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
  "${FIRECRACKER_BIN}" --api-sock "${vm_dir}/fc.sock" --config-file "${vm_dir}/vm.json" >"${vm_dir}/console.log" 2>&1 &
  echo $! >"${vm_dir}/pid"
}

setup_bridge() {
  ip link add name "${BRIDGE_NAME}" type bridge 2>/dev/null || true
  ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}" 2>/dev/null || true
  ip addr add "${API_LB_IP}/24" dev "${BRIDGE_NAME}" 2>/dev/null || true
  ip link set "${BRIDGE_NAME}" up
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
  if ! iptables -C FORWARD -i "${BRIDGE_NAME}" -j ACCEPT 2>/dev/null; then
    iptables -A FORWARD -i "${BRIDGE_NAME}" -j ACCEPT
  fi
  if ! iptables -C FORWARD -o "${BRIDGE_NAME}" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null; then
    iptables -A FORWARD -o "${BRIDGE_NAME}" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
  fi
  if ! iptables -t nat -C POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null; then
    iptables -t nat -A POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE
  fi
}

setup_api_lb() {
  local cfg="${RUN_ROOT}/haproxy.cfg"
  mkdir -p "${RUN_ROOT}"
  docker rm -f "${API_LB_CONTAINER_NAME}" >/dev/null 2>&1 || true
  {
    printf 'global\n'
    printf '  maxconn 2048\n'
    printf 'defaults\n'
    printf '  mode tcp\n'
    printf '  timeout connect 5s\n'
    printf '  timeout client 60s\n'
    printf '  timeout server 60s\n'
    printf 'frontend kube_api\n'
    printf '  bind %s:%s\n' "${API_LB_IP}" "${API_LB_PORT}"
    printf '  default_backend kube_apis\n'
    printf 'backend kube_apis\n'
    printf '  balance roundrobin\n'
    printf '  option tcp-check\n'
    printf '  default-server inter 2s fall 3 rise 2\n'
    for i in $(seq 0 "$((CONTROL_PLANE_COUNT - 1))"); do
      printf '  server cp%d %s:6443 check\n' "${i}" "$(node_ip "${i}")"
    done
  } >"${cfg}"
  docker run -d --name "${API_LB_CONTAINER_NAME}" --network host -v "${cfg}:/usr/local/etc/haproxy/haproxy.cfg:ro" "${HAPROXY_IMAGE}" >/dev/null
}

wait_for_ssh() {
  local ip="$1"
  for _ in $(seq 1 120); do
    if ssh "${SSH_OPTS[@]}" "root@${ip}" true >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

node_ssh() {
  local idx="$1"
  shift
  ssh "${SSH_OPTS[@]}" "root@$(node_ip "${idx}")" "$@"
}

server_ssh() {
  node_ssh 0 "$@"
}

copy_guest_file() {
  local remote_path="$1"
  local local_path="$2"
  if server_ssh "test -f $(printf '%q' "${remote_path}")" >/dev/null 2>&1; then
    server_ssh "cat $(printf '%q' "${remote_path}")" >"${local_path}"
  fi
}

prepare_node_runtime() {
  local idx="$1"
  node_ssh "${idx}" "cat >/root/prepare-node.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
swapoff -a || true
sed -i '/ swap / s/^/#/' /etc/fstab 2>/dev/null || true
modprobe overlay || true
modprobe br_netfilter || true
sysctl --system >/dev/null
systemctl daemon-reload || true
systemctl enable --now ssh containerd kubelet >/dev/null 2>&1 || true
systemctl restart containerd
kubeadm reset -f >/dev/null 2>&1 || true
rm -rf /etc/kubernetes /var/lib/etcd /var/lib/cni /var/lib/kubelet/* /etc/cni/net.d/*
mkdir -p /etc/cni/net.d
kubeadm config images pull --kubernetes-version="$(kubeadm version -o short)" >/dev/null 2>&1 || true
EOF
  node_ssh "${idx}" "chmod +x /root/prepare-node.sh && /root/prepare-node.sh"
}

wait_for_flannel() {
  local flannel_ns=""
  for _ in $(seq 1 180); do
    flannel_ns="$(server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf get daemonset -A --no-headers 2>/dev/null | awk '\$2==\"kube-flannel-ds\"{print \$1; exit}'" || true)"
    if [[ -n "${flannel_ns}" ]]; then
      if server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf -n ${flannel_ns} rollout status daemonset/kube-flannel-ds --timeout=5s" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 2
  done
  return 1
}

install_network_plugin() {
  if [[ "${NETWORK_PLUGIN}" == "flannel" ]]; then
    server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf apply -f $(printf '%q' "${FLANNEL_MANIFEST_URL}")"
    wait_for_flannel
    return 0
  fi

  server_ssh "cat >/root/install-network-plugin.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

export KUBECONFIG=/etc/kubernetes/admin.conf
network_plugin="${NETWORK_PLUGIN:?}"
artifact_prefix="${ARTIFACT_PREFIX:?}"

case "${network_plugin}" in
  cilium)
    cli_version="${CILIUM_CLI_VERSION:-}"
    case "$(uname -m)" in
      x86_64) cli_arch=amd64 ;;
      aarch64) cli_arch=arm64 ;;
      *)
        echo "unsupported Cilium CLI architecture: $(uname -m)" >&2
        exit 1
        ;;
    esac
    if [[ -z "${cli_version}" ]]; then
      cli_version="$(curl -fsSL https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)"
    fi
    cache_dir="/var/cache/cilium-cli/${cli_version}"
    archive="${cache_dir}/cilium-linux-${cli_arch}.tar.gz"
    sumfile="${archive}.sha256sum"
    mkdir -p "${cache_dir}"
    if [[ ! -f "${archive}" || ! -f "${sumfile}" ]]; then
      curl -L --fail "https://github.com/cilium/cilium-cli/releases/download/${cli_version}/cilium-linux-${cli_arch}.tar.gz" -o "${archive}"
      curl -L --fail "https://github.com/cilium/cilium-cli/releases/download/${cli_version}/cilium-linux-${cli_arch}.tar.gz.sha256sum" -o "${sumfile}"
    fi
    (
      cd "${cache_dir}"
      sha256sum --check "$(basename "${sumfile}")"
    )
    tar xzvfC "${archive}" /usr/local/bin >/dev/null
    cilium install \
      --version "${CILIUM_VERSION}" \
      --wait \
      --wait-duration 10m \
      --set ipam.mode=kubernetes \
      --set k8s.requireIPv4PodCIDR=true \
      --set kubeProxyReplacement=false \
      --set k8sServiceHost="${API_LB_IP}" \
      --set k8sServicePort="${API_LB_PORT}"
    cilium status --wait --wait-duration 10m --interactive=false > "${artifact_prefix}.cilium-status" 2>&1
    kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get pods -l k8s-app=cilium -o wide > "${artifact_prefix}.cilium-pods" 2>&1 || true
    ;;
  *)
    echo "unsupported network plugin: ${network_plugin}" >&2
    exit 1
    ;;
esac
EOF
  server_ssh "chmod +x /root/install-network-plugin.sh && NETWORK_PLUGIN=$(printf '%q' "${NETWORK_PLUGIN}") CILIUM_VERSION=$(printf '%q' "${CILIUM_VERSION}") CILIUM_CLI_VERSION=$(printf '%q' "${CILIUM_CLI_VERSION}") API_LB_IP=$(printf '%q' "${API_LB_IP}") API_LB_PORT=$(printf '%q' "${API_LB_PORT}") ARTIFACT_PREFIX=$(printf '%q' "${ARTIFACT_PREFIX}") /root/install-network-plugin.sh" >"${RUN_ROOT}/network-plugin-install.log" 2>&1
}

init_primary_control_plane() {
  server_ssh "cat >/root/init-primary.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
version="\$(kubeadm version -o short)"
kubeadm init \
  --kubernetes-version "\${version}" \
  --control-plane-endpoint "${API_LB_ENDPOINT}" \
  --apiserver-advertise-address "${PRIMARY_CONTROL_PLANE_IP}" \
  --pod-network-cidr "${POD_CIDR}" \
  --service-cidr "${SERVICE_CIDR}" \
  --upload-certs \
  --ignore-preflight-errors=all
mkdir -p /root/.kube
cp /etc/kubernetes/admin.conf /root/.kube/config
certificate_key="\$(kubeadm init phase upload-certs --upload-certs 2>/dev/null | tail -n 1)"
join_cmd="\$(kubeadm token create --print-join-command)"
printf '%s\n' "\${certificate_key}" >/root/certificate-key.txt
printf '%s\n' "\${join_cmd}" >/root/join-command.txt
EOF
  server_ssh "chmod +x /root/init-primary.sh && /root/init-primary.sh" >"${RUN_ROOT}/kubeadm-init.log" 2>&1
  install_network_plugin
}

wait_for_node_ready() {
  local name="$1"
  for _ in $(seq 1 240); do
    if server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf get node ${name} --no-headers 2>/dev/null | awk '\$2==\"Ready\"{ok=1} END{exit(ok?0:1)}'"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

join_additional_control_planes() {
  local join_cmd
  local certificate_key
  join_cmd="$(server_ssh "cat /root/join-command.txt")"
  certificate_key="$(server_ssh "cat /root/certificate-key.txt")"
  [[ -n "${join_cmd}" && -n "${certificate_key}" ]] || {
    echo "failed to retrieve kubeadm join material from primary control plane" >&2
    exit 1
  }

  for i in $(seq 1 "$((CONTROL_PLANE_COUNT - 1))"); do
    local ip
    local name
    ip="$(node_ip "${i}")"
    name="$(node_name "${i}")"
    node_ssh "${i}" "cat >/root/join-control-plane.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
${join_cmd} --control-plane --certificate-key ${certificate_key} --apiserver-advertise-address ${ip} --ignore-preflight-errors=all
EOF
    node_ssh "${i}" "chmod +x /root/join-control-plane.sh && /root/join-control-plane.sh" >"${RUN_ROOT}/kubeadm-join-control-plane-${i}.log" 2>&1
    wait_for_node_ready "${name}"
  done
}

join_workers() {
  if (( WORKER_COUNT == 0 )); then
    return 0
  fi
  local join_cmd
  join_cmd="$(server_ssh "cat /root/join-command.txt")"
  for i in $(seq "${CONTROL_PLANE_COUNT}" "$((NODE_COUNT - 1))"); do
    local name
    name="$(node_name "${i}")"
    node_ssh "${i}" "cat >/root/join-worker.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
${join_cmd} --ignore-preflight-errors=all
EOF
    node_ssh "${i}" "chmod +x /root/join-worker.sh && /root/join-worker.sh" >"${RUN_ROOT}/kubeadm-join-worker-${i}.log" 2>&1
    wait_for_node_ready "${name}"
  done
}

snapshot_cluster_status() {
  server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes -o wide > ${ARTIFACT_PREFIX}.nodes 2>&1 || true
kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes --no-headers > ${ARTIFACT_PREFIX}.nodes-plain 2>&1 || true
kubectl --kubeconfig /etc/kubernetes/admin.conf get pods -A -o wide > ${ARTIFACT_PREFIX}.pods 2>&1 || true
kubectl --kubeconfig /etc/kubernetes/admin.conf get svc -A -o wide > ${ARTIFACT_PREFIX}.services 2>&1 || true
kubectl --kubeconfig /etc/kubernetes/admin.conf get endpoints kubernetes -o wide > ${ARTIFACT_PREFIX}.apiserver-endpoints 2>&1 || true" >/dev/null 2>&1 || true
}

wait_for_cluster() {
  local ready_count="0"
  local control_plane_ready="0"
  local endpoint_count="0"
  for _ in $(seq 1 240); do
    snapshot_cluster_status
    ready_count="$(server_ssh "awk '\$2==\"Ready\"{c++} END{print c+0}' ${ARTIFACT_PREFIX}.nodes-plain 2>/dev/null || echo 0")"
    control_plane_ready="$(server_ssh "awk '\$2==\"Ready\" && \$3 ~ /control-plane/ {c++} END{print c+0}' ${ARTIFACT_PREFIX}.nodes-plain 2>/dev/null || echo 0")"
    endpoint_count="$(server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf get endpoints kubernetes -o jsonpath='{range .subsets[*].addresses[*]}{.ip}{\"\\n\"}{end}' 2>/dev/null | awk 'NF{c++} END{print c+0}' || echo 0")"
    if [[ "${ready_count}" -ge "${NODE_COUNT}" && "${control_plane_ready}" -ge "${CONTROL_PLANE_COUNT}" && "${endpoint_count}" -ge "${CONTROL_PLANE_COUNT}" ]]; then
      return 0
    fi
    sleep 3
  done
  return 1
}

rebalance_coredns() {
  server_ssh "kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system rollout restart deployment coredns >/dev/null 2>&1 || true
kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system rollout status deployment/coredns --timeout=240s" >/dev/null 2>&1 || true
}

capture_etcd_members() {
  server_ssh "ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt \
  --key=/etc/kubernetes/pki/etcd/healthcheck-client.key \
  member list -w table > ${ARTIFACT_PREFIX}.etcd-members 2>&1
ETCDCTL_API=3 etcdctl \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt \
  --key=/etc/kubernetes/pki/etcd/healthcheck-client.key \
  endpoint status -w table > ${ARTIFACT_PREFIX}.etcd-endpoints 2>&1" >/dev/null 2>&1 || true
}

run_smoke() {
  server_ssh "cat >/root/run-smoke.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${NETWORK_PLUGIN:-}" == "cilium" ]]; then
  cilium status --wait --wait-duration 10m --interactive=false >/var/log/kubeadm-ha-lab.cilium-status 2>&1
  if [[ "${CILIUM_CONNECTIVITY_TEST:-0}" == "1" ]]; then
    connectivity_args=()
    if [[ "${WORKER_COUNT:-0}" == "0" ]]; then
      connectivity_args+=(--single-node)
    fi
    cilium connectivity test "${connectivity_args[@]}" >/var/log/kubeadm-ha-lab.cilium-connectivity.log 2>&1
  fi
fi
kubectl --kubeconfig /etc/kubernetes/admin.conf create namespace smoke --dry-run=client -o yaml | kubectl --kubeconfig /etc/kubernetes/admin.conf apply -f -
cat <<'YAML' | kubectl --kubeconfig /etc/kubernetes/admin.conf apply -f -
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-smoke
  namespace: smoke
spec:
  selector:
    matchLabels:
      app: node-smoke
  template:
    metadata:
      labels:
        app: node-smoke
    spec:
      tolerations:
        - operator: Exists
      containers:
        - name: node-smoke
          image: busybox:1.36
          command: ["sh", "-lc", "sleep 3600"]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echo
  namespace: smoke
spec:
  replicas: 2
  selector:
    matchLabels:
      app: echo
  template:
    metadata:
      labels:
        app: echo
    spec:
      tolerations:
        - operator: Exists
      containers:
        - name: echo
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: echo
  namespace: smoke
spec:
  selector:
    app: echo
  ports:
    - port: 80
      targetPort: 80
YAML
kubectl --kubeconfig /etc/kubernetes/admin.conf -n smoke rollout status daemonset/node-smoke --timeout=240s
kubectl --kubeconfig /etc/kubernetes/admin.conf -n smoke rollout status deployment/echo --timeout=240s
kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system rollout status deployment/coredns --timeout=240s
cat <<'YAML' | kubectl --kubeconfig /etc/kubernetes/admin.conf apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: dns-http
  namespace: smoke
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      tolerations:
        - operator: Exists
      containers:
        - name: dns-http
          image: busybox:1.36
          command:
            - sh
            - -lc
            - |
              for _ in $(seq 1 30); do
                nslookup echo.smoke.svc.cluster.local && \
                wget -qO- http://echo.smoke.svc.cluster.local >/tmp/echo.html && \
                test -s /tmp/echo.html && exit 0
                sleep 2
              done
              exit 1
YAML
kubectl --kubeconfig /etc/kubernetes/admin.conf -n smoke wait --for=condition=complete job/dns-http --timeout=240s
kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes -o wide > /var/log/kubeadm-ha-lab.nodes 2>&1
kubectl --kubeconfig /etc/kubernetes/admin.conf get pods -A -o wide > /var/log/kubeadm-ha-lab.pods 2>&1
kubectl --kubeconfig /etc/kubernetes/admin.conf get svc -A -o wide > /var/log/kubeadm-ha-lab.services 2>&1
kubectl --kubeconfig /etc/kubernetes/admin.conf -n smoke get pods -o wide > /var/log/kubeadm-ha-lab.smoke-pods 2>&1
kubectl --kubeconfig /etc/kubernetes/admin.conf -n smoke get svc echo -o wide > /var/log/kubeadm-ha-lab.smoke-service 2>&1
kubectl --kubeconfig /etc/kubernetes/admin.conf -n smoke logs job/dns-http > /var/log/kubeadm-ha-lab.smoke-job.log 2>&1
kubectl --kubeconfig /etc/kubernetes/admin.conf config view --raw > /var/log/kubeadm-ha-lab.kubeconfig 2>&1
EOF
  server_ssh "chmod +x /root/run-smoke.sh && NETWORK_PLUGIN=$(printf '%q' "${NETWORK_PLUGIN}") CILIUM_CONNECTIVITY_TEST=$(printf '%q' "${CILIUM_CONNECTIVITY_TEST}") WORKER_COUNT=$(printf '%q' "${WORKER_COUNT}") /root/run-smoke.sh"
}

verify_api_lb() {
  for _ in $(seq 1 60); do
    if curl -ksSf --max-time 5 "https://${API_LB_ENDPOINT}/version" >"${RUN_ROOT}/api-lb-version.json" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

collect_diagnostics() {
  mkdir -p "${RUN_ROOT}/artifacts"
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    local dir="${RUN_ROOT}/nodes/node${i}"
    local node_artifacts="${RUN_ROOT}/artifacts/node${i}"
    local ip
    ip="$(node_ip "${i}")"
    mkdir -p "${node_artifacts}"
    [[ -f "${dir}/console.log" ]] && cp "${dir}/console.log" "${node_artifacts}/console.log"
    [[ -f "${dir}/firecracker.log" ]] && cp "${dir}/firecracker.log" "${node_artifacts}/firecracker.log"
    if ssh "${SSH_OPTS[@]}" "root@${ip}" true >/dev/null 2>&1; then
      ssh "${SSH_OPTS[@]}" "root@${ip}" "uname -a" >"${node_artifacts}/uname.txt" 2>&1 || true
      ssh "${SSH_OPTS[@]}" "root@${ip}" "ip addr show" >"${node_artifacts}/ip-addr.txt" 2>&1 || true
      ssh "${SSH_OPTS[@]}" "root@${ip}" "ip route show" >"${node_artifacts}/ip-route.txt" 2>&1 || true
      ssh "${SSH_OPTS[@]}" "root@${ip}" "journalctl -u containerd -u kubelet --no-pager -n 200 || true" >"${node_artifacts}/services.log" 2>&1 || true
      ssh "${SSH_OPTS[@]}" "root@${ip}" "test -f /etc/kubernetes/admin.conf && kubectl --kubeconfig /etc/kubernetes/admin.conf get pods -A -o wide || true" >"${node_artifacts}/pods.txt" 2>&1 || true
    fi
  done
}

collect_artifacts() {
  mkdir -p "${RUN_ROOT}/artifacts"
  copy_guest_file "${ARTIFACT_PREFIX}.nodes" "${RUN_ROOT}/artifacts/nodes.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.pods" "${RUN_ROOT}/artifacts/pods.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.services" "${RUN_ROOT}/artifacts/services.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.apiserver-endpoints" "${RUN_ROOT}/artifacts/apiserver-endpoints.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.etcd-members" "${RUN_ROOT}/artifacts/etcd-members.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.etcd-endpoints" "${RUN_ROOT}/artifacts/etcd-endpoints.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.smoke-pods" "${RUN_ROOT}/artifacts/smoke-pods.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.smoke-service" "${RUN_ROOT}/artifacts/smoke-service.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.smoke-job.log" "${RUN_ROOT}/artifacts/smoke-job.log"
  copy_guest_file "${ARTIFACT_PREFIX}.cilium-status" "${RUN_ROOT}/artifacts/cilium-status.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.cilium-pods" "${RUN_ROOT}/artifacts/cilium-pods.txt"
  copy_guest_file "${ARTIFACT_PREFIX}.cilium-connectivity.log" "${RUN_ROOT}/artifacts/cilium-connectivity.log"
  copy_guest_file "${ARTIFACT_PREFIX}.kubeconfig" "${RUN_ROOT}/artifacts/kubeconfig.yaml"
  [[ -f "${RUN_ROOT}/api-lb-version.json" ]] && cp "${RUN_ROOT}/api-lb-version.json" "${RUN_ROOT}/artifacts/api-lb-version.json"
  collect_diagnostics
  cat >"${RUN_ROOT}/artifacts/receipt.json" <<EOF
{"cluster":"kubeadm-firecracker-ha","status":"succeeded","apiLbEndpoint":"${API_LB_ENDPOINT}","primaryControlPlaneIP":"${PRIMARY_CONTROL_PLANE_IP}","cidr":"${CIDR}","controlPlaneCount":${CONTROL_PLANE_COUNT},"workerCount":${WORKER_COUNT},"nodeCount":${NODE_COUNT},"kubernetesMinor":"${KUBERNETES_MINOR}","podCIDR":"${POD_CIDR}","serviceCIDR":"${SERVICE_CIDR}","networkPlugin":"${NETWORK_PLUGIN}","ciliumVersion":"${CILIUM_VERSION}","flannelManifestURL":"${FLANNEL_MANIFEST_URL}"}
EOF
}

cleanup_run() {
  set +e
  docker rm -f "${API_LB_CONTAINER_NAME}" >/dev/null 2>&1 || true
  for pid_file in "${RUN_ROOT}"/nodes/*/pid; do
    [[ -f "${pid_file}" ]] || continue
    kill "$(cat "${pid_file}")" 2>/dev/null || true
  done
  pkill -f "${RUN_ROOT}/nodes/.*/fc.sock" >/dev/null 2>&1 || true
  sleep 1
  for pid_file in "${RUN_ROOT}"/nodes/*/pid; do
    [[ -f "${pid_file}" ]] || continue
    kill -9 "$(cat "${pid_file}")" 2>/dev/null || true
  done
  pkill -9 -f "${RUN_ROOT}/nodes/.*/fc.sock" >/dev/null 2>&1 || true
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    ip link del "$(node_tap "${i}")" 2>/dev/null || true
  done
  iptables -D FORWARD -i "${BRIDGE_NAME}" -j ACCEPT 2>/dev/null || true
  iptables -D FORWARD -o "${BRIDGE_NAME}" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
  iptables -t nat -D POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null || true
  ip link set "${BRIDGE_NAME}" down 2>/dev/null || true
  ip link del "${BRIDGE_NAME}" type bridge 2>/dev/null || true
  rm -rf "${RUN_ROOT}"
  set -e
}

status_cluster() {
  if [[ -f "${RUN_ROOT}/artifacts/receipt.json" ]]; then
    cat "${RUN_ROOT}/artifacts/receipt.json"
    echo
    [[ -f "${RUN_ROOT}/artifacts/nodes.txt" ]] && cat "${RUN_ROOT}/artifacts/nodes.txt"
    exit 0
  fi
  echo "no cluster artifacts at ${RUN_ROOT}" >&2
  exit 1
}

on_exit() {
  local status="$?"
  trap - EXIT
  if [[ "${MODE}" == "apply" && "${APPLY_ACTIVE}" == "1" && "${status}" -ne 0 ]]; then
    echo "apply failed; collecting diagnostics under ${RUN_ROOT}/artifacts" >&2
    collect_diagnostics || true
  fi
  exit "${status}"
}

trap on_exit EXIT

apply_cluster() {
  validate_config
  require_cmd awk
  require_cmd chroot
  require_cmd curl
  require_cmd docker
  require_cmd e2fsck
  require_cmd grep
  require_cmd ip
  require_cmd iptables
  require_cmd mkfs.ext4
  require_cmd mount
  require_cmd resize2fs
  require_cmd sha256sum
  require_cmd sort
  require_cmd ssh
  require_cmd ssh-keygen
  require_cmd truncate
  require_cmd umount
  require_cmd unsquashfs
  [[ -x "${FIRECRACKER_BIN}" ]] || {
    echo "missing Firecracker binary at ${FIRECRACKER_BIN}" >&2
    exit 2
  }

  APPLY_ACTIVE="1"
  mkdir -p "${RUN_ROOT}" "${CACHE_ROOT}"
  cleanup_run
  ensure_guest_ssh_key
  download_firecracker_assets || exit 1
  ensure_base_rootfs || exit 1
  prepare_base_image || exit 1
  setup_bridge

  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    boot_node "${i}"
  done
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    wait_for_ssh "$(node_ip "${i}")"
  done
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    prepare_node_runtime "${i}"
  done

  setup_api_lb
  init_primary_control_plane
  join_additional_control_planes
  join_workers
  rebalance_coredns
  wait_for_cluster
  capture_etcd_members
  run_smoke
  verify_api_lb
  collect_artifacts
  APPLY_ACTIVE="0"

  cat "${RUN_ROOT}/artifacts/receipt.json"
  echo
  cat "${RUN_ROOT}/artifacts/nodes.txt"
  echo
  cat "${RUN_ROOT}/artifacts/etcd-members.txt"
  echo
  cat "${RUN_ROOT}/artifacts/smoke-job.log"
}

case "${MODE}" in
  apply)
    apply_cluster
    ;;
  delete)
    cleanup_run
    ;;
  status)
    status_cluster
    ;;
esac
