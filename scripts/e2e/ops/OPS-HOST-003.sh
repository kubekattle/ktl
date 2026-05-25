#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-003.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Delete remote and local scratch. Default.
  --no-cleanup           Leave scratch and remote path for debugging.
  -h, --help             Show this help.

OPS-HOST-003 proves `host.file.copy` on lab.ssh-linux. It plans and applies
one SSH-backed file copy, verifies checksum, owner/mode, backup evidence,
repeats the apply as a no-op, audits and exports the stack run, then deletes
through the stack and proves the original file was restored from backup.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux file copy E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-HOST-003"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-003.XXXXXX")"
stack_root="${scratch_root}/stack"
remote_root="/tmp/torque-ops-host-003-${OPS_RUN_ID}"
remote_file="${remote_root}/copied.conf"
remote_backup="${remote_file}.torque-backup"
apply_run_id=""
repeat_run_id=""
delete_run_id=""
cleanup_status="pending"

cleanup_lab_resources() {
  local status="succeeded"
  local scratch_status="not-requested"
  local ssh_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    ops_set_ssh_base_args
    if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "rm -rf '${remote_root}' && test ! -e '${remote_root}'"; then
      ssh_status="deleted"
    else
      ssh_status="failed"
      status="failed"
    fi
  else
    scratch_status="skipped"
    ssh_status="skipped"
  fi
  cleanup_status="${status}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.ssh-linux" \
    remoteRoot="${remote_root}" \
    remoteFile="${remote_file}" \
    remoteBackup="${remote_backup}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    ssh="${ssh_status}" \
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

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create host.file.copy stack fixture"
python3 - "${stack_root}" "${remote_file}" "${remote_backup}" "${OPS_RUN_ID}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
remote_file = sys.argv[2]
remote_backup = sys.argv[3]
run_id = sys.argv[4]
files = root / "files"
files.mkdir(parents=True, exist_ok=True)
files.joinpath("source.conf").write_text(
    f"task=OPS-HOST-003\nrun={run_id}\ncomponent=file-copy\n",
    encoding="utf-8",
)
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-003
cli:
  inferDeps: false
nodes:
  - name: copy-config
    kind: host.file.copy
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      sourcePath: files/source.conf
      path: {remote_file!r}
      mode: "0600"
      owner: root
      group: root
      backup: true
      backupPath: {remote_backup!r}
      restoreOnDelete: true
      validate: 'test -s "$TORQUE_FILE_COPY_TEMP_PATH" && grep -q "task=OPS-HOST-003" "$TORQUE_FILE_COPY_TEMP_PATH"'
""",
    encoding="utf-8",
)
PY

ops_set_ssh_base_args
ops_log "seed remote original file"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
  "mkdir -p '${remote_root}' && printf '%s\n%s\n%s\n' 'original=OPS-HOST-003' 'run=${OPS_RUN_ID}' 'component=restore-source' > '${remote_file}' && chmod 0644 '${remote_file}' && chown root:root '${remote_file}' && rm -f '${remote_backup}'" \
  >"${OPS_RUN_DIR}/ssh/seed.out" 2>"${OPS_RUN_DIR}/ssh/seed.stderr"

ops_log "plan host.file.copy stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply host.file.copy stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover host.file.copy apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_file}'" >"${OPS_RUN_DIR}/ssh/copied.conf"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "stat -c '%a %U %G %s' '${remote_file}'" >"${OPS_RUN_DIR}/ssh/copied.stat"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "sha256sum '${remote_file}' | awk '{print \$1}'" >"${OPS_RUN_DIR}/ssh/copied.sha256"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_backup}'" >"${OPS_RUN_DIR}/ssh/backup.conf"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "sha256sum '${remote_backup}' | awk '{print \$1}'" >"${OPS_RUN_DIR}/ssh/backup.sha256"

ops_log "audit first host.file.copy run"
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
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat host.file.copy run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete copied file through stack and restore backup"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"

delete_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${delete_run_id}" ]] || ops_fail "failed to discover host.file.copy delete run ID"
printf '%s\n' "${delete_run_id}" >"${OPS_RUN_DIR}/stack/delete-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${delete_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-delete.json" 2>"${OPS_RUN_DIR}/stack/audit-delete.stderr"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_file}'" >"${OPS_RUN_DIR}/ssh/restored.conf"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "stat -c '%a %U %G %s' '${remote_file}'" >"${OPS_RUN_DIR}/ssh/restored.stat"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "test ! -e '${remote_backup}'" >"${OPS_RUN_DIR}/ssh/backup-delete-check.out" 2>"${OPS_RUN_DIR}/ssh/backup-delete-check.stderr"

ops_log "verify host.file.copy evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${remote_file}" \
  "${remote_backup}" \
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
remote_file = sys.argv[5]
remote_backup = sys.argv[6]
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
        if item.get("nodeId") == "host.file.copy/copy-config" and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
plan = read_json(run_dir / "stack" / "plan.json")
apply_audit = read_json(run_dir / "stack" / "audit-apply.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
delete_audit = read_json(run_dir / "stack" / "audit-delete.json")
copied = (run_dir / "ssh" / "copied.conf").read_text(encoding="utf-8")
backup = (run_dir / "ssh" / "backup.conf").read_text(encoding="utf-8")
restored = (run_dir / "ssh" / "restored.conf").read_text(encoding="utf-8")
copied_stat = (run_dir / "ssh" / "copied.stat").read_text(encoding="utf-8").strip().split()
restored_stat = (run_dir / "ssh" / "restored.stat").read_text(encoding="utf-8").strip().split()
copied_sha = (run_dir / "ssh" / "copied.sha256").read_text(encoding="utf-8").strip()
backup_sha = (run_dir / "ssh" / "backup.sha256").read_text(encoding="utf-8").strip()
expected = f"task=OPS-HOST-003\nrun={run_id}\ncomponent=file-copy\n"
original = f"original=OPS-HOST-003\nrun={run_id}\ncomponent=restore-source\n"
if copied != expected:
    errors.append("copied content mismatch")
if backup != original:
    errors.append("backup content mismatch")
if restored != original:
    errors.append("restored content mismatch")
if not copied_stat or copied_stat[0] != "600":
    errors.append(f"copied mode is {copied_stat[0] if copied_stat else 'missing'}, want 600")
if len(copied_stat) < 3 or copied_stat[1] != "root" or copied_stat[2] != "root":
    errors.append(f"copied owner/group is {' '.join(copied_stat[1:3]) if len(copied_stat) >= 3 else 'missing'}, want root root")
if not restored_stat or restored_stat[0] != "644":
    errors.append(f"restored mode is {restored_stat[0] if restored_stat else 'missing'}, want 644")
if copied_sha != hashlib.sha256(expected.encode("utf-8")).hexdigest():
    errors.append("copied sha256 mismatch")
if backup_sha != hashlib.sha256(original.encode("utf-8")).hexdigest():
    errors.append("backup sha256 mismatch")
nodes = {node.get("id"): node for node in plan.get("nodes", [])}
if "host.file.copy/copy-config" not in nodes:
    errors.append("plan missing host.file.copy/copy-config")
for label, audit in (("apply", apply_audit), ("repeat", repeat_audit), ("delete", delete_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")
apply_receipt = artifact(apply_audit, "host-file-copy-apply.json")
apply_diff = artifact(apply_audit, "host-file-copy-diff.json")
apply_verify = artifact(apply_audit, "host-file-copy-verify.json")
repeat_receipt = artifact(repeat_audit, "host-file-copy-apply.json")
delete_receipt = artifact(delete_audit, "host-file-copy-apply.json")
if apply_receipt.get("status") != "succeeded" or apply_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed copy")
if not ((apply_receipt.get("backup") or {}).get("exists")):
    errors.append("first apply receipt did not record backup state")
if (apply_diff.get("changes") or {}).get("content") is not True or apply_diff.get("diffQuality") != "exact":
    errors.append("first diff receipt did not record exact content diff")
if apply_verify.get("status") != "succeeded" or apply_verify.get("actualDigest") != apply_verify.get("desiredDigest"):
    errors.append("verify receipt did not match actual/desired digest")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
if delete_receipt.get("status") != "succeeded" or delete_receipt.get("restored") is not True:
    errors.append("delete receipt did not record backup restore")
for name in ("host-file-copy-observe.json", "host-file-copy-plan.json", "host-file-copy-diff.json", "host-file-copy-apply.json", "host-file-copy-verify.json", "host-file-copy.json"):
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
        {"id": "file/source-conf", "type": "file", "sha256": hashlib.sha256(expected.encode("utf-8")).hexdigest()},
        {"id": "file/copied-conf", "type": "file", "pathDigest": hashlib.sha256(remote_file.encode("utf-8")).hexdigest()},
        {"id": "file/backup-conf", "type": "file", "pathDigest": hashlib.sha256(remote_backup.encode("utf-8")).hexdigest()},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-host-file-copy-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "host.file.copy",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "applyRunId": apply_run_id,
    "repeatRunId": repeat_run_id,
    "deleteRunId": delete_run_id,
    "copiedMode": copied_stat[0] if copied_stat else "",
    "copiedOwner": copied_stat[1] if len(copied_stat) > 1 else "",
    "copiedGroup": copied_stat[2] if len(copied_stat) > 2 else "",
    "copiedSha256": copied_sha,
    "backupSha256": backup_sha,
    "applyChanged": apply_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
    "deleteRestored": delete_receipt.get("restored"),
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
    "nodeKind": "host.file.copy",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
