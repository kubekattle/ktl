#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-FC-K8S-001.sh [options]

Options:
  --nodes N            Firecracker node count. Defaults to 8; allowed 8-12.
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Delete lab resources after the run. Default.
  --no-cleanup         Leave Firecracker VMs and stack app for debugging.
  -h, --help           Show this help.

STACK-FC-K8S-001 proves a stack-driven Firecracker Kubernetes lab on the SSH
host. The generated stack bootstraps 8-12 Firecracker VMs, forms a k3s cluster,
opens a local SSH tunnel, installs a Helm HTTP DaemonSet app with release.helm,
reapplies the stack for idempotence, verifies node-local HTTP access, and
records stack/audit/export evidence.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

node_count=8
cleanup_enabled=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --nodes)
      [[ $# -ge 2 ]] || ops_fail "--nodes requires a value"
      node_count="$2"
      shift 2
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

[[ "${node_count}" =~ ^[0-9]+$ ]] || ops_fail "--nodes must be an integer"
if (( node_count < 8 || node_count > 12 )); then
  ops_fail "--nodes must be between 8 and 12"
fi
[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Firecracker/Kubernetes E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd base64
ops_require_cmd go
ops_require_cmd kubectl
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "STACK-FC-K8S-001"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-fc-k8s.XXXXXX")"
stack_root="${scratch_root}/stack"
safe_suffix="$(printf '%s' "${OPS_RUN_ID}" | tr '[:upper:]_' '[:lower:]-' | tr -cd 'a-z0-9-' | cut -c1-10)"
short_suffix="$(printf '%s' "${safe_suffix}" | cut -c1-5)"
cksum_value="$(printf '%s' "${OPS_RUN_ID}" | cksum | awk '{print $1}')"
subnet_octet="$((200 + (cksum_value % 40)))"
remote_root="/var/lib/torque-firecracker-k8s/runs/${OPS_RUN_ID}"
server_ip="172.31.${subnet_octet}.10"
tunnel_port="$(
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
apply_run_id=""
reapply_run_id=""
audit_run_id=""
stack_applied=0

fallback_remote_cleanup() {
  [[ "${cleanup_enabled}" == "1" ]] || return 0
  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "set +e
if [ -x '${remote_root}/bootstrap-firecracker-k3s.sh' ]; then
  RUN_ROOT='${remote_root}' NODE_COUNT='${node_count}' SUBNET_OCTET='${subnet_octet}' BRIDGE_NAME='tqfc${short_suffix}' TAP_PREFIX='tq${short_suffix}' RUN_ID='${OPS_RUN_ID}' '${remote_root}/bootstrap-firecracker-k3s.sh' delete
fi
for p in '${remote_root}'/vms/*/pid; do [ -f \"\$p\" ] && kill \"\$(cat \"\$p\")\" 2>/dev/null; done
for i in \$(seq 0 $((node_count - 1))); do ip link del 'tq${short_suffix}'\$i 2>/dev/null; done
ip link set 'tqfc${short_suffix}' down 2>/dev/null
ip link del 'tqfc${short_suffix}' type bridge 2>/dev/null
iptables -t nat -D POSTROUTING -s '172.31.${subnet_octet}.0/24' ! -o 'tqfc${short_suffix}' -j MASQUERADE 2>/dev/null
rm -rf '${remote_root}'
true" >"${OPS_RUN_DIR}/remote/fallback-cleanup.out" 2>"${OPS_RUN_DIR}/remote/fallback-cleanup.stderr" || true
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
    "${node_count}" \
    "${TORQUE_LAB_SSH}" \
    "${remote_root}" \
    "${server_ip}" \
    "172.31.${subnet_octet}.0/24" \
    "${apply_run_id}" \
    "${reapply_run_id}" \
    "${audit_run_id}" \
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
    node_count,
    lab_ssh,
    remote_root,
    server_ip,
    subnet,
    apply_run_id,
    reapply_run_id,
    audit_run_id,
    cleanup_status,
    cleanup_performed,
    exit_code,
) = sys.argv[1:16]
run = Path(run_dir)
expected = int(node_count)
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

verification = load("verification/receipt.json")
summary = load("verification/summary.json")
remote = load("remote/receipt.json")
overall_ok = code == 0 and cleanup_status == "succeeded"
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

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
    "nodeCount": expected,
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "lab/firecracker-host", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {"id": "cluster/firecracker-k3s", "type": "kubernetes", "nodeCount": expected, "serverIP": server_ip, "subnet": subnet},
        {"id": "app/torque-fc-app", "type": "helm-release", "namespace": "torque-fc-app", "access": "hostNetwork:18080"},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-stack-driven-firecracker-k8s-change",
    "status": "succeeded" if overall_ok else "blocked",
    "evidence": {
        "applyRunId": apply_run_id,
        "reapplyRunId": reapply_run_id,
        "auditRunId": audit_run_id,
        "readyNodes": summary.get("readyNodes", verification.get("readyNodes")),
        "accessibleHTTPNodes": summary.get("accessibleHTTPNodes"),
        "remoteRoot": remote_root,
    },
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if cleanup_status == "succeeded" else "failed",
    "cleanupPerformed": cleanup_performed == "true",
    "stackDeleteLog": "stack/delete.jsonl",
    "remoteRoot": remote_root,
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if overall_ok else "failed",
    "finishedAt": finished_at,
    "applyRunId": apply_run_id,
    "reapplyRunId": reapply_run_id,
    "auditRunId": audit_run_id,
    "expectedNodes": expected,
    "readyNodes": summary.get("readyNodes", verification.get("readyNodes")),
    "availableDaemonSetPods": summary.get("availableDaemonSetPods", verification.get("availableDaemonSetPods")),
    "accessibleHTTPNodes": summary.get("accessibleHTTPNodes"),
    "serverIP": remote.get("serverIP", server_ip),
    "remoteSubnet": remote.get("subnet", subnet),
    "cleanupStatus": cleanup_status,
})
PY
}

finish() {
  local code=$?
  local cleanup_status="succeeded"
  local cleanup_performed="false"
  trap - EXIT
  if [[ "${cleanup_enabled}" == "1" && -d "${stack_root}" ]]; then
    ops_log "cleanup stack resources"
    cleanup_performed="true"
    (
      cd "${repo_root}"
      ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
    ) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr" || {
      fallback_remote_cleanup
      cleanup_status="failed"
      code=1
    }
  fi
  rm -f "${OPS_RUN_DIR}/stack/firecracker-kubeconfig.yaml"
  write_standard_artifacts "${code}" "${cleanup_status}" "${cleanup_performed}" || code=1
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/verification" "${OPS_RUN_DIR}/remote"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create Firecracker stack fixture"
python3 - \
  "${stack_root}" \
  "${OPS_RUN_DIR}" \
  "${OPS_RUN_ID}" \
  "${node_count}" \
  "${remote_root}" \
  "${safe_suffix}" \
  "${short_suffix}" \
  "${subnet_octet}" \
  "${server_ip}" \
  "${tunnel_port}" <<'PY'
import base64
import shlex
import sys
import textwrap
from pathlib import Path

stack_root = Path(sys.argv[1])
run_dir = Path(sys.argv[2])
run_id = sys.argv[3]
node_count = int(sys.argv[4])
remote_root = sys.argv[5]
safe_suffix = sys.argv[6]
short_suffix = sys.argv[7]
subnet_octet = int(sys.argv[8])
server_ip = sys.argv[9]
tunnel_port = int(sys.argv[10])

chart = stack_root / "charts" / "fc-app"
(chart / "templates").mkdir(parents=True, exist_ok=True)
(chart / "Chart.yaml").write_text("apiVersion: v2\nname: torque-fc-app\nversion: 0.1.0\n", encoding="utf-8")
(chart / "values.yaml").write_text("image: hashicorp/http-echo:1.0.0\nport: 18080\nrunId: unknown\n", encoding="utf-8")
(chart / "templates" / "configmap.yaml").write_text(
    """apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}
data:
  runId: {{ .Values.runId | quote }}
  proof: firecracker-k3s-stack
""",
    encoding="utf-8",
)
(chart / "templates" / "daemonset.yaml").write_text(
    """apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ .Release.Name }}
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Release.Name }}
    spec:
      hostNetwork: true
      dnsPolicy: Default
      tolerations:
        - operator: Exists
      containers:
        - name: http
          image: {{ .Values.image | quote }}
          imagePullPolicy: IfNotPresent
          args:
            - "-listen=:{{ .Values.port }}"
            - "-text=torque-firecracker-k8s {{ .Values.runId }}"
          ports:
            - name: http
              containerPort: {{ .Values.port }}
              protocol: TCP
          readinessProbe:
            httpGet:
              path: /
              port: http
            initialDelaySeconds: 1
            periodSeconds: 2
""",
    encoding="utf-8",
)

remote_script = r'''#!/usr/bin/env bash
set -euo pipefail

mode="${1:-apply}"
: "${RUN_ROOT:?missing RUN_ROOT}"
: "${NODE_COUNT:?missing NODE_COUNT}"
: "${SUBNET_OCTET:?missing SUBNET_OCTET}"
: "${BRIDGE_NAME:?missing BRIDGE_NAME}"
: "${TAP_PREFIX:?missing TAP_PREFIX}"
: "${RUN_ID:?missing RUN_ID}"

BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/firecracker-sandbox-lab/vmlinux.bin}"
K3S_BIN="${K3S_BIN:-/usr/local/bin/k3s}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
CACHE_ROOT="${CACHE_ROOT:-/var/lib/torque-firecracker-k8s/cache}"
NET_PREFIX="172.31.${SUBNET_OCTET}"
GATEWAY="${NET_PREFIX}.1"
SERVER_IP="${NET_PREFIX}.10"
CIDR="${NET_PREFIX}.0/24"
TOKEN_FILE="${RUN_ROOT}/cluster-token"

cleanup_run() {
  local remove_root="${1:-0}"
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
  if [[ "${remove_root}" == "1" ]]; then
    rm -rf "${RUN_ROOT}"
  else
    rm -rf "${RUN_ROOT}/vms" "${TOKEN_FILE}" "${RUN_ROOT}/receipt.json" "${RUN_ROOT}/nodes.txt" "${RUN_ROOT}/pods.txt" "${RUN_ROOT}/kubeconfig.yaml" "${RUN_ROOT}/server-journal.txt"
    mkdir -p "${RUN_ROOT}"
  fi
  set -e
}

if [[ "${mode}" == "delete" || "${mode}" == "cleanup" ]]; then
  cleanup_run 1
  exit 0
fi

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 2; }
}

for cmd in cp e2fsck mount resize2fs sha256sum ssh sysctl truncate umount; do
  require_cmd "${cmd}"
done
for path in "${BASE_ROOTFS}" "${KERNEL}" "${K3S_BIN}" "${FIRECRACKER}" "${LAB_KEY}"; do
  [[ -e "${path}" ]] || { echo "missing ${path}" >&2; exit 2; }
done

mkdir -p "${RUN_ROOT}/vms" "${CACHE_ROOT}"
cache_key="$(
  {
    sha256sum "${BASE_ROOTFS}" "${K3S_BIN}" "${KERNEL}"
    printf 'packages=iptables,conntrack,ipset,ethtool,socat,ca-certificates\n'
  } | sha256sum | awk '{print substr($1,1,16)}'
)"
prepared="${CACHE_ROOT}/prepared-${cache_key}.ext4"

prepare_base_image() {
  local tmp="${prepared}.tmp"
  local mnt="${CACHE_ROOT}/mnt-${cache_key}"
  rm -f "${tmp}"
  cp --reflink=auto "${BASE_ROOTFS}" "${tmp}" 2>/dev/null || cp "${BASE_ROOTFS}" "${tmp}"
  set +e
  e2fsck -fy "${tmp}" >/tmp/torque-fc-e2fsck-${cache_key}.log 2>&1
  local e=$?
  set -e
  [[ "${e}" -le 1 ]] || { cat /tmp/torque-fc-e2fsck-${cache_key}.log >&2; exit "${e}"; }
  truncate -s 3G "${tmp}"
  resize2fs "${tmp}" >/tmp/torque-fc-resize-${cache_key}.log 2>&1
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
  chroot "${mnt}" apt-get update >/tmp/torque-fc-apt-update-${cache_key}.log 2>&1
  chroot "${mnt}" env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    iptables conntrack ipset ethtool socat ca-certificates >/tmp/torque-fc-apt-install-${cache_key}.log 2>&1
  install -m 0755 "${K3S_BIN}" "${mnt}/usr/local/bin/k3s"
  chroot "${mnt}" update-alternatives --set iptables /usr/sbin/iptables-legacy >/dev/null 2>&1 || true
  chroot "${mnt}" update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy >/dev/null 2>&1 || true
  cleanup_mounts
  trap - RETURN
  mv "${tmp}" "${prepared}"
}

if [[ ! -s "${prepared}" ]]; then
  echo "preparing-base-image"
  prepare_base_image
else
  echo "using-cached-base-image"
fi

SSH_OPTS=(-n -i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=2)
if [[ -s "${RUN_ROOT}/receipt.json" && -d "${RUN_ROOT}/vms" ]]; then
  live_count=0
  for pid_file in "${RUN_ROOT}"/vms/*/pid; do
    [[ -f "${pid_file}" ]] && kill -0 "$(cat "${pid_file}")" 2>/dev/null && live_count="$((live_count + 1))"
  done
  nodes_text="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" '/usr/local/bin/k3s kubectl get nodes -o wide --no-headers 2>/dev/null' || true)"
  printf '%s\n' "${nodes_text}" >"${RUN_ROOT}/nodes.txt"
  ready_count="$(printf '%s\n' "${nodes_text}" | awk '$2=="Ready"{c++} END{print c+0}')"
  if [[ "${live_count}" -ge "${NODE_COUNT}" && "${ready_count}" -ge "${NODE_COUNT}" ]]; then
    ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'cat /etc/rancher/k3s/k3s.yaml' |
      sed "s#https://0.0.0.0:6443#https://${SERVER_IP}:6443#g" >"${RUN_ROOT}/kubeconfig.yaml"
    ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" '/usr/local/bin/k3s kubectl get pods -A -o wide || true' >"${RUN_ROOT}/pods.txt" 2>&1 || true
    cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-k8s/v1","kind":"FirecrackerK3sReceipt","status":"succeeded","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"readyCount":${ready_count},"serverIP":"${SERVER_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}","idempotentReuse":true}
EOF
    echo "cluster-already-ready nodes=${ready_count} live=${live_count}"
    cat "${RUN_ROOT}/nodes.txt"
    exit 0
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
ExecStart=/usr/local/bin/k3s server --cluster-init --node-ip ${ip} --advertise-address ${ip} --bind-address 0.0.0.0 --tls-san ${ip} --tls-san 127.0.0.1 --flannel-iface eth0 --flannel-backend=host-gw --disable-kube-proxy --disable-network-policy --write-kubeconfig-mode 0644 --disable traefik --disable servicelb --disable metrics-server --disable coredns --disable local-storage --kubelet-arg=fail-cgroupv1=false
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
ExecStart=/usr/local/bin/k3s agent --server https://${SERVER_IP}:6443 --node-ip ${ip} --flannel-iface eth0 --kubelet-arg=fail-cgroupv1=false
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

for i in $(seq 0 "$((NODE_COUNT - 1))"); do
  vm="${RUN_ROOT}/vms/node${i}"
  mkdir -p "${vm}"
  ip="${NET_PREFIX}.$((10 + i))"
  name="$(printf 'fc-%02d' "${i}")"
  tap="${TAP_PREFIX}${i}"
  mac="$(printf '06:00:00:%02x:00:%02x' "${SUBNET_OCTET}" "$((10 + i))")"
  cp --reflink=auto "${prepared}" "${vm}/rootfs.ext4" 2>/dev/null || cp "${prepared}" "${vm}/rootfs.ext4"
  set +e
  e2fsck -fy "${vm}/rootfs.ext4" >/dev/null 2>&1
  e=$?
  set -e
  [[ "${e}" -le 1 ]] || exit "${e}"
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
  rm -f "${mnt}/etc/resolv.conf"
  printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
  rm -f "${mnt}/etc/machine-id"
  touch "${mnt}/etc/machine-id"
  rm -f "${mnt}/var/lib/dbus/machine-id" 2>/dev/null || true
  rm -rf "${mnt}/var/lib/rancher/k3s" "${mnt}/var/lib/kubelet" "${mnt}/run/k3s" "${mnt}/run/flannel" 2>/dev/null || true
  mkdir -p "${mnt}/etc/systemd/system/multi-user.target.wants"
  role="agent"
  [[ "${i}" -eq 0 ]] && role="server"
  write_service "${role}" "${ip}" >"${mnt}/etc/systemd/system/k3s.service"
  ln -sf /etc/systemd/system/k3s.service "${mnt}/etc/systemd/system/multi-user.target.wants/k3s.service"
  ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
  umount "${mnt}"
  ip tuntap add dev "${tap}" mode tap
  ip link set "${tap}" master "${BRIDGE_NAME}"
  ip link set "${tap}" up
  cat >"${vm}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=0 systemd.legacy_systemd_cgroup_controller=1 systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${vm}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":1,"mem_size_mib":1024},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${tap}","guest_mac":"${mac}"}],"logger":{"log_path":"${vm}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
  "${FIRECRACKER}" --api-sock "${vm}/fc.sock" --config-file "${vm}/vm.json" >"${vm}/console.log" 2>&1 &
  echo $! >"${vm}/pid"
  echo "started ${name} ${ip}"
done

for _ in $(seq 1 120); do
  if ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" true >/dev/null 2>&1; then
    echo "server-ssh-ready"
    break
  fi
  sleep 2
done

for attempt in $(seq 1 360); do
  nodes_text="$(ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" '/usr/local/bin/k3s kubectl get nodes -o wide --no-headers 2>/dev/null' || true)"
  printf '%s\n' "${nodes_text}" >"${RUN_ROOT}/nodes.txt"
  ready_count="$(printf '%s\n' "${nodes_text}" | awk '$2=="Ready"{c++} END{print c+0}')"
  if [[ "${ready_count}" -ge "${NODE_COUNT}" ]]; then
    ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'cat /etc/rancher/k3s/k3s.yaml' |
      sed "s#https://0.0.0.0:6443#https://${SERVER_IP}:6443#g" >"${RUN_ROOT}/kubeconfig.yaml"
    ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" '/usr/local/bin/k3s kubectl get pods -A -o wide || true' >"${RUN_ROOT}/pods.txt" 2>&1 || true
    cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-k8s/v1","kind":"FirecrackerK3sReceipt","status":"succeeded","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"readyCount":${ready_count},"serverIP":"${SERVER_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}"}
EOF
    echo "cluster-ready nodes=${ready_count}"
    cat "${RUN_ROOT}/nodes.txt"
    exit 0
  fi
  if (( attempt % 30 == 0 )); then
    echo "waiting-cluster attempt=${attempt} ready=${ready_count}/${NODE_COUNT}"
    cat "${RUN_ROOT}/nodes.txt"
  fi
  sleep 2
done

ssh "${SSH_OPTS[@]}" "root@${SERVER_IP}" 'journalctl -u k3s --no-pager -n 180 || true' >"${RUN_ROOT}/server-journal.txt" 2>&1 || true
cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-k8s/v1","kind":"FirecrackerK3sReceipt","status":"failed","runId":"${RUN_ID}","nodeCount":${NODE_COUNT},"serverIP":"${SERVER_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}"}
EOF
echo "cluster failed; see ${RUN_ROOT}" >&2
exit 1
'''

remote_b64 = base64.b64encode(remote_script.encode("utf-8")).decode("ascii")
remote_script_path = f"{remote_root}/bootstrap-firecracker-k3s.sh"
bridge = f"tqfc{short_suffix}"
tap_prefix = f"tq{short_suffix}"

def q(value: str) -> str:
    return shlex.quote(value)

remote_apply = f"""set -euo pipefail
mkdir -p {q(remote_root)}
printf %s {q(remote_b64)} | base64 -d > {q(remote_script_path)}
chmod +x {q(remote_script_path)}
RUN_ROOT={q(remote_root)} NODE_COUNT={node_count} SUBNET_OCTET={subnet_octet} BRIDGE_NAME={q(bridge)} TAP_PREFIX={q(tap_prefix)} RUN_ID={q(run_id)} {q(remote_script_path)} apply
"""
remote_delete = f"""set +e
if [ -x {q(remote_script_path)} ]; then
  RUN_ROOT={q(remote_root)} NODE_COUNT={node_count} SUBNET_OCTET={subnet_octet} BRIDGE_NAME={q(bridge)} TAP_PREFIX={q(tap_prefix)} RUN_ID={q(run_id)} {q(remote_script_path)} delete
else
  for i in $(seq 0 {node_count - 1}); do ip link del {q(tap_prefix)}$i 2>/dev/null; done
  ip link set {q(bridge)} down 2>/dev/null; ip link del {q(bridge)} type bridge 2>/dev/null
  iptables -t nat -D POSTROUTING -s 172.31.{subnet_octet}.0/24 ! -o {q(bridge)} -j MASQUERADE 2>/dev/null
  rm -rf {q(remote_root)}
fi
"""

kubeconfig = run_dir / "stack" / "firecracker-kubeconfig.yaml"
control = Path(f"/tmp/tqfc-{safe_suffix}-{subnet_octet}.ctl")
target = "root@141.105.65.227"
ssh_base = "ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new"
tunnel_apply = f"""set -euo pipefail
mkdir -p {q(str(run_dir / 'stack'))}
{ssh_base} {q(target)} "cat {q(remote_root + '/kubeconfig.yaml')}" | sed 's#https://{server_ip}:6443#https://127.0.0.1:{tunnel_port}#g' > {q(str(kubeconfig))}
ssh -S {q(str(control))} -O exit {q(target)} >/dev/null 2>&1 || true
rm -f {q(str(control))}
{ssh_base} -o ExitOnForwardFailure=yes -M -S {q(str(control))} -fN -L 127.0.0.1:{tunnel_port}:{server_ip}:6443 {q(target)}
for i in $(seq 1 60); do
  if kubectl --kubeconfig {q(str(kubeconfig))} get nodes >/dev/null 2>&1; then
    kubectl --kubeconfig {q(str(kubeconfig))} get nodes -o wide
    exit 0
  fi
  sleep 2
done
echo tunnel did not expose Kubernetes API >&2
exit 1
"""
tunnel_delete = f"""set +e
ssh -S {q(str(control))} -O exit {q(target)} >/dev/null 2>&1
rm -f {q(str(control))} {q(str(kubeconfig))}
"""

summary = run_dir / "verification" / "summary.json"
nodes_txt = run_dir / "verification" / "nodes.txt"
app_txt = run_dir / "verification" / "app.txt"
app_access_txt = run_dir / "verification" / "app-access.txt"
verify_apply = f"""set -euo pipefail
nodes_file={q(str(nodes_txt))}
app_file={q(str(app_txt))}
access_file={q(str(app_access_txt))}
summary={q(str(summary))}
expected_body={q("torque-firecracker-k8s " + run_id)}
ready_nodes=0
ready_pods=0
desired=0
available=0
accessible=0
for attempt in $(seq 1 72); do
  kubectl --kubeconfig {q(str(kubeconfig))} get nodes -o wide > "${{nodes_file}}"
  kubectl --kubeconfig {q(str(kubeconfig))} -n torque-fc-app get ds,pods -o wide > "${{app_file}}"
  ready_nodes="$(awk '$2=="Ready"{{c++}} END{{print c+0}}' "${{nodes_file}}")"
  ready_pods="$(kubectl --kubeconfig {q(str(kubeconfig))} -n torque-fc-app get pods --no-headers 2>/dev/null | awk '$3=="Running"{{c++}} END{{print c+0}}')"
  desired="$(kubectl --kubeconfig {q(str(kubeconfig))} -n torque-fc-app get ds torque-fc-app -o jsonpath='{{.status.desiredNumberScheduled}}')"
  available="$(kubectl --kubeconfig {q(str(kubeconfig))} -n torque-fc-app get ds torque-fc-app -o jsonpath='{{.status.numberAvailable}}')"
  : > "${{access_file}}"
  for i in $(seq 0 "$(({node_count} - 1))"); do
    ip="172.31.{subnet_octet}.$((10 + i))"
    if body="$({ssh_base} {q(target)} "curl -fsS --max-time 10 http://${{ip}}:18080/" 2>&1)"; then
      printf '%s\\t%s\\n' "${{ip}}" "${{body}}" >>"${{access_file}}"
    else
      printf '%s\\tERROR: %s\\n' "${{ip}}" "${{body}}" >>"${{access_file}}"
    fi
  done
  accessible="$(grep -cF "${{expected_body}}" "${{access_file}}" || true)"
  if [[ "${{ready_nodes}}" == "{node_count}" && "${{ready_pods}}" == "{node_count}" && "${{desired}}" == "{node_count}" && "${{available}}" == "{node_count}" && "${{accessible}}" == "{node_count}" ]]; then
    break
  fi
  if (( attempt % 6 == 0 )); then
    echo "waiting-app attempt=${{attempt}} nodes=${{ready_nodes}} pods=${{ready_pods}} desired=${{desired}} available=${{available}} http=${{accessible}}/{node_count}" >&2
    cat "${{app_file}}" >&2 || true
    cat "${{access_file}}" >&2 || true
  fi
  sleep 5
done
python3 - "$summary" "$ready_nodes" "$ready_pods" "$desired" "$available" "$accessible" "$access_file" <<'VERIFY'
import json, sys
path, ready_nodes, ready_pods, desired, available, accessible, access_file = sys.argv[1:8]
doc = {{
  "apiVersion": "torque.dev/e2e/v1",
  "kind": "FirecrackerK8sVerification",
  "status": "succeeded" if ready_nodes == "{node_count}" and ready_pods == "{node_count}" and desired == "{node_count}" and available == "{node_count}" and accessible == "{node_count}" else "failed",
  "readyNodes": int(ready_nodes),
  "readyPods": int(ready_pods),
  "desiredDaemonSetPods": int(desired),
  "availableDaemonSetPods": int(available),
  "accessibleHTTPNodes": int(accessible),
  "httpPort": 18080,
  "httpEvidence": access_file,
  "expectedNodes": {node_count},
}}
open(path, "w", encoding="utf-8").write(json.dumps(doc, indent=2, sort_keys=True) + "\\n")
if doc["status"] != "succeeded":
  raise SystemExit(json.dumps(doc, sort_keys=True))
VERIFY
cat "$summary"
"""

def block(value: str, spaces: int) -> str:
    pad = " " * spaces
    return "\n".join(pad + line if line else pad for line in value.rstrip("\n").splitlines())

stack_root.mkdir(parents=True, exist_ok=True)
(stack_root / "stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: stack-fc-k8s-001
cli:
  inferDeps: false
nodes:
  - name: fc-k8s-bootstrap
    kind: host.command.run
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      timeout: 60m
      command: |
{block(remote_apply, 8)}
      deleteCommand: |
{block(remote_delete, 8)}
  - name: fc-k8s-tunnel
    kind: host.command.run
    needs: [fc-k8s-bootstrap]
    host:
      transport: local
      timeout: 5m
      command: |
{block(tunnel_apply, 8)}
      deleteCommand: |
{block(tunnel_delete, 8)}
  - name: torque-fc-app
    kind: release.helm
    chart: ./charts/fc-app
    cluster: {{ name: firecracker-k3s, kubeconfig: {str(kubeconfig)!r} }}
    namespace: torque-fc-app
    needs: [fc-k8s-tunnel]
    apply: {{ createNamespace: true, wait: true, timeout: 10m }}
    set: {{ runId: {run_id}, image: hashicorp/http-echo:1.0.0, port: 18080 }}
  - name: fc-k8s-verify
    kind: host.command.run
    needs: [torque-fc-app]
    host:
      transport: local
      timeout: 5m
      command: |
{block(verify_apply, 8)}
      deleteCommand: "true"
""",
    encoding="utf-8",
)
PY

ops_log "plan Firecracker stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply Firecracker stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"
stack_applied=1

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover stack apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ops_log "reapply Firecracker stack for idempotence"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/reapply.jsonl" 2>"${OPS_RUN_DIR}/stack/reapply.stderr"

reapply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${reapply_run_id}" ]] || ops_fail "failed to discover stack reapply run ID"
printf '%s\n' "${reapply_run_id}" >"${OPS_RUN_DIR}/stack/reapply-run-id.txt"
audit_run_id="${reapply_run_id}"

ops_log "collect remote Firecracker receipt"
ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/receipt.json'" >"${OPS_RUN_DIR}/remote/receipt.json"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/nodes.txt'" >"${OPS_RUN_DIR}/remote/nodes.txt"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/pods.txt'" >"${OPS_RUN_DIR}/remote/pods.txt" || true

ops_log "audit and export stack run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${audit_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit.json" 2>"${OPS_RUN_DIR}/stack/audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${audit_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "verify Firecracker stack evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${node_count}" \
  "${OPS_RUN_DIR}/remote/receipt.json" \
  "${OPS_RUN_DIR}/verification/summary.json" \
  "${OPS_RUN_DIR}/stack/audit.json" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
expected = int(sys.argv[5])
receipt = json.loads(Path(sys.argv[6]).read_text(encoding="utf-8"))
summary = json.loads(Path(sys.argv[7]).read_text(encoding="utf-8"))
audit = json.loads(Path(sys.argv[8]).read_text(encoding="utf-8"))
errors = []

if receipt.get("status") != "succeeded":
    errors.append("remote Firecracker receipt failed")
if int(receipt.get("readyCount", 0)) != expected:
    errors.append("remote ready count mismatch")
if summary.get("status") != "succeeded":
    errors.append("local app verification failed")
if int(summary.get("readyNodes", 0)) != expected:
    errors.append("verified node count mismatch")
if int(summary.get("availableDaemonSetPods", 0)) != expected:
    errors.append("DaemonSet availability mismatch")
if int(summary.get("accessibleHTTPNodes", 0)) != expected:
    errors.append("HTTP app accessibility mismatch")
if audit.get("status") != "succeeded":
    errors.append(f"stack audit status is {audit.get('status')}")
integrity = audit.get("integrity", {})
if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
    errors.append("stack audit integrity failed")
if not (run_dir / "stack" / "stack-export.tgz").is_file():
    errors.append("missing stack export bundle")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackFirecrackerK8s001Receipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if not errors else "failed",
    "startedAt": started_at,
    "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "expectedNodes": expected,
    "readyNodes": summary.get("readyNodes"),
    "availableDaemonSetPods": summary.get("availableDaemonSetPods"),
    "accessibleHTTPNodes": summary.get("accessibleHTTPNodes"),
    "httpPort": summary.get("httpPort"),
    "serverIP": receipt.get("serverIP"),
    "remoteSubnet": receipt.get("subnet"),
    "errors": errors,
}
(run_dir / "verification" / "receipt.json").write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY

ops_log "STACK-FC-K8S-001 passed"
