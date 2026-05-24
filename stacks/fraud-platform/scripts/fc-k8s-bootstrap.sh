#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: fc-k8s-bootstrap.
# Keep runtime differences in environment/profile values, not by editing evidence output.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export TORQUE_STACK_ROOT="${TORQUE_STACK_ROOT:-${STACK_DIR}}"
export TORQUE_FRAUD_PROFILE="${TORQUE_FRAUD_PROFILE:-${TORQUE_STACK_PROFILE:-lab}}"
mode="${1:-apply}"
if [[ "${TORQUE_FRAUD_PROFILE}" != "lab" ]]; then
  if [[ "${mode}" == "delete" ]]; then
    echo "skip Firecracker lab teardown for profile=${TORQUE_FRAUD_PROFILE}"
    exit 0
  fi
  KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-${KUBECONFIG:-}}"
  [[ -n "${KUBECONFIG_PATH}" ]] || { echo "set TORQUE_FRAUD_KUBECONFIG or KUBECONFIG for profile=${TORQUE_FRAUD_PROFILE}" >&2; exit 2; }
  kubectl --kubeconfig "${KUBECONFIG_PATH}" get nodes -o wide
  exit 0
fi
LAB_TARGET="${TORQUE_LAB_SSH:-ssh://root@${TORQUE_LAB_PUBLIC_IP:?set TORQUE_LAB_PUBLIC_IP or TORQUE_LAB_SSH}}"
      LAB_TARGET="${LAB_TARGET#ssh://}"
      case "${mode}" in
        apply)
          ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "${LAB_TARGET}" 'bash -s' <<'TORQUE_REMOTE_COMMAND'
set -euo pipefail
RUN_ROOT="/var/lib/torque-firecracker-k8s/fraud-platform"
NODE_COUNT="5"
SUBNET_OCTET="250"
BRIDGE_NAME="tqfcfraud"
TAP_PREFIX="tqfrd"
RUN_ID="fraud-platform"
mkdir -p "${RUN_ROOT}"
cat >"${RUN_ROOT}/fraud-k3s-lab.sh" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

mode="${1:-apply}"
RUN_ROOT="${RUN_ROOT:-/var/lib/torque-firecracker-k8s/fraud-platform}"
NODE_COUNT="${NODE_COUNT:-5}"
SUBNET_OCTET="${SUBNET_OCTET:-250}"
BRIDGE_NAME="${BRIDGE_NAME:-tqfcfraud}"
TAP_PREFIX="${TAP_PREFIX:-tqfrd}"
RUN_ID="${RUN_ID:-fraud-platform}"
BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/kata/share/kata-containers/vmlinux-6.18.28-194}"
K3S_BIN="${K3S_BIN:-/usr/local/bin/k3s}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
CACHE_ROOT="${CACHE_ROOT:-/var/lib/torque-firecracker-k8s/cache}"
ROOTFS_SIZE="${ROOTFS_SIZE:-16G}"
NET_PREFIX="172.31.${SUBNET_OCTET}"
GATEWAY="${NET_PREFIX}.1"
SERVER_IP="${NET_PREFIX}.10"
CIDR="${NET_PREFIX}.0/24"
TOKEN_FILE="${RUN_ROOT}/cluster-token"
PACKAGES="iptables conntrack ipset ethtool socat ca-certificates curl wget tar gzip"
SSH_OPTS=(-n -i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=3)
PUBLIC_RULES=(
  "30080:${SERVER_IP}:30080"
  "3301:${SERVER_IP}:32301"
  "2746:${SERVER_IP}:32746"
  "8081:${SERVER_IP}:32081"
  "8080:${SERVER_IP}:32080"
  "8265:${SERVER_IP}:32665"
  "10001:${SERVER_IP}:32001"
)

node_ip() { printf '%s.%d' "${NET_PREFIX}" "$((10 + $1))"; }
node_name() { printf 'fc-%02d' "$1"; }
node_mem() {
  case "$1" in
    0) echo 3072 ;;
    1) echo 3072 ;;
    *) echo 2048 ;;
  esac
}
public_ip() {
  ip -4 route get 1.1.1.1 | awk '{for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}'
}

remove_public_access() {
  local pub rule public_port target_ip target_port
  pub="$(public_ip)"
  for rule in "${PUBLIC_RULES[@]}"; do
    IFS=: read -r public_port target_ip target_port <<<"${rule}"
    iptables -t nat -D PREROUTING -d "${pub}/32" -p tcp -m tcp --dport "${public_port}" -j DNAT --to-destination "${target_ip}:${target_port}" 2>/dev/null || true
    iptables -D FORWARD -p tcp -d "${target_ip}/32" -m tcp --dport "${target_port}" -j ACCEPT 2>/dev/null || true
  done
}

apply_public_access() {
  local pub rule public_port target_ip target_port
  pub="$(public_ip)"
  remove_public_access
  for rule in "${PUBLIC_RULES[@]}"; do
    IFS=: read -r public_port target_ip target_port <<<"${rule}"
    iptables -t nat -A PREROUTING -d "${pub}/32" -p tcp -m tcp --dport "${public_port}" -j DNAT --to-destination "${target_ip}:${target_port}"
    iptables -A FORWARD -p tcp -d "${target_ip}/32" -m tcp --dport "${target_port}" -j ACCEPT
    echo "public ${public_port} -> ${target_ip}:${target_port}"
  done
}

cleanup_run() {
  local remove_root="${1:-0}"
  set +e
  remove_public_access
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
  if [[ "${remove_root}" == "1" ]]; then
    rm -rf "${RUN_ROOT}"
  else
    rm -rf "${RUN_ROOT}/vms" "${TOKEN_FILE}" "${RUN_ROOT}/receipt.json" "${RUN_ROOT}/nodes.txt" "${RUN_ROOT}/pods.txt" "${RUN_ROOT}/kubeconfig.yaml" "${RUN_ROOT}/server-journal.txt"
    mkdir -p "${RUN_ROOT}"
  fi
  set -e
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 2; }
}

prepare_base_image() {
  local cache_key prepared tmp mnt
  mkdir -p "${CACHE_ROOT}"
  cache_key="$(
    {
      sha256sum "${BASE_ROOTFS}" "${K3S_BIN}" "${KERNEL}"
      printf 'packages=%s\n' "${PACKAGES}"
      printf 'rootfs-size=%s\n' "${ROOTFS_SIZE}"
      printf 'k3s-dns=yes\n'
    } | sha256sum | awk '{print substr($1,1,16)}'
  )"
  prepared="${CACHE_ROOT}/prepared-fraud-${cache_key}.ext4"
  if [[ -s "${prepared}" ]]; then
    echo "${prepared}"
    return
  fi
  tmp="${prepared}.tmp"
  mnt="${CACHE_ROOT}/mnt-fraud-${cache_key}"
  rm -f "${tmp}"
  cp --reflink=auto "${BASE_ROOTFS}" "${tmp}" 2>/dev/null || cp "${BASE_ROOTFS}" "${tmp}"
  set +e
  e2fsck -fy "${tmp}" >/tmp/torque-fraud-e2fsck-${cache_key}.log 2>&1
  local e=$?
  set -e
  [[ "${e}" -le 1 ]] || { cat /tmp/torque-fraud-e2fsck-${cache_key}.log >&2; exit "${e}"; }
  truncate -s "${ROOTFS_SIZE}" "${tmp}"
  resize2fs "${tmp}" >/tmp/torque-fraud-resize-${cache_key}.log 2>&1
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
  mount -t proc proc "${mnt}/proc"
  mount -t sysfs sysfs "${mnt}/sys"
  mount --bind /dev "${mnt}/dev"
  mount --bind /run "${mnt}/run"
  chroot "${mnt}" apt-get update >/tmp/torque-fraud-apt-update-${cache_key}.log 2>&1
  chroot "${mnt}" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ${PACKAGES} >/tmp/torque-fraud-apt-install-${cache_key}.log 2>&1
  install -m 0755 "${K3S_BIN}" "${mnt}/usr/local/bin/k3s"
  chroot "${mnt}" update-alternatives --set iptables /usr/sbin/iptables-legacy >/dev/null 2>&1 || true
  chroot "${mnt}" update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy >/dev/null 2>&1 || true
  chroot "${mnt}" /bin/bash -lc "apt-get clean && rm -rf /var/lib/apt/lists/*"
  cleanup_mounts
  trap - RETURN
  mv "${tmp}" "${prepared}"
  echo "${prepared}"
}

write_hosts_file() {
  cat <<EOF
127.0.0.1 localhost
127.0.1.1 $(node_name "$1")
${NET_PREFIX}.10 fc-00 control-plane
${NET_PREFIX}.11 fc-01 observability
${NET_PREFIX}.12 fc-02 events
${NET_PREFIX}.13 fc-03 processing
${NET_PREFIX}.14 fc-04 mlbatch
EOF
}

write_service() {
  local role="$1"
  local ip="$2"
  if [[ "${role}" == "server" ]]; then
    cat <<EOF
[Unit]
Description=Lightweight Kubernetes
Wants=network-online.target
After=network-online.target
[Service]
Type=simple
Environment=K3S_TOKEN=${TOKEN}
ExecStart=/usr/local/bin/k3s server --cluster-init --node-ip ${ip} --advertise-address ${ip} --bind-address 0.0.0.0 --tls-san ${ip} --tls-san 127.0.0.1 --cluster-cidr 10.250.0.0/16 --service-cidr 10.251.0.0/16 --cluster-dns 10.251.0.10 --flannel-iface eth0 --flannel-backend host-gw --write-kubeconfig-mode 0644 --disable traefik --disable servicelb --disable metrics-server --disable-cloud-controller --disable-network-policy
KillMode=process
Delegate=yes
LimitNOFILE=1048576
LimitNPROC=infinity
LimitCORE=infinity
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
Environment=K3S_TOKEN=${TOKEN}
ExecStart=/usr/local/bin/k3s agent --server https://${SERVER_IP}:6443 --node-ip ${ip} --flannel-iface eth0
KillMode=process
Delegate=yes
LimitNOFILE=1048576
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
Restart=always
RestartSec=5s
TimeoutStartSec=0
[Install]
WantedBy=multi-user.target
EOF
  fi
}

apply_vms() {
  for cmd in cp curl e2fsck mount resize2fs sha256sum ssh sysctl truncate umount openssl; do require_cmd "${cmd}"; done
  for path in "${BASE_ROOTFS}" "${KERNEL}" "${K3S_BIN}" "${FIRECRACKER}" "${LAB_KEY}"; do
    [[ -e "${path}" ]] || { echo "missing ${path}" >&2; exit 2; }
  done
  mkdir -p "${RUN_ROOT}/vms"
  local prepared
  prepared="$(prepare_base_image)"

  if [[ -s "${RUN_ROOT}/receipt.json" && -d "${RUN_ROOT}/vms" ]]; then
    local live_count=0 ready_count=0
    for pid_file in "${RUN_ROOT}"/vms/*/pid; do
      [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null && live_count="$((live_count + 1))"
    done
    nodes_text="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'timeout 20s /usr/local/bin/k3s kubectl get nodes -o wide --no-headers 2>/dev/null' || true)"
    ready_count="$(printf '%s\n' "${nodes_text}" | awk '$2=="Ready"{c++} END{print c+0}')"
    if [[ "${live_count}" -ge "${NODE_COUNT}" && "${ready_count}" -ge "${NODE_COUNT}" ]]; then
      ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'cat /etc/rancher/k3s/k3s.yaml' |
        sed "s#https://0.0.0.0:6443#https://${SERVER_IP}:6443#g" >"${RUN_ROOT}/kubeconfig.yaml"
      printf '%s\n' "${nodes_text}" >"${RUN_ROOT}/nodes.txt"
      cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-k8s/v1","kind":"FraudPlatformK3sReceipt","status":"succeeded","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"readyCount":${ready_count},"serverIP":"${SERVER_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}","idempotentReuse":true}
EOF
      echo "fraud-platform-already-ready nodes=${ready_count} live=${live_count}"
      cat "${RUN_ROOT}/nodes.txt"
      return
    fi
  fi

  cleanup_run
  mkdir -p "${RUN_ROOT}/vms"
  openssl rand -hex 24 >"${TOKEN_FILE}"
  chmod 0600 "${TOKEN_FILE}"
  TOKEN="$(cat "${TOKEN_FILE}")"

  ip link add name "${BRIDGE_NAME}" type bridge
  ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}"
  ip link set "${BRIDGE_NAME}" up
  sysctl -w net.ipv4.ip_forward=1 >/dev/null
  iptables -t nat -C POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE 2>/dev/null ||
    iptables -t nat -A POSTROUTING -s "${CIDR}" ! -o "${BRIDGE_NAME}" -j MASQUERADE

  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    local vm ip name tap mac mnt role mem service_name
    vm="${RUN_ROOT}/vms/node${i}"
    ip="$(node_ip "${i}")"
    name="$(node_name "${i}")"
    tap="${TAP_PREFIX}${i}"
    role="agent"
    service_name="k3s-agent.service"
    [[ "${i}" == "0" ]] && role="server" && service_name="k3s.service"
    mem="$(node_mem "${i}")"
    mac="$(printf '06:00:00:%02x:02:%02x' "${SUBNET_OCTET}" "$((10 + i))")"
    mkdir -p "${vm}"
    cp --reflink=auto "${prepared}" "${vm}/rootfs.ext4" 2>/dev/null || cp "${prepared}" "${vm}/rootfs.ext4"
    e2fsck -fy "${vm}/rootfs.ext4" >/dev/null 2>&1 || true
    mnt="${vm}/mnt"
    mkdir -p "${mnt}"
    mount -o loop "${vm}/rootfs.ext4" "${mnt}"
    printf '%s\n' "${name}" >"${mnt}/etc/hostname"
    write_hosts_file "${i}" >"${mnt}/etc/hosts"
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
    mkdir -p "${mnt}/etc/systemd/system/multi-user.target.wants"
    ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
    write_service "${role}" "${ip}" >"${mnt}/etc/systemd/system/${service_name}"
    ln -sf "/etc/systemd/system/${service_name}" "${mnt}/etc/systemd/system/multi-user.target.wants/${service_name}"
    umount "${mnt}"
    ip tuntap add dev "${tap}" mode tap
    ip link set "${tap}" master "${BRIDGE_NAME}"
    ip link set "${tap}" up
    cat >"${vm}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=1 systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":2,"mem_size_mib":${mem}},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${tap}","guest_mac":"${mac}"}],"logger":{"log_path":"${vm}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
    "${FIRECRACKER}" --api-sock "${vm}/fc.sock" --config-file "${vm}/vm.json" >"${vm}/console.log" 2>&1 &
    echo $! >"${vm}/pid"
    echo "started ${name} ${ip} mem=${mem}"
  done

  local ssh_count=0
  for i in $(seq 0 "$((NODE_COUNT - 1))"); do
    for _ in $(seq 1 180); do
      if ssh "${SSH_OPTS[@]}" "root@$(node_ip "${i}")" true >/dev/null 2>&1; then
        ssh_count="$((ssh_count + 1))"
        break
      fi
      sleep 2
    done
  done
  [[ "${ssh_count}" -eq "${NODE_COUNT}" ]] || { echo "only ${ssh_count}/${NODE_COUNT} VMs reachable" >&2; exit 1; }

  local ready_count=0 nodes_text=""
  for attempt in $(seq 1 180); do
    nodes_text="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'timeout 20s /usr/local/bin/k3s kubectl get nodes -o wide --no-headers 2>/dev/null' || true)"
    ready_count="$(printf '%s\n' "${nodes_text}" | awk '$2=="Ready"{c++} END{print c+0}')"
    [[ "${ready_count}" -ge "${NODE_COUNT}" ]] && break
    if (( attempt % 10 == 0 )); then
      echo "waiting-k3s nodes=${ready_count}/${NODE_COUNT}" >&2
      printf '%s\n' "${nodes_text}" >&2
    fi
    sleep 3
  done
  printf '%s\n' "${nodes_text}" >"${RUN_ROOT}/nodes.txt"
  ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'cat /etc/rancher/k3s/k3s.yaml' |
    sed "s#https://0.0.0.0:6443#https://${SERVER_IP}:6443#g" >"${RUN_ROOT}/kubeconfig.yaml"
  ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'timeout 20s /usr/local/bin/k3s kubectl get pods -A -o wide || true' >"${RUN_ROOT}/pods.txt" 2>&1 || true
  cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-k8s/v1","kind":"FraudPlatformK3sReceipt","status":"succeeded","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"readyCount":${ready_count},"serverIP":"${SERVER_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}"}
EOF
  [[ "${ready_count}" -ge "${NODE_COUNT}" ]] || { echo "only ${ready_count}/${NODE_COUNT} k3s nodes ready" >&2; exit 1; }
  cat "${RUN_ROOT}/nodes.txt"
}

case "${mode}" in
  apply) apply_vms ;;
  public-apply) apply_public_access ;;
  public-delete) remove_public_access ;;
  delete|cleanup) cleanup_run 1 ;;
  *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
esac
REMOTE
chmod +x "${RUN_ROOT}/fraud-k3s-lab.sh"
RUN_ROOT="${RUN_ROOT}" NODE_COUNT="${NODE_COUNT}" SUBNET_OCTET="${SUBNET_OCTET}" BRIDGE_NAME="${BRIDGE_NAME}" TAP_PREFIX="${TAP_PREFIX}" RUN_ID="${RUN_ID}" \
  "${RUN_ROOT}/fraud-k3s-lab.sh" apply
TORQUE_REMOTE_COMMAND
          ;;
        delete)
          ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "${LAB_TARGET}" 'bash -s' <<'TORQUE_REMOTE_DELETE'
set +e
RUN_ROOT="/var/lib/torque-firecracker-k8s/fraud-platform"
if [ -x "${RUN_ROOT}/fraud-k3s-lab.sh" ]; then
  RUN_ROOT="${RUN_ROOT}" "${RUN_ROOT}/fraud-k3s-lab.sh" delete
else
  for p in "${RUN_ROOT}"/vms/*/pid; do [ -f "$p" ] && kill "$(cat "$p")" 2>/dev/null; done
  for i in $(seq 0 4); do ip link del "tqfrd${i}" 2>/dev/null; done
  ip link set tqfcfraud down 2>/dev/null
  ip link del tqfcfraud type bridge 2>/dev/null
  iptables -t nat -D POSTROUTING -s "172.31.250.0/24" ! -o tqfcfraud -j MASQUERADE 2>/dev/null
  rm -rf "${RUN_ROOT}"
fi
TORQUE_REMOTE_DELETE
          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
