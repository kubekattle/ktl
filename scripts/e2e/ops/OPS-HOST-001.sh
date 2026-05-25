#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-001.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Delete Firecracker VM resources after the run. Default.
  --no-cleanup         Leave the VM running for debugging.
  -h, --help           Show this help.

OPS-HOST-001 proves guarded host.command.run inside a real Firecracker VM on
the lab SSH host. It boots one microVM, collects target facts, seals ops plan
inputs, applies an approved command, proves a policy-blocked command never
mutates, proves a timeout receipt, audits/exports the stack run, and cleans up.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing Firecracker VM E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "${OPS_HOST_001_TASK_ID:-OPS-HOST-001}"
started_at="$(ops_utc_now)"
host_ssh_identity="${TORQUE_LAB_SSH_IDENTITY:-}"
host_ssh_opts="${TORQUE_LAB_SSH_OPTS:-}"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-001.XXXXXX")"
cksum_value="$(printf '%s' "${OPS_RUN_ID}" | cksum | awk '{print $1}')"
short_suffix="$(printf '%08x' "${cksum_value}")"
subnet_octet="$((120 + (cksum_value % 40)))"
remote_root="/var/lib/torque-ops-host-001/runs/${OPS_RUN_ID}"
guest_ip="172.30.${subnet_octet}.10"
bridge_name="tqoh${short_suffix}"
tap_name="to${short_suffix}0"
stack_safe="${scratch_root}/stack-safe"
stack_blocked="${scratch_root}/stack-blocked"
stack_timeout="${scratch_root}/stack-timeout"
targetgraph_path="${scratch_root}/targetgraph.yaml"
facts_dir="${scratch_root}/facts"
lock_dir="${scratch_root}/locks"
policy_allow="${scratch_root}/policy-allow.json"
policy_block="${scratch_root}/policy-block.json"
guest_key="${scratch_root}/guest_ssh_key"
holder="ops-host-001-${OPS_RUN_ID}"
target_id="vm/ops-host-001"
lock_scope="target/${target_id}"
safe_run_id=""
blocked_run_id=""
timeout_run_id=""

cleanup_remote_vm() {
 [[ "${cleanup_enabled}" == "1" ]] || return 0
  local current_identity="${TORQUE_LAB_SSH_IDENTITY:-}"
  local current_opts="${TORQUE_LAB_SSH_OPTS:-}"
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
  ops_set_ssh_base_args
  local cleanup_code=0
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "set +e
if [ -x '${remote_root}/bootstrap-host-command-vm.sh' ]; then
  RUN_ROOT='${remote_root}' SUBNET_OCTET='${subnet_octet}' BRIDGE_NAME='${bridge_name}' TAP_NAME='${tap_name}' RUN_ID='${OPS_RUN_ID}' '${remote_root}/bootstrap-host-command-vm.sh' delete
fi
pid_file='${remote_root}/vm/pid'
[ -f \"\${pid_file}\" ] && kill \"\$(cat \"\${pid_file}\")\" 2>/dev/null
ip link del '${tap_name}' 2>/dev/null
ip link set '${bridge_name}' down 2>/dev/null
ip link del '${bridge_name}' type bridge 2>/dev/null
rm -rf '${remote_root}'
true" >"${OPS_RUN_DIR}/remote/cleanup.out" 2>"${OPS_RUN_DIR}/remote/cleanup.stderr" || cleanup_code=1
  if [[ -n "${current_identity}" ]]; then
    export TORQUE_LAB_SSH_IDENTITY="${current_identity}"
  else
    unset TORQUE_LAB_SSH_IDENTITY
  fi
  if [[ -n "${current_opts}" ]]; then
    export TORQUE_LAB_SSH_OPTS="${current_opts}"
  else
    unset TORQUE_LAB_SSH_OPTS
  fi
  return "${cleanup_code}"
}

finish() {
  local code=$?
  trap - EXIT
  local cleanup_status="skipped"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    if cleanup_remote_vm; then
      cleanup_status="succeeded"
    else
      cleanup_status="failed"
      code=1
    fi
  fi
  rm -f "${guest_key}"
  [[ "${cleanup_enabled}" == "1" ]] && rm -rf "${scratch_root}"

  python3 - \
    "${OPS_RUN_DIR}" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${started_at}" \
    "${TORQUE_LAB_SSH}" \
    "${guest_ip}" \
    "${remote_root}" \
    "${safe_run_id}" \
    "${blocked_run_id}" \
    "${timeout_run_id}" \
    "${cleanup_status}" \
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
    guest_ip,
    remote_root,
    safe_run_id,
    blocked_run_id,
    timeout_run_id,
    cleanup_status,
    exit_code,
) = sys.argv[1:13]
run = Path(run_dir)
code = int(exit_code)

def load(rel):
    path = run / rel
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}

def write(rel, doc):
    path = run / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

errors = []
safe = load("stack/safe-audit.json")
blocked = load("stack/blocked-audit.json")
timeout = load("stack/timeout-audit.json")
marker = (run / "vm" / "safe-marker.txt")
blocked_marker = (run / "vm" / "blocked-marker-check.out")
if safe_run_id and safe.get("status") != "succeeded":
    errors.append("safe stack run did not succeed")
if marker.is_file() and marker.read_text(encoding="utf-8").strip() != run_id:
    errors.append("safe marker did not match run id")
elif not marker.is_file():
    errors.append("safe marker missing")
if blocked_run_id and blocked.get("status") not in {"blocked", "failed"}:
    errors.append("blocked stack run did not block or fail")
if blocked_marker.is_file() and blocked_marker.read_text(encoding="utf-8").strip() != "absent":
    errors.append("blocked marker exists")
if timeout_run_id and timeout.get("status") != "failed":
    errors.append("timeout stack run did not fail")

finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
status = "succeeded" if code == 0 and not errors and cleanup_status in {"succeeded", "skipped"} else "failed"
write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["lab.vm", "lab.ssh-linux"],
    "host": lab_ssh,
    "guestIP": guest_ip,
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "host/firecracker-lab", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {"id": "vm/ops-host-001", "type": "firecracker-vm", "transport": "ssh-via-lab-host", "ip": guest_ip, "remoteRoot": remote_root},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-guarded-host-command-run",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "safeRunId": safe_run_id,
    "blockedRunId": blocked_run_id,
    "timeoutRunId": timeout_run_id,
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "safeRunId": safe_run_id,
    "blockedRunId": blocked_run_id,
    "timeoutRunId": timeout_run_id,
    "safeAuditStatus": safe.get("status"),
    "blockedAuditStatus": blocked.get("status"),
    "timeoutAuditStatus": timeout.get("status"),
    "cleanupStatus": cleanup_status,
    "errors": errors,
    "verifiedAt": finished_at,
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": cleanup_status,
    "remoteRoot": remote_root,
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "finishedAt": finished_at,
    "guestIP": guest_ip,
    "safeRunId": safe_run_id,
    "blockedRunId": blocked_run_id,
    "timeoutRunId": timeout_run_id,
})
if status != "succeeded":
    sys.exit(1)
PY

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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/remote" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/vm" "${OPS_RUN_DIR}/verification"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "install remote Firecracker VM bootstrap"
ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "mkdir -p '${remote_root}' && cat > '${remote_root}/bootstrap-host-command-vm.sh' && chmod +x '${remote_root}/bootstrap-host-command-vm.sh'" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

mode="${1:-apply}"
: "${RUN_ROOT:?missing RUN_ROOT}"
: "${SUBNET_OCTET:?missing SUBNET_OCTET}"
: "${BRIDGE_NAME:?missing BRIDGE_NAME}"
: "${TAP_NAME:?missing TAP_NAME}"
: "${RUN_ID:?missing RUN_ID}"

BASE_ROOTFS="${BASE_ROOTFS:-/opt/firecracker-sandbox-lab/rootfs.ext4}"
KERNEL="${KERNEL:-/opt/firecracker-sandbox-lab/vmlinux.bin}"
FIRECRACKER="${FIRECRACKER:-/usr/local/bin/firecracker}"
LAB_KEY="${LAB_KEY:-/opt/firecracker-sandbox-lab/lab_ssh_key}"
NET_PREFIX="172.30.${SUBNET_OCTET}"
GATEWAY="${NET_PREFIX}.1"
GUEST_IP="${NET_PREFIX}.10"
CIDR="${NET_PREFIX}.0/24"
VM_DIR="${RUN_ROOT}/vm"
MAC_OCTET="$(printf '%02x' "${SUBNET_OCTET}")"

cleanup_run() {
  set +e
  [ -f "${VM_DIR}/pid" ] && kill "$(cat "${VM_DIR}/pid")" 2>/dev/null
  sleep 1
  [ -f "${VM_DIR}/pid" ] && kill -9 "$(cat "${VM_DIR}/pid")" 2>/dev/null
  ip link del "${TAP_NAME}" 2>/dev/null
  ip link set "${BRIDGE_NAME}" down 2>/dev/null
  ip link del "${BRIDGE_NAME}" type bridge 2>/dev/null
  rm -rf "${RUN_ROOT}"
  set -e
}

if [[ "${mode}" == "delete" || "${mode}" == "cleanup" ]]; then
  cleanup_run
  exit 0
fi

for cmd in cp e2fsck ip mount ssh ssh-keygen umount; do
  command -v "${cmd}" >/dev/null 2>&1 || { echo "missing required command: ${cmd}" >&2; exit 2; }
done
for path in "${BASE_ROOTFS}" "${KERNEL}" "${FIRECRACKER}" "${LAB_KEY}"; do
  [[ -e "${path}" ]] || { echo "missing ${path}" >&2; exit 2; }
done

cleanup_run
mkdir -p "${VM_DIR}"
cp --reflink=auto "${BASE_ROOTFS}" "${VM_DIR}/rootfs.ext4" 2>/dev/null || cp "${BASE_ROOTFS}" "${VM_DIR}/rootfs.ext4"
set +e
e2fsck -fy "${VM_DIR}/rootfs.ext4" >"${VM_DIR}/e2fsck.log" 2>&1
e=$?
set -e
[[ "${e}" -le 1 ]] || { cat "${VM_DIR}/e2fsck.log" >&2; exit "${e}"; }

mnt="${VM_DIR}/mnt"
mkdir -p "${mnt}"
mount -o loop "${VM_DIR}/rootfs.ext4" "${mnt}"
cleanup_mount() {
  set +e
  mountpoint -q "${mnt}" && umount "${mnt}"
}
trap cleanup_mount RETURN

printf 'ops-host-001\n' >"${mnt}/etc/hostname"
cat >"${mnt}/etc/hosts" <<EOF
127.0.0.1 localhost
127.0.1.1 ops-host-001
${GUEST_IP} ops-host-001
EOF
cat >"${mnt}/etc/network/interfaces" <<EOF
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet static
    address ${GUEST_IP}
    netmask 255.255.255.0
    gateway ${GATEWAY}
EOF
rm -f "${mnt}/etc/resolv.conf"
printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' >"${mnt}/etc/resolv.conf"
rm -f "${mnt}/etc/machine-id" "${mnt}/var/lib/dbus/machine-id" 2>/dev/null || true
touch "${mnt}/etc/machine-id"
mkdir -p "${mnt}/root/.ssh" "${mnt}/etc/systemd/system/multi-user.target.wants"
ssh-keygen -y -f "${LAB_KEY}" >"${mnt}/root/.ssh/authorized_keys"
chmod 0700 "${mnt}/root/.ssh"
chmod 0600 "${mnt}/root/.ssh/authorized_keys"
ln -sf /lib/systemd/system/ssh.service "${mnt}/etc/systemd/system/multi-user.target.wants/ssh.service"
cleanup_mount
trap - RETURN

ip link add name "${BRIDGE_NAME}" type bridge
ip addr add "${GATEWAY}/24" dev "${BRIDGE_NAME}"
ip link set "${BRIDGE_NAME}" up
ip tuntap add dev "${TAP_NAME}" mode tap
ip link set "${TAP_NAME}" master "${BRIDGE_NAME}"
ip link set "${TAP_NAME}" up

cat >"${VM_DIR}/vm.json" <<EOF
{"boot-source":{"kernel_image_path":"${KERNEL}","boot_args":"console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw random.trust_cpu=on systemd.unified_cgroup_hierarchy=0 systemd.legacy_systemd_cgroup_controller=1 systemd.mask=serial-getty@ttyS0.service systemd.mask=systemd-random-seed.service"},"drives":[{"drive_id":"rootfs","path_on_host":"${VM_DIR}/rootfs.ext4","is_root_device":true,"is_read_only":false}],"machine-config":{"vcpu_count":1,"mem_size_mib":512},"network-interfaces":[{"iface_id":"eth0","host_dev_name":"${TAP_NAME}","guest_mac":"06:00:00:${MAC_OCTET}:00:10"}],"logger":{"log_path":"${VM_DIR}/firecracker.log","level":"Info","show_level":true,"show_log_origin":true}}
EOF
"${FIRECRACKER}" --api-sock "${VM_DIR}/fc.sock" --config-file "${VM_DIR}/vm.json" >"${VM_DIR}/console.log" 2>&1 &
echo $! >"${VM_DIR}/pid"

SSH_OPTS=(-n -i "${LAB_KEY}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=2)
for _ in $(seq 1 90); do
  if ! kill -0 "$(cat "${VM_DIR}/pid")" 2>/dev/null; then
    cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-vm/v1","kind":"FirecrackerVMReceipt","status":"failed","runId":"${RUN_ID}","guestIP":"${GUEST_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}","tap":"${TAP_NAME}","reason":"firecracker exited before ssh readiness"}
EOF
    cat "${VM_DIR}/console.log" >&2 || true
    exit 1
  fi
  if ssh "${SSH_OPTS[@]}" "root@${GUEST_IP}" "printf ready" >/dev/null 2>&1; then
    ssh "${SSH_OPTS[@]}" "root@${GUEST_IP}" "uname -a" >"${RUN_ROOT}/guest-uname.txt" 2>&1 || true
    cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-vm/v1","kind":"FirecrackerVMReceipt","status":"succeeded","runId":"${RUN_ID}","guestIP":"${GUEST_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}","tap":"${TAP_NAME}"}
EOF
    echo "firecracker-vm-ready ${GUEST_IP}"
    exit 0
  fi
  sleep 2
done

cat >"${RUN_ROOT}/receipt.json" <<EOF
{"apiVersion":"torque.dev/firecracker-vm/v1","kind":"FirecrackerVMReceipt","status":"failed","runId":"${RUN_ID}","guestIP":"${GUEST_IP}","subnet":"${CIDR}","bridge":"${BRIDGE_NAME}","tap":"${TAP_NAME}"}
EOF
echo "Firecracker VM did not become reachable over SSH" >&2
exit 1
REMOTE

ops_log "boot Firecracker VM on ${TORQUE_LAB_SSH}"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
  "RUN_ROOT='${remote_root}' SUBNET_OCTET='${subnet_octet}' BRIDGE_NAME='${bridge_name}' TAP_NAME='${tap_name}' RUN_ID='${OPS_RUN_ID}' '${remote_root}/bootstrap-host-command-vm.sh' apply" \
  >"${OPS_RUN_DIR}/remote/bootstrap.out" 2>"${OPS_RUN_DIR}/remote/bootstrap.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/receipt.json'" >"${OPS_RUN_DIR}/remote/receipt.json"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat /opt/firecracker-sandbox-lab/lab_ssh_key" >"${guest_key}"
chmod 0600 "${guest_key}"

proxy_jump="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
guest_ssh_opts=(-i "${guest_key}" -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -J "${proxy_jump}")
export TORQUE_LAB_SSH_IDENTITY="${guest_key}"
export TORQUE_LAB_SSH_OPTS="-J ${proxy_jump} -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

ops_log "create TargetGraph and stack fixtures"
python3 - \
  "${targetgraph_path}" \
  "${guest_key}" \
  "${guest_ip}" \
  "${stack_safe}" \
  "${stack_blocked}" \
  "${stack_timeout}" \
  "${OPS_RUN_ID}" <<'PY'
import sys
from pathlib import Path

targetgraph, guest_key, guest_ip, stack_safe, stack_blocked, stack_timeout, run_id = sys.argv[1:8]
Path(targetgraph).write_text(f"""apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ops-host-001
targets:
  - id: vm/ops-host-001
    type: host
    transportRef: ssh/guest
    labels:
      task: OPS-HOST-001
      profile: lab.vm
    lockScope: target/vm/ops-host-001
    allowedCapabilities:
      - host.command.run
transports:
  - id: ssh/guest
    kind: ssh
    host: root@{guest_ip}
    config:
      identityFile: {guest_key!r}
""", encoding="utf-8")

def write_stack(root, name, command, timeout="20s"):
    root = Path(root)
    root.mkdir(parents=True, exist_ok=True)
    root.joinpath("stack.yaml").write_text(f"""apiVersion: torque.dev/v1
kind: Stack
name: {name}
cli:
  inferDeps: false
nodes:
  - name: {name}
    kind: host.command.run
    host:
      transport: ssh
      targetId: vm/ops-host-001
      target: root@{guest_ip}
      timeout: {timeout}
      command: {command!r}
      deleteCommand: "rm -rf /tmp/torque-ops-host-001"
""", encoding="utf-8")

write_stack(stack_safe, "ops-host-001-safe", f"set -eu; mkdir -p /tmp/torque-ops-host-001; printf 'password=fixture-secret\\n'; printf {run_id!r} > /tmp/torque-ops-host-001/safe-marker")
write_stack(stack_blocked, "ops-host-001-blocked", "set -eu; mkdir -p /tmp/torque-ops-host-001; printf blocked > /tmp/torque-ops-host-001/blocked-marker")
write_stack(stack_timeout, "ops-host-001-timeout", "sleep 5", "1s")
PY

ops_log "collect Firecracker VM facts"
(
  cd "${repo_root}"
  ./bin/torque ops facts collect --targets "${targetgraph_path}" --selector task=OPS-HOST-001 --out-dir "${facts_dir}" --format json --timeout 90s
) >"${OPS_RUN_DIR}/vm/facts-collect.json" 2>"${OPS_RUN_DIR}/vm/facts-collect.stderr"

ops_log "prepare locks and policy decisions"
(
  cd "${repo_root}"
  ./bin/torque ops lock acquire --scope "${lock_scope}" --target "${target_id}" --holder "${holder}" --operation host.command.run --lock-dir "${lock_dir}" --ttl 20m --format json
) >"${OPS_RUN_DIR}/vm/lock-acquire.json" 2>"${OPS_RUN_DIR}/vm/lock-acquire.stderr"
(
  cd "${repo_root}"
  ./bin/torque ops policy check --mode guarded --operation host.command.run --target "${target_id}" --mutating --approved --format json
) >"${policy_allow}" 2>"${OPS_RUN_DIR}/vm/policy-allow.stderr"
(
  cd "${repo_root}"
  ./bin/torque ops policy check --mode observe-only --operation host.command.run --target "${target_id}" --mutating --format json
) >"${policy_block}" 2>"${OPS_RUN_DIR}/vm/policy-block.stderr" || true
cp "${policy_allow}" "${OPS_RUN_DIR}/vm/policy-allow.json"
cp "${policy_block}" "${OPS_RUN_DIR}/vm/policy-block.json"

run_stack_case() {
  local label="$1"
  local stack_root="$2"
  local policy_file="$3"
  local expect="$4"
  local bundle="${OPS_RUN_DIR}/stack/${label}-plan.tgz"
  (
    cd "${repo_root}"
    ./bin/torque stack plan --config "${stack_root}" \
      --ops-targets "${targetgraph_path}" \
      --ops-selector task=OPS-HOST-001 \
      --ops-facts "${facts_dir}" \
      --ops-lock-dir "${lock_dir}" \
      --ops-lock-scope "${lock_scope}" \
      --ops-policy-decision "${policy_file}" \
      --bundle "${bundle}"
  ) >"${OPS_RUN_DIR}/stack/${label}-plan.out" 2>"${OPS_RUN_DIR}/stack/${label}-plan.stderr"
  set +e
  (
    cd "${repo_root}"
    ./bin/torque stack apply --config "${stack_root}" --from-bundle "${bundle}" --yes --concurrency 1 --output json
  ) >"${OPS_RUN_DIR}/stack/${label}-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/${label}-apply.stderr"
  local apply_code=$?
  set -e
  printf '%s\n' "${apply_code}" >"${OPS_RUN_DIR}/stack/${label}-apply.exit"
  if [[ "${expect}" == "success" && "${apply_code}" -ne 0 ]]; then
    ops_fail "${label} apply failed; see ${OPS_RUN_DIR}/stack/${label}-apply.stderr"
  fi
  if [[ "${expect}" != "success" && "${apply_code}" -eq 0 ]]; then
    ops_fail "${label} apply unexpectedly succeeded"
  fi
  local run_id
  run_id="$(
    cd "${repo_root}"
    ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
      python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
  )"
  [[ -n "${run_id}" ]] || ops_fail "failed to discover ${label} run ID"
  printf '%s\n' "${run_id}" >"${OPS_RUN_DIR}/stack/${label}-run-id.txt"
  set +e
  (
    cd "${repo_root}"
    ./bin/torque stack audit --config "${stack_root}" --run-id "${run_id}" --output json --include-artifacts
  ) >"${OPS_RUN_DIR}/stack/${label}-audit.json" 2>"${OPS_RUN_DIR}/stack/${label}-audit.stderr"
  local audit_code=$?
  set -e
  printf '%s\n' "${audit_code}" >"${OPS_RUN_DIR}/stack/${label}-audit.exit"
  if [[ "${expect}" == "success" && "${audit_code}" -ne 0 ]]; then
    ops_fail "${label} audit failed; see ${OPS_RUN_DIR}/stack/${label}-audit.stderr"
  fi
  (
    cd "${repo_root}"
    ./bin/torque stack export --config "${stack_root}" --run-id "${run_id}" --out "${OPS_RUN_DIR}/stack/${label}-export.tgz"
  ) >"${OPS_RUN_DIR}/stack/${label}-export.out" 2>"${OPS_RUN_DIR}/stack/${label}-export.stderr"
  printf '%s' "${run_id}"
}

plan_replay_case() {
  local label="$1"
  local stack_root="$2"
  local policy_file="$3"
  local bundle="${OPS_RUN_DIR}/stack/${label}-plan.tgz"
  (
    cd "${repo_root}"
    ./bin/torque stack plan --config "${stack_root}" \
      --ops-targets "${targetgraph_path}" \
      --ops-selector task=OPS-HOST-001 \
      --ops-facts "${facts_dir}" \
      --ops-lock-dir "${lock_dir}" \
      --ops-lock-scope "${lock_scope}" \
      --ops-policy-decision "${policy_file}" \
      --bundle "${bundle}"
  ) >"${OPS_RUN_DIR}/stack/${label}-plan.out" 2>"${OPS_RUN_DIR}/stack/${label}-plan.stderr"
  printf '%s' "${bundle}"
}

apply_replay_case() {
  local label="$1"
  local stack_root="$2"
  local bundle="$3"
  local expect="$4"
  local approval="$5"
  local args=(stack apply --config "${stack_root}" --from-bundle "${bundle}" --concurrency 1 --output json)
  if [[ "${approval}" == "yes" ]]; then
    args+=(--yes)
  fi
  set +e
  (
    cd "${repo_root}"
    ./bin/torque "${args[@]}"
  ) >"${OPS_RUN_DIR}/stack/${label}-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/${label}-apply.stderr"
  local apply_code=$?
  set -e
  printf '%s\n' "${apply_code}" >"${OPS_RUN_DIR}/stack/${label}-apply.exit"
  if [[ "${expect}" == "success" && "${apply_code}" -ne 0 ]]; then
    ops_fail "${label} apply failed; see ${OPS_RUN_DIR}/stack/${label}-apply.stderr"
  fi
  if [[ "${expect}" != "success" && "${apply_code}" -eq 0 ]]; then
    ops_fail "${label} apply unexpectedly succeeded"
  fi
  local run_id
  run_id="$(
    cd "${repo_root}"
    ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
      python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
  )"
  [[ -n "${run_id}" ]] || ops_fail "failed to discover ${label} run ID"
  printf '%s\n' "${run_id}" >"${OPS_RUN_DIR}/stack/${label}-run-id.txt"
  set +e
  (
    cd "${repo_root}"
    ./bin/torque stack audit --config "${stack_root}" --run-id "${run_id}" --output json --include-artifacts
  ) >"${OPS_RUN_DIR}/stack/${label}-audit.json" 2>"${OPS_RUN_DIR}/stack/${label}-audit.stderr"
  local audit_code=$?
  set -e
  printf '%s\n' "${audit_code}" >"${OPS_RUN_DIR}/stack/${label}-audit.exit"
  if [[ "${expect}" == "success" && "${audit_code}" -ne 0 ]]; then
    ops_fail "${label} audit failed; see ${OPS_RUN_DIR}/stack/${label}-audit.stderr"
  fi
  (
    cd "${repo_root}"
    ./bin/torque stack export --config "${stack_root}" --run-id "${run_id}" --out "${OPS_RUN_DIR}/stack/${label}-export.tgz"
  ) >"${OPS_RUN_DIR}/stack/${label}-export.out" 2>"${OPS_RUN_DIR}/stack/${label}-export.stderr"
  printf '%s' "${run_id}"
}

ops_log "apply approved host.command.run inside Firecracker VM"
safe_run_id="$(run_stack_case safe "${stack_safe}" "${policy_allow}" success)"
ssh "${guest_ssh_opts[@]}" "root@${guest_ip}" "cat /tmp/torque-ops-host-001/safe-marker" >"${OPS_RUN_DIR}/vm/safe-marker.txt"

ops_log "prove blocked policy stops mutation before execution"
blocked_run_id="$(run_stack_case blocked "${stack_blocked}" "${policy_block}" blocked)"
if ssh "${guest_ssh_opts[@]}" "root@${guest_ip}" "test -e /tmp/torque-ops-host-001/blocked-marker"; then
  printf 'present\n' >"${OPS_RUN_DIR}/vm/blocked-marker-check.out"
  ops_fail "blocked marker exists in Firecracker VM"
else
  printf 'absent\n' >"${OPS_RUN_DIR}/vm/blocked-marker-check.out"
fi

ops_log "prove timeout receipt"
timeout_run_id="$(run_stack_case timeout "${stack_timeout}" "${policy_allow}" failed)"

ops_log "prove approved replay gate and drift blocks"
no_yes_bundle="$(plan_replay_case replay-no-yes "${stack_blocked}" "${policy_allow}")"
apply_replay_case replay-no-yes "${stack_blocked}" "${no_yes_bundle}" blocked no >"${OPS_RUN_DIR}/stack/replay-no-yes-run-id.txt"

targetgraph_backup="${scratch_root}/targetgraph.yaml.bak"
cp "${targetgraph_path}" "${targetgraph_backup}"
targetgraph_drift_bundle="$(plan_replay_case replay-targetgraph-drift "${stack_blocked}" "${policy_allow}")"
printf '\n# replay drift %s\n' "${OPS_RUN_ID}" >>"${targetgraph_path}"
apply_replay_case replay-targetgraph-drift "${stack_blocked}" "${targetgraph_drift_bundle}" blocked yes >"${OPS_RUN_DIR}/stack/replay-targetgraph-drift-run-id.txt"
mv "${targetgraph_backup}" "${targetgraph_path}"

facts_backup="${scratch_root}/facts.bak"
rm -rf "${facts_backup}"
cp -a "${facts_dir}" "${facts_backup}"
facts_drift_bundle="$(plan_replay_case replay-facts-drift "${stack_blocked}" "${policy_allow}")"
printf '\n{"drift":"%s"}\n' "${OPS_RUN_ID}" >>"${facts_dir}/facts.json"
apply_replay_case replay-facts-drift "${stack_blocked}" "${facts_drift_bundle}" blocked yes >"${OPS_RUN_DIR}/stack/replay-facts-drift-run-id.txt"
rm -rf "${facts_dir}"
cp -a "${facts_backup}" "${facts_dir}"

policy_backup="${scratch_root}/policy-allow.json.bak"
cp "${policy_allow}" "${policy_backup}"
policy_drift_bundle="$(plan_replay_case replay-policy-drift "${stack_blocked}" "${policy_allow}")"
printf '\n' >>"${policy_allow}"
apply_replay_case replay-policy-drift "${stack_blocked}" "${policy_drift_bundle}" blocked yes >"${OPS_RUN_DIR}/stack/replay-policy-drift-run-id.txt"
mv "${policy_backup}" "${policy_allow}"

lock_backup="${scratch_root}/locks.bak"
rm -rf "${lock_backup}"
cp -a "${lock_dir}" "${lock_backup}"
lock_drift_bundle="$(plan_replay_case replay-lock-drift "${stack_blocked}" "${policy_allow}")"
python3 - "${lock_dir}" <<'PY'
import json
import sys
from pathlib import Path

lock_dir = Path(sys.argv[1])
for path in lock_dir.glob("*.json"):
    doc = json.loads(path.read_text(encoding="utf-8"))
    doc["holder"] = "unexpected-replay-holder"
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY
apply_replay_case replay-lock-drift "${stack_blocked}" "${lock_drift_bundle}" blocked yes >"${OPS_RUN_DIR}/stack/replay-lock-drift-run-id.txt"
rm -rf "${lock_dir}"
cp -a "${lock_backup}" "${lock_dir}"

if ssh "${guest_ssh_opts[@]}" "root@${guest_ip}" "test -e /tmp/torque-ops-host-001/blocked-marker"; then
  printf 'present\n' >"${OPS_RUN_DIR}/vm/replay-blocked-marker-check.out"
  ops_fail "replay blocked marker exists in Firecracker VM"
else
  printf 'absent\n' >"${OPS_RUN_DIR}/vm/replay-blocked-marker-check.out"
fi

ops_log "verify host.command.run and approved replay evidence"
python3 - \
  "${OPS_RUN_DIR}/stack/safe-audit.json" \
  "${OPS_RUN_DIR}/stack/blocked-audit.json" \
  "${OPS_RUN_DIR}/stack/timeout-audit.json" \
  "${OPS_RUN_DIR}/verification/host-command-proof.json" \
  "${OPS_RUN_DIR}/verification/ops-cli-004b-proof.json" \
  "${OPS_RUN_DIR}/stack/replay-no-yes-audit.json" \
  "${OPS_RUN_DIR}/stack/replay-targetgraph-drift-audit.json" \
  "${OPS_RUN_DIR}/stack/replay-facts-drift-audit.json" \
  "${OPS_RUN_DIR}/stack/replay-policy-drift-audit.json" \
  "${OPS_RUN_DIR}/stack/replay-lock-drift-audit.json" <<'PY'
import json
import sys
from pathlib import Path

(
    safe_path,
    blocked_path,
    timeout_path,
    host_out_path,
    replay_out_path,
    no_yes_path,
    targetgraph_drift_path,
    facts_drift_path,
    policy_drift_path,
    lock_drift_path,
) = map(Path, sys.argv[1:11])
errors = []
replay_errors = []

def load(path):
    return json.loads(path.read_text(encoding="utf-8"))

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("name") == name:
            try:
                return json.loads(item.get("body") or "{}")
            except json.JSONDecodeError:
                return {}
    return {}

def has_blocker(doc, code):
    return any(item.get("code") == code for item in doc.get("blockers", []))

safe = load(safe_path)
blocked = load(blocked_path)
timeout = load(timeout_path)
safe_host = artifact(safe, "host-command.json")
safe_verify = artifact(safe, "host-command-verify.json")
safe_replay = artifact(safe, "ops-replay.json")
safe_preflight = artifact(safe, "ops-preflight.json")
blocked_preflight = artifact(blocked, "ops-preflight.json")
timeout_execute = artifact(timeout, "host-command-execute.json")
if safe_host.get("guardMode") != "ops":
    errors.append("safe host command did not use ops guard mode")
if safe_host.get("targetId") != "vm/ops-host-001":
    errors.append("safe host command targetId mismatch")
if "fixture-secret" in json.dumps(safe_host):
    errors.append("safe host command leaked fixture secret")
redaction = safe_verify.get("redaction", {})
if not redaction.get("stdoutRedacted") or not redaction.get("noSensitiveKeyValues"):
    errors.append("safe host command redaction proof missing")
if blocked_preflight.get("status") != "blocked":
    errors.append("blocked policy did not stop at ops preflight")
if timeout_execute.get("status") != "timeout" or not timeout_execute.get("timedOut"):
    errors.append("timeout execution receipt missing timedOut=true")
if safe_replay.get("status") != "eligible":
    replay_errors.append("safe replay was not eligible")
if safe_preflight.get("status") != "eligible":
    replay_errors.append("safe preflight was not eligible")

replay_cases = [
    ("no-yes", load(no_yes_path), "ops-replay.json", "ops.replay.approval_required"),
    ("targetgraph-drift", load(targetgraph_drift_path), "ops-preflight.json", "ops.targetgraph.changed"),
    ("facts-drift", load(facts_drift_path), "ops-preflight.json", "ops.facts.changed"),
    ("policy-drift", load(policy_drift_path), "ops-preflight.json", "ops.policy.changed"),
    ("lock-drift", load(lock_drift_path), "ops-preflight.json", "ops.lock.holder_mismatch"),
]
for label, audit, artifact_name, code in replay_cases:
    if audit.get("status") != "blocked":
        replay_errors.append(f"{label} audit status is {audit.get('status')}, want blocked")
    gate = artifact(audit, artifact_name)
    if gate.get("status") != "blocked" or not has_blocker(gate, code):
        replay_errors.append(f"{label} missing blocker {code} in {artifact_name}")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsHost001Proof",
    "status": "succeeded" if not errors else "failed",
    "errors": errors,
    "safeGuardMode": safe_host.get("guardMode"),
    "safeTargetId": safe_host.get("targetId"),
    "blockedPreflightStatus": blocked_preflight.get("status"),
    "timeoutStatus": timeout_execute.get("status"),
}
host_out_path.parent.mkdir(parents=True, exist_ok=True)
host_out_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
replay_doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsCLI004bProof",
    "status": "succeeded" if not replay_errors else "failed",
    "errors": replay_errors,
    "safeReplayStatus": safe_replay.get("status"),
    "safePreflightStatus": safe_preflight.get("status"),
    "blockedCases": {
        "noYes": "ops.replay.approval_required",
        "targetGraphDrift": "ops.targetgraph.changed",
        "factsDrift": "ops.facts.changed",
        "policyDrift": "ops.policy.changed",
        "lockDrift": "ops.lock.holder_mismatch",
    },
}
replay_out_path.parent.mkdir(parents=True, exist_ok=True)
replay_out_path.write_text(json.dumps(replay_doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
all_errors = errors + replay_errors
if all_errors:
    raise SystemExit("; ".join(all_errors))
PY
