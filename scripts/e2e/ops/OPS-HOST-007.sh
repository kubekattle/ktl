#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-007.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test cron file and local scratch. Default.
  --no-cleanup           Leave scratch/cron state for debugging.
  -h, --help             Show this help.

OPS-HOST-007 proves `host.cron.manage` on lab.ssh-linux. It creates a temporary
cron.d entry through a stack node, verifies exact digest diff evidence, repeats
apply as a no-op, audits and exports the run, then deletes through the stack and
proves the cron file was removed.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux cron E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-HOST-007"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-007.XXXXXX")"
stack_root="${scratch_root}/stack"
cron_name="tqops007$$"
cron_path="/etc/cron.d/${cron_name}"
cron_command="/bin/sh -c 'printf OPS-HOST-007 >/tmp/${cron_name}.proof'"
apply_run_id=""
repeat_run_id=""
delete_run_id=""
cleanup_status="pending"

cleanup_lab_resources() {
  local status="succeeded"
  local scratch_status="not-requested"
  local cron_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    if [[ -n "${cron_path}" ]]; then
      ops_set_ssh_base_args
      if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "rm -f '${cron_path}' '/tmp/${cron_name}.proof'; test ! -e '${cron_path}'"; then
        cron_status="removed"
      else
        cron_status="failed"
        status="failed"
      fi
    else
      cron_status="skipped"
    fi
  else
    scratch_status="skipped"
    cron_status="skipped"
  fi
  cleanup_status="${status}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.ssh-linux" \
    cronName="${cron_name}" \
    path="${cron_path}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    cronCleanup="${cron_status}" \
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
ops_log "verify cron.d path on lab host"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "test -d /etc/cron.d && rm -f '${cron_path}' '/tmp/${cron_name}.proof'" >"${OPS_RUN_DIR}/ssh/cron-preflight.out" 2>"${OPS_RUN_DIR}/ssh/cron-preflight.stderr"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create host.cron.manage stack fixture"
python3 - "${stack_root}" "${cron_name}" "${cron_path}" "${cron_command}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
name = sys.argv[2]
path = sys.argv[3]
command = sys.argv[4]
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-007
cli:
  inferDeps: false
nodes:
  - name: manage-cron
    kind: host.cron.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      path: {path!r}
      cronName: {name!r}
      schedule: '* * * * *'
      cronUser: root
      cronCommand: {command!r}
      mode: '0644'
      removeOnDelete: true
""",
    encoding="utf-8",
)
PY

ops_log "plan host.cron.manage stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply host.cron.manage stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover host.cron.manage apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "test -f '${cron_path}' && stat -c '%a %U %G %s' '${cron_path}' && sha256sum '${cron_path}'" >"${OPS_RUN_DIR}/ssh/cron-state.apply"

ops_log "audit first host.cron.manage run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-apply.json" 2>"${OPS_RUN_DIR}/stack/audit-apply.stderr"

ops_log "repeat apply to prove cron no-op"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/repeat-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/repeat-apply.stderr"

repeat_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat host.cron.manage run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete cron through stack"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"

delete_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${delete_run_id}" ]] || ops_fail "failed to discover host.cron.manage delete run ID"
printf '%s\n' "${delete_run_id}" >"${OPS_RUN_DIR}/stack/delete-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${delete_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-delete.json" 2>"${OPS_RUN_DIR}/stack/audit-delete.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "test ! -e '${cron_path}'" >"${OPS_RUN_DIR}/ssh/cron-delete-check.out" 2>"${OPS_RUN_DIR}/ssh/cron-delete-check.stderr"

ops_log "verify host.cron.manage evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${cron_name}" \
  "${cron_path}" \
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
cron_name = sys.argv[5]
cron_path = sys.argv[6]
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
        if item.get("nodeId") == "host.cron.manage/manage-cron" and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
plan = read_json(run_dir / "stack" / "plan.json")
apply_audit = read_json(run_dir / "stack" / "audit-apply.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
delete_audit = read_json(run_dir / "stack" / "audit-delete.json")
remote_state = (run_dir / "ssh" / "cron-state.apply").read_text(encoding="utf-8").splitlines()
nodes = {node.get("id"): node for node in plan.get("nodes", [])}
if "host.cron.manage/manage-cron" not in nodes:
    errors.append("plan missing host.cron.manage/manage-cron")
for label, audit in (("apply", apply_audit), ("repeat", repeat_audit), ("delete", delete_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")
apply_receipt = artifact(apply_audit, "host-cron-apply.json")
apply_diff = artifact(apply_audit, "host-cron-diff.json")
apply_verify = artifact(apply_audit, "host-cron-verify.json")
repeat_receipt = artifact(repeat_audit, "host-cron-apply.json")
delete_receipt = artifact(delete_audit, "host-cron-apply.json")
after = apply_receipt.get("after") or {}
delete_after = delete_receipt.get("after") or {}
if apply_receipt.get("status") != "succeeded" or apply_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed cron create")
if after.get("exists") is not True or not str(after.get("sha256") or "").startswith("sha256:"):
    errors.append("first apply after state does not prove cron file digest")
if (apply_diff.get("changes") or {}).get("exists") is not True or apply_diff.get("diffQuality") != "exact":
    errors.append("first diff receipt did not record exact cron create diff")
if apply_verify.get("status") != "succeeded" or apply_verify.get("exists") is not True:
    errors.append("verify receipt did not prove cron presence")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
if delete_receipt.get("status") != "succeeded" or delete_after.get("exists") is not False:
    errors.append("delete receipt did not record cron removal")
for name in ("host-cron-observe.json", "host-cron-plan.json", "host-cron-diff.json", "host-cron-apply.json", "host-cron-verify.json", "host-cron.json"):
    if not artifact(apply_audit, name):
        errors.append(f"apply audit missing {name}")
stack_export = run_dir / "stack" / "stack-export.tgz"
if not stack_export.is_file() or stack_export.stat().st_size <= 0:
    errors.append("stack export bundle missing")
remote_mode = remote_state[0].split()[0] if remote_state else ""
if len(remote_state) < 2 or remote_mode not in {"644", "0644"} or cron_path not in remote_state[1]:
    errors.append(f"remote cron state did not prove mode/hash/path: {remote_state}")

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
        {"id": "cron/" + cron_name, "type": "cron", "pathDigest": "sha256:" + hashlib.sha256(cron_path.encode("utf-8")).hexdigest()},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-host-cron-manage-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "host.cron.manage",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "cronName": cron_name,
    "path": cron_path,
    "applyRunId": apply_run_id,
    "repeatRunId": repeat_run_id,
    "deleteRunId": delete_run_id,
    "applyChanged": apply_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
    "deleteExists": delete_after.get("exists"),
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
    "nodeKind": "host.cron.manage",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
