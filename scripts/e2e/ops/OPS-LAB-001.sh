#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-LAB-001.sh [options]

Options:
  --lab-profile PROFILE   Lab profile to run. Repeatable or comma-separated.
                          Defaults to lab.local,lab.ssh-linux,lab.k3s.
  --evidence-root DIR     Evidence root. Defaults to a temp directory.
  --cleanup               Clean lab resources after the run. Default.
  --no-cleanup            Leave lab resources for debugging.
  -h, --help              Show this help.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1   Required when running non-local lab profiles.
  TORQUE_LAB_SSH             SSH target for lab.ssh-linux, e.g. ssh://root@host.
  TORQUE_LAB_K3S_SSH         SSH target for lab.k3s. Defaults to TORQUE_LAB_SSH.
  TORQUE_LAB_K3S_KUBECTL     Remote kubectl command. Defaults to "k3s kubectl".
  TORQUE_LAB_NAMESPACE_PREFIX Namespace prefix. Defaults to "torque-e2e".
  TORQUE_LAB_SSH_IDENTITY    Optional SSH private key path.
  TORQUE_LAB_SSH_OPTS        Optional extra SSH options.

The lab.ssh-linux profile also records whether the remote lab exposes KVM,
QEMU, and Firecracker so later task scripts can use isolated VM/microVM tests.
EOF
}

cleanup_enabled=1
profiles=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --lab-profile)
      [[ $# -ge 2 ]] || ops_fail "--lab-profile requires a value"
      IFS=',' read -r -a split_profiles <<<"$2"
      profiles+=("${split_profiles[@]}")
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

if [[ ${#profiles[@]} -eq 0 ]]; then
  profiles=(lab.local lab.ssh-linux lab.k3s)
fi

for profile in "${profiles[@]}"; do
  case "${profile}" in
    lab.local|lab.ssh-linux|lab.k3s) ;;
    *) ops_fail "unsupported lab profile for OPS-LAB-001: ${profile}" ;;
  esac
done

for profile in "${profiles[@]}"; do
  if [[ "${profile}" != "lab.local" && "${TORQUE_OPS_E2E_CONFIRM:-}" != "1" ]]; then
    ops_fail "refusing remote lab run without TORQUE_OPS_E2E_CONFIRM=1"
  fi
done

ops_init_run "OPS-LAB-001"
repo_root="$(ops_repo_root)"
started_at="$(ops_utc_now)"
profile_csv="$(IFS=,; echo "${profiles[*]}")"

ops_write_json_object "${OPS_RUN_DIR}/metadata.json" \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  startedAt="${started_at}" \
  profiles="${profile_csv}" \
  cleanupRequested="${cleanup_enabled}" \
  repoRoot="${repo_root}"

write_target_snapshot() {
  local path="${OPS_RUN_DIR}/target-snapshot.json"
  mkdir -p "$(dirname "${path}")"
  python3 - "${path}" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${profile_csv}" "${TORQUE_LAB_SSH:-}" "${TORQUE_LAB_K3S_SSH:-${TORQUE_LAB_SSH:-}}" <<'PY'
import json
import sys

path, task_id, run_id, profile_csv, ssh_target, k3s_target = sys.argv[1:7]
profiles = [item for item in profile_csv.split(",") if item]
targets = []
for profile in profiles:
    target_type = profile[4:] if profile.startswith("lab.") else profile
    target = {"profile": profile, "type": target_type}
    if profile == "lab.ssh-linux":
        target["transport"] = "ssh"
        target["configured"] = bool(ssh_target)
    elif profile == "lab.k3s":
        target["transport"] = "ssh+k3s"
        target["configured"] = bool(k3s_target)
    else:
        target["transport"] = "local"
        target["configured"] = True
    targets.append(target)

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "profiles": profiles,
    "targets": targets,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
}

write_target_snapshot
ops_write_json_object "${OPS_RUN_DIR}/decision.json" \
  status=succeeded \
  decision=allow \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  reason=operator-confirmed-lab-smoke \
  cleanupRequested="${cleanup_enabled}" \
  decidedAt="$(ops_utc_now)"

ssh_remote_root=""
k3s_namespace=""
k3s_target=""

cleanup_lab_resources() {
  local cleanup_started
  cleanup_started="$(ops_utc_now)"
  mkdir -p "${OPS_RUN_DIR}/cleanup"

  local status="succeeded"
  local ssh_status="not-requested"
  if [[ -n "${ssh_remote_root}" && -n "${TORQUE_LAB_SSH:-}" ]]; then
    ops_set_ssh_base_args
    if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "rm -rf '${ssh_remote_root}' && test ! -e '${ssh_remote_root}'"; then
      ssh_status="deleted"
    else
      ssh_status="failed"
      status="failed"
    fi
  fi

  local k3s_status="not-requested"
  if [[ -n "${k3s_namespace}" && -n "${k3s_target}" ]]; then
    local kubectl_bin="${TORQUE_LAB_K3S_KUBECTL:-k3s kubectl}"
    ops_set_ssh_base_args
    if ssh "${OPS_SSH_ARGS[@]}" "${k3s_target}" "${kubectl_bin} delete ns '${k3s_namespace}' --wait=true --timeout=90s --ignore-not-found >/dev/null"; then
      k3s_status="deleted"
    else
      k3s_status="failed"
      status="failed"
    fi
  fi

  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    startedAt="${cleanup_started}" \
    finishedAt="$(ops_utc_now)" \
    ssh="${ssh_status}" \
    k3s="${k3s_status}"
  [[ "${status}" == "succeeded" ]]
}

finish() {
  local code=$?
  trap - EXIT
  local cleanup_code=0
  if [[ "${cleanup_enabled}" == "1" ]]; then
    cleanup_lab_resources || cleanup_code=$?
  fi
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

run_local_smoke() {
  ops_log "lab.local smoke"
  local dir="${OPS_RUN_DIR}/local"
  local scratch
  scratch="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-lab-local.XXXXXX")"
  mkdir -p "${dir}"
  printf 'local marker for %s\n' "${OPS_RUN_ID}" >"${scratch}/marker.txt"
  {
    printf 'profile=lab.local\n'
    printf 'marker=%s\n' "$(cat "${scratch}/marker.txt")"
    printf 'password=%s\n' "${OPS_SECRET_CANARY}"
  } | ops_redact_stdin "${dir}/output.redacted.txt"
  rm -rf "${scratch}"
  test ! -e "${scratch}" || ops_fail "local scratch cleanup failed"
  ops_write_json_object "${dir}/receipt.json" \
    status=succeeded \
    profile=lab.local \
    scratchCleanup=deleted \
    output=output.redacted.txt \
    observedAt="$(ops_utc_now)"
}

run_ssh_smoke() {
  ops_log "lab.ssh-linux smoke"
  ops_require_cmd ssh
  ops_require_env TORQUE_LAB_SSH

  local dir="${OPS_RUN_DIR}/ssh"
  mkdir -p "${dir}"
  ssh_remote_root="/tmp/torque-ops-lab-${OPS_RUN_ID}"

  ops_set_ssh_base_args
  local target
  target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"

  ssh "${OPS_SSH_ARGS[@]}" "${target}" "rm -rf '${ssh_remote_root}' && mkdir -p '${ssh_remote_root}' && uname -a > '${ssh_remote_root}/uname.txt' && printf 'password=%s\n' '${OPS_SECRET_CANARY}' > '${ssh_remote_root}/secret-canary.txt'"
  {
    printf 'profile=lab.ssh-linux\n'
    printf 'target=%s\n' "${target}"
    ssh "${OPS_SSH_ARGS[@]}" "${target}" "cat '${ssh_remote_root}/uname.txt'; cat '${ssh_remote_root}/secret-canary.txt'"
  } | ops_redact_stdin "${dir}/output.redacted.txt"
  ssh "${OPS_SSH_ARGS[@]}" "${target}" "set -eu
if [ -e /dev/kvm ]; then echo kvm=present; else echo kvm=missing; fi
if command -v qemu-system-x86_64 >/dev/null 2>&1; then echo qemu-system-x86_64=\$(command -v qemu-system-x86_64); else echo qemu-system-x86_64=missing; fi
if command -v qemu-img >/dev/null 2>&1; then echo qemu-img=\$(command -v qemu-img); else echo qemu-img=missing; fi
if command -v firecracker >/dev/null 2>&1; then echo firecracker=\$(command -v firecracker); else echo firecracker=missing; fi
if command -v firectl >/dev/null 2>&1; then echo firectl=\$(command -v firectl); else echo firectl=missing; fi
if command -v jailer >/dev/null 2>&1; then echo jailer=\$(command -v jailer); else echo jailer=missing; fi
" | ops_redact_stdin "${dir}/vm-capabilities.redacted.txt"

  ops_write_json_object "${dir}/receipt.json" \
    status=succeeded \
    profile=lab.ssh-linux \
    target="${target}" \
    remoteRoot="${ssh_remote_root}" \
    output=output.redacted.txt \
    vmCapabilities=vm-capabilities.redacted.txt \
    observedAt="$(ops_utc_now)"
}

run_k3s_smoke() {
  ops_log "lab.k3s smoke"
  ops_require_cmd ssh
  if [[ -z "${TORQUE_LAB_K3S_SSH:-}" ]]; then
    ops_require_env TORQUE_LAB_SSH
    TORQUE_LAB_K3S_SSH="${TORQUE_LAB_SSH}"
  fi

  local dir="${OPS_RUN_DIR}/k3s"
  mkdir -p "${dir}"
  k3s_target="$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")"
  local kubectl_bin="${TORQUE_LAB_K3S_KUBECTL:-k3s kubectl}"
  local namespace_prefix="${TORQUE_LAB_NAMESPACE_PREFIX:-torque-e2e}"
  local run_slug
  run_slug="$(printf '%s' "${OPS_RUN_ID}" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-')"
  k3s_namespace="${namespace_prefix}-lab1-${run_slug}"
  if [[ ${#k3s_namespace} -gt 63 ]]; then
    k3s_namespace="${k3s_namespace:0:63}"
    k3s_namespace="${k3s_namespace%-}"
  fi

  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "${k3s_target}" "${kubectl_bin} create ns '${k3s_namespace}' >/dev/null"
  ssh "${OPS_SSH_ARGS[@]}" "${k3s_target}" "${kubectl_bin} -n '${k3s_namespace}' create configmap torque-ops-lab-smoke --from-literal=run='${OPS_RUN_ID}' --from-literal=purpose='OPS-LAB-001' >/dev/null"
  {
    printf 'profile=lab.k3s\n'
    printf 'target=%s\n' "${k3s_target}"
    printf 'namespace=%s\n' "${k3s_namespace}"
    ssh "${OPS_SSH_ARGS[@]}" "${k3s_target}" "${kubectl_bin} -n '${k3s_namespace}' get configmap torque-ops-lab-smoke -o json"
    printf '\ntoken=%s\n' "${OPS_SECRET_CANARY}"
  } | ops_redact_stdin "${dir}/output.redacted.txt"

  ops_write_json_object "${dir}/receipt.json" \
    status=succeeded \
    profile=lab.k3s \
    target="${k3s_target}" \
    namespace="${k3s_namespace}" \
    output=output.redacted.txt \
    observedAt="$(ops_utc_now)"
}

for profile in "${profiles[@]}"; do
  case "${profile}" in
    lab.local) run_local_smoke ;;
    lab.ssh-linux) run_ssh_smoke ;;
    lab.k3s) run_k3s_smoke ;;
  esac
done

mkdir -p "${OPS_RUN_DIR}/verification"
python3 - "${OPS_RUN_DIR}/verification/receipt.json" "${OPS_TASK_ID}" "${OPS_RUN_ID}" "${profile_csv}" "$(ops_utc_now)" <<'PY'
import json
import os
import sys

path, task_id, run_id, profile_csv, verified_at = sys.argv[1:6]
root = os.path.dirname(os.path.dirname(path))
profiles = [item for item in profile_csv.split(",") if item]
receipts = []
for profile in profiles:
    if profile == "lab.local":
        rel = "local/receipt.json"
    elif profile == "lab.ssh-linux":
        rel = "ssh/receipt.json"
    elif profile == "lab.k3s":
        rel = "k3s/receipt.json"
    else:
        continue
    receipts.append({"profile": profile, "path": rel, "present": os.path.isfile(os.path.join(root, rel))})

doc = {
    "status": "succeeded",
    "taskId": task_id,
    "runId": run_id,
    "profiles": profiles,
    "receipts": receipts,
    "verifiedAt": verified_at,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY

ops_write_json_object "${OPS_RUN_DIR}/result.json" \
  status=succeeded \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  finishedAt="$(ops_utc_now)" \
  profiles="${profile_csv}"
