#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-004.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the selected package and local scratch. Default.
  --no-cleanup           Leave scratch/package state for debugging.
  -h, --help             Show this help.

OPS-HOST-004 proves `host.package.install` on lab.ssh-linux. It selects an
absent harmless package from the lab host package cache, installs it through a
stack node, verifies package before/after evidence, repeats apply as a no-op,
audits and exports the stack run, then deletes through the stack and proves the
package was removed.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux package E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-HOST-004"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-004.XXXXXX")"
stack_root="${scratch_root}/stack"
package_manager=""
package_name=""
apply_run_id=""
repeat_run_id=""
delete_run_id=""
cleanup_status="pending"

package_installed_check() {
  local manager="$1"
  local package="$2"
  case "${manager}" in
    apt)
      printf "dpkg-query -W -f='\\\${Status}' -- '%s' 2>/dev/null | grep -q '^install ok installed$'" "${package}"
      ;;
    dnf|yum)
      printf "rpm -q '%s' >/dev/null 2>&1" "${package}"
      ;;
    apk)
      printf "apk info -e '%s' >/dev/null 2>&1" "${package}"
      ;;
    *)
      printf "false"
      ;;
  esac
}

package_remove_command() {
  local manager="$1"
  local package="$2"
  case "${manager}" in
    apt)
      printf "if %s; then apt-get purge -y '%s'; fi" "$(package_installed_check "${manager}" "${package}")" "${package}"
      ;;
    dnf|yum)
      printf "if %s; then %s remove -y '%s'; fi" "$(package_installed_check "${manager}" "${package}")" "${manager}" "${package}"
      ;;
    apk)
      printf "if %s; then apk del '%s'; fi" "$(package_installed_check "${manager}" "${package}")" "${package}"
      ;;
    *)
      printf "false"
      ;;
  esac
}

cleanup_lab_resources() {
  local status="succeeded"
  local scratch_status="not-requested"
  local package_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    if [[ -n "${package_manager}" && -n "${package_name}" ]]; then
      ops_set_ssh_base_args
      local remove_cmd check_cmd
      remove_cmd="$(package_remove_command "${package_manager}" "${package_name}")"
      check_cmd="$(package_installed_check "${package_manager}" "${package_name}")"
      if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "${remove_cmd} >/dev/null 2>&1; ! (${check_cmd})"; then
        package_status="removed"
      else
        package_status="failed"
        status="failed"
      fi
    else
      package_status="skipped"
    fi
  else
    scratch_status="skipped"
    package_status="skipped"
  fi
  cleanup_status="${status}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.ssh-linux" \
    packageManager="${package_manager}" \
    package="${package_name}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    packageCleanup="${package_status}" \
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/ssh" "${OPS_RUN_DIR}/verification"

ops_set_ssh_base_args
ops_log "select harmless absent package on lab host"
if ! selection="$(
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "sh -s" <<'REMOTE'
set -eu

try_apt() {
  for pkg in sl figlet toilet cowsay fortune-mod; do
    if dpkg-query -W -f='${Status}' -- "$pkg" 2>/dev/null | grep -q '^install ok installed$'; then
      continue
    fi
    candidate="$(apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/ {print $2; exit}')"
    if [ -n "$candidate" ] && [ "$candidate" != "(none)" ]; then
      printf 'apt %s\n' "$pkg"
      return 0
    fi
  done
  return 1
}

if command -v apt-get >/dev/null 2>&1 && command -v dpkg-query >/dev/null 2>&1 && command -v apt-cache >/dev/null 2>&1; then
  if try_apt; then
    exit 0
  fi
  if apt-get update -qq >/dev/null 2>&1 && try_apt; then
    exit 0
  fi
fi

if command -v dnf >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1; then
  for pkg in sl figlet cowsay; do
    if rpm -q "$pkg" >/dev/null 2>&1; then
      continue
    fi
    if dnf list available "$pkg" >/dev/null 2>&1; then
      printf 'dnf %s\n' "$pkg"
      exit 0
    fi
  done
fi

if command -v yum >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1; then
  for pkg in sl figlet cowsay; do
    if rpm -q "$pkg" >/dev/null 2>&1; then
      continue
    fi
    if yum list available "$pkg" >/dev/null 2>&1; then
      printf 'yum %s\n' "$pkg"
      exit 0
    fi
  done
fi

if command -v apk >/dev/null 2>&1; then
  for pkg in figlet mandoc; do
    if apk info -e "$pkg" >/dev/null 2>&1; then
      continue
    fi
    if apk search -x "$pkg" | grep -q .; then
      printf 'apk %s\n' "$pkg"
      exit 0
    fi
  done
fi

exit 1
REMOTE
)"; then
  ops_fail "failed to select an absent harmless lab package"
fi
package_manager="${selection%% *}"
package_name="${selection#* }"
[[ -n "${package_manager}" && -n "${package_name}" && "${package_manager}" != "${package_name}" ]] || ops_fail "invalid package selection: ${selection}"
printf '%s\n' "${selection}" >"${OPS_RUN_DIR}/ssh/package-selection.txt"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create host.package.install stack fixture"
python3 - "${stack_root}" "${package_manager}" "${package_name}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
manager = sys.argv[2]
package = sys.argv[3]
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-004
cli:
  inferDeps: false
nodes:
  - name: install-package
    kind: host.package.install
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      packageManager: {manager!r}
      package: {package!r}
      state: present
      purge: true
      removeOnDelete: true
""",
    encoding="utf-8",
)
PY

ops_log "plan host.package.install stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply host.package.install stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover host.package.install apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

case "${package_manager}" in
  apt)
    ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "dpkg-query -W -f='\${Status}\t\${Version}' -- '${package_name}'" >"${OPS_RUN_DIR}/ssh/package-state.apply"
    ;;
  dnf|yum)
    ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "rpm -q --qf '%{VERSION}-%{RELEASE}' '${package_name}'" >"${OPS_RUN_DIR}/ssh/package-state.apply"
    ;;
  apk)
    ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "apk info -v '${package_name}'" >"${OPS_RUN_DIR}/ssh/package-state.apply"
    ;;
esac

ops_log "audit first host.package.install run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-apply.json" 2>"${OPS_RUN_DIR}/stack/audit-apply.stderr"

ops_log "repeat apply to prove no-op"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/repeat-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/repeat-apply.stderr"

repeat_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat host.package.install run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete package through stack"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"

delete_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${delete_run_id}" ]] || ops_fail "failed to discover host.package.install delete run ID"
printf '%s\n' "${delete_run_id}" >"${OPS_RUN_DIR}/stack/delete-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${delete_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-delete.json" 2>"${OPS_RUN_DIR}/stack/audit-delete.stderr"

check_cmd="$(package_installed_check "${package_manager}" "${package_name}")"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "! (${check_cmd})" >"${OPS_RUN_DIR}/ssh/package-delete-check.out" 2>"${OPS_RUN_DIR}/ssh/package-delete-check.stderr"

ops_log "verify host.package.install evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${package_manager}" \
  "${package_name}" \
  "${apply_run_id}" \
  "${repeat_run_id}" \
  "${delete_run_id}" <<'PY'
import hashlib
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
manager = sys.argv[5]
package = sys.argv[6]
apply_run_id = sys.argv[7]
repeat_run_id = sys.argv[8]
delete_run_id = sys.argv[9]

def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path, doc):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("nodeId") == "host.package.install/install-package" and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
plan = read_json(run_dir / "stack" / "plan.json")
apply_audit = read_json(run_dir / "stack" / "audit-apply.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
delete_audit = read_json(run_dir / "stack" / "audit-delete.json")
package_state = (run_dir / "ssh" / "package-state.apply").read_text(encoding="utf-8").strip()
if not package_state:
    errors.append("missing package apply state")
nodes = {node.get("id"): node for node in plan.get("nodes", [])}
if "host.package.install/install-package" not in nodes:
    errors.append("plan missing host.package.install/install-package")
for label, audit in (("apply", apply_audit), ("repeat", repeat_audit), ("delete", delete_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")
apply_receipt = artifact(apply_audit, "host-package-apply.json")
apply_diff = artifact(apply_audit, "host-package-diff.json")
apply_verify = artifact(apply_audit, "host-package-verify.json")
repeat_receipt = artifact(repeat_audit, "host-package-apply.json")
delete_receipt = artifact(delete_audit, "host-package-apply.json")
if apply_receipt.get("status") != "succeeded" or apply_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed package install")
if (apply_receipt.get("after") or {}).get("installed") is not True:
    errors.append("first apply after state is not installed")
if apply_receipt.get("packageManager") != manager:
    errors.append(f"package manager receipt is {apply_receipt.get('packageManager')}, want {manager}")
if (apply_diff.get("changes") or {}).get("installed") is not True or apply_diff.get("diffQuality") != "exact":
    errors.append("first diff receipt did not record exact install diff")
if apply_verify.get("status") != "succeeded" or apply_verify.get("installed") is not True:
    errors.append("verify receipt did not prove installed package")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
if delete_receipt.get("status") != "succeeded" or (delete_receipt.get("after") or {}).get("installed") is not False:
    errors.append("delete receipt did not record package removal")
for name in ("host-package-observe.json", "host-package-plan.json", "host-package-diff.json", "host-package-apply.json", "host-package-verify.json", "host-package.json"):
    if not artifact(apply_audit, name):
        errors.append(f"apply audit missing {name}")
stack_export = run_dir / "stack" / "stack-export.tgz"
if not stack_export.is_file() or stack_export.stat().st_size <= 0:
    errors.append("stack export bundle missing")

status = "succeeded" if not errors else "failed"
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
write_json(run_dir / "metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["lab.ssh-linux"],
})
write_json(run_dir / "target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "host/lab-ssh", "type": "host", "transport": "ssh"},
        {"id": "package/" + package, "type": "package", "manager": manager, "packageDigest": "sha256:" + hashlib.sha256(package.encode("utf-8")).hexdigest()},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-host-package-install-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "host.package.install",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "packageManager": manager,
    "package": package,
    "packageState": package_state,
    "applyRunId": apply_run_id,
    "repeatRunId": repeat_run_id,
    "deleteRunId": delete_run_id,
    "applyChanged": apply_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
    "deleteInstalled": (delete_receipt.get("after") or {}).get("installed"),
    "errors": errors,
    "verifiedAt": finished_at,
})
write_json(run_dir / "result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "finishedAt": finished_at,
    "nodeKind": "host.package.install",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
