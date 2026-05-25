#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-006.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test user/group and local scratch. Default.
  --no-cleanup           Leave scratch/user state for debugging.
  -h, --help             Show this help.

OPS-HOST-006 proves `host.user.manage` on lab.ssh-linux. It selects an unused
UID/GID, creates a temporary group and user through a stack node, verifies
UID/GID before/after evidence, repeats apply as a no-op, audits and exports the
stack run, then deletes through the stack and proves user/group cleanup.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux user E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-HOST-006"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-006.XXXXXX")"
stack_root="${scratch_root}/stack"
user_name="tqops006$$"
group_name="${user_name}"
home_dir="/tmp/${user_name}-home"
selected_id=""
selected_shell=""
apply_run_id=""
repeat_run_id=""
delete_run_id=""
cleanup_status="pending"

cleanup_lab_resources() {
  local status="succeeded"
  local scratch_status="not-requested"
  local user_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    if [[ -n "${user_name}" && -n "${group_name}" ]]; then
      ops_set_ssh_base_args
      if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "userdel -r '${user_name}' >/dev/null 2>&1 || true; groupdel '${group_name}' >/dev/null 2>&1 || true; rm -rf '${home_dir}'; ! getent passwd '${user_name}' >/dev/null 2>&1 && ! getent group '${group_name}' >/dev/null 2>&1 && test ! -e '${home_dir}'"; then
        user_status="removed"
      else
        user_status="failed"
        status="failed"
      fi
    else
      user_status="skipped"
    fi
  else
    scratch_status="skipped"
    user_status="skipped"
  fi
  cleanup_status="${status}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.ssh-linux" \
    user="${user_name}" \
    groupName="${group_name}" \
    uid="${selected_id}" \
    gid="${selected_id}" \
    home="${home_dir}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    userCleanup="${user_status}" \
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
ops_log "select unused UID/GID on lab host"
if ! selection="$(
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "sh -s" <<'REMOTE'
set -eu
for cmd in getent useradd usermod userdel groupadd groupmod groupdel; do
  command -v "$cmd" >/dev/null 2>&1 || exit 1
done
shell=/usr/sbin/nologin
if [ ! -x "$shell" ]; then
  shell=/bin/false
fi
for id in $(seq 24000 24999); do
  if ! getent passwd "$id" >/dev/null 2>&1 && ! getent group "$id" >/dev/null 2>&1; then
    printf '%s %s\n' "$id" "$shell"
    exit 0
  fi
done
exit 1
REMOTE
)"; then
  ops_fail "failed to select an unused UID/GID on lab host"
fi
selected_id="${selection%% *}"
selected_shell="${selection#* }"
[[ "${selected_id}" =~ ^[0-9]+$ && -n "${selected_shell}" && "${selected_shell}" != "${selected_id}" ]] || ops_fail "invalid UID/GID selection: ${selection}"
printf '%s\n' "${selection}" >"${OPS_RUN_DIR}/ssh/user-selection.txt"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create host.user.manage stack fixture"
python3 - "${stack_root}" "${user_name}" "${group_name}" "${selected_id}" "${selected_shell}" "${home_dir}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
user = sys.argv[2]
group = sys.argv[3]
uid = int(sys.argv[4])
shell = sys.argv[5]
home = sys.argv[6]
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-006
cli:
  inferDeps: false
nodes:
  - name: manage-user
    kind: host.user.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      user: {user!r}
      groupName: {group!r}
      userGroup: {group!r}
      uid: {uid}
      gid: {uid}
      home: {home!r}
      shell: {shell!r}
      comment: 'Torque OPS-HOST-006'
      createHome: true
      removeHome: true
      removeOnDelete: true
""",
    encoding="utf-8",
)
PY

ops_log "plan host.user.manage stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply host.user.manage stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover host.user.manage apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "getent passwd '${user_name}'; getent group '${group_name}'; id -u '${user_name}'; id -g '${user_name}'; test -d '${home_dir}'" >"${OPS_RUN_DIR}/ssh/user-state.apply"

ops_log "audit first host.user.manage run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-apply.json" 2>"${OPS_RUN_DIR}/stack/audit-apply.stderr"

ops_log "repeat apply to prove user no-op"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/repeat-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/repeat-apply.stderr"

repeat_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat host.user.manage run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete user/group through stack"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"

delete_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${delete_run_id}" ]] || ops_fail "failed to discover host.user.manage delete run ID"
printf '%s\n' "${delete_run_id}" >"${OPS_RUN_DIR}/stack/delete-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${delete_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-delete.json" 2>"${OPS_RUN_DIR}/stack/audit-delete.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "! getent passwd '${user_name}' >/dev/null 2>&1 && ! getent group '${group_name}' >/dev/null 2>&1 && test ! -e '${home_dir}'" >"${OPS_RUN_DIR}/ssh/user-delete-check.out" 2>"${OPS_RUN_DIR}/ssh/user-delete-check.stderr"

ops_log "verify host.user.manage evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${user_name}" \
  "${group_name}" \
  "${selected_id}" \
  "${home_dir}" \
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
user = sys.argv[5]
group = sys.argv[6]
uid = int(sys.argv[7])
home = sys.argv[8]
apply_run_id = sys.argv[9]
repeat_run_id = sys.argv[10]
delete_run_id = sys.argv[11]

def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path, doc):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("nodeId") == "host.user.manage/manage-user" and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
plan = read_json(run_dir / "stack" / "plan.json")
apply_audit = read_json(run_dir / "stack" / "audit-apply.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
delete_audit = read_json(run_dir / "stack" / "audit-delete.json")
remote_state = (run_dir / "ssh" / "user-state.apply").read_text(encoding="utf-8").splitlines()
nodes = {node.get("id"): node for node in plan.get("nodes", [])}
if "host.user.manage/manage-user" not in nodes:
    errors.append("plan missing host.user.manage/manage-user")
for label, audit in (("apply", apply_audit), ("repeat", repeat_audit), ("delete", delete_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")
apply_receipt = artifact(apply_audit, "host-user-apply.json")
apply_diff = artifact(apply_audit, "host-user-diff.json")
apply_verify = artifact(apply_audit, "host-user-verify.json")
repeat_receipt = artifact(repeat_audit, "host-user-apply.json")
delete_receipt = artifact(delete_audit, "host-user-apply.json")
after_user = (apply_receipt.get("after") or {}).get("user") or {}
after_group = (apply_receipt.get("after") or {}).get("group") or {}
delete_user = (delete_receipt.get("after") or {}).get("user") or {}
delete_group = (delete_receipt.get("after") or {}).get("group") or {}
if apply_receipt.get("status") != "succeeded" or apply_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed user create")
if after_user.get("exists") is not True or after_user.get("uid") != uid or after_user.get("gid") != uid:
    errors.append("first apply after user state does not prove requested UID/GID")
if after_group.get("exists") is not True or after_group.get("gid") != uid:
    errors.append("first apply after group state does not prove requested GID")
if (apply_diff.get("changes") or {}).get("user") is not True or (apply_diff.get("changes") or {}).get("group") is not True or apply_diff.get("diffQuality") != "exact":
    errors.append("first diff receipt did not record exact user/group diff")
if apply_verify.get("status") != "succeeded" or apply_verify.get("userExists") is not True or apply_verify.get("groupExists") is not True:
    errors.append("verify receipt did not prove user/group presence")
if apply_verify.get("uid") != uid or apply_verify.get("gid") != uid or apply_verify.get("groupGid") != uid:
    errors.append("verify receipt did not prove UID/GID")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
if delete_receipt.get("status") != "succeeded" or delete_user.get("exists") is not False or delete_group.get("exists") is not False:
    errors.append("delete receipt did not record user/group removal")
for name in ("host-user-observe.json", "host-user-plan.json", "host-user-diff.json", "host-user-apply.json", "host-user-verify.json", "host-user.json"):
    if not artifact(apply_audit, name):
        errors.append(f"apply audit missing {name}")
stack_export = run_dir / "stack" / "stack-export.tgz"
if not stack_export.is_file() or stack_export.stat().st_size <= 0:
    errors.append("stack export bundle missing")
if len(remote_state) < 4 or remote_state[-2] != str(uid) or remote_state[-1] != str(uid):
    errors.append(f"remote user state did not prove UID/GID: {remote_state}")

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
        {"id": "user/" + user, "type": "user", "uid": uid, "userDigest": "sha256:" + hashlib.sha256(user.encode("utf-8")).hexdigest()},
        {"id": "group/" + group, "type": "group", "gid": uid, "groupDigest": "sha256:" + hashlib.sha256(group.encode("utf-8")).hexdigest()},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-host-user-manage-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "host.user.manage",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "user": user,
    "groupName": group,
    "uid": uid,
    "gid": uid,
    "home": home,
    "applyRunId": apply_run_id,
    "repeatRunId": repeat_run_id,
    "deleteRunId": delete_run_id,
    "applyChanged": apply_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
    "deleteUserExists": delete_user.get("exists"),
    "deleteGroupExists": delete_group.get("exists"),
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
    "nodeKind": "host.user.manage",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
