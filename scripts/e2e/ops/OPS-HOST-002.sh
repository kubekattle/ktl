#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-002.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Delete the rendered file and local scratch. Default.
  --no-cleanup           Leave scratch and remote path for debugging.
  -h, --help             Show this help.

OPS-HOST-002 proves `host.file.render` on lab.ssh-linux. It plans and applies
one SSH-backed file render, verifies rendered content, mode, digest evidence,
repeats the apply as a no-op, audits and exports the stack run, then deletes
the rendered file.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux file render E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-HOST-002"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-002.XXXXXX")"
stack_root="${scratch_root}/stack"
remote_root="/tmp/torque-ops-host-002-${OPS_RUN_ID}"
remote_file="${remote_root}/rendered.conf"
apply_run_id=""
repeat_run_id=""
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

ops_log "create host.file.render stack fixture"
python3 - "${stack_root}" "${remote_file}" "${OPS_RUN_ID}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
remote_file = sys.argv[2]
run_id = sys.argv[3]
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-002
cli:
  inferDeps: false
nodes:
  - name: render-config
    kind: host.file.render
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      path: {remote_file!r}
      mode: "0600"
      owner: root
      group: root
      template: |
        task={{{{ .Task }}}}
        run={{{{ .RunID }}}}
        component={{{{ .Component }}}}
      data:
        Task: OPS-HOST-002
        RunID: {run_id}
        Component: file-render
      validate: 'test -s "$TORQUE_FILE_RENDER_TEMP_PATH" && grep -q "task=OPS-HOST-002" "$TORQUE_FILE_RENDER_TEMP_PATH"'
      removeOnDelete: true
""",
    encoding="utf-8",
)
PY

ops_log "plan host.file.render stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply host.file.render stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover host.file.render apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_file}'" >"${OPS_RUN_DIR}/ssh/rendered.conf"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "stat -c '%a %U %G %s' '${remote_file}'" >"${OPS_RUN_DIR}/ssh/rendered.stat"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "sha256sum '${remote_file}' | awk '{print \$1}'" >"${OPS_RUN_DIR}/ssh/rendered.sha256"

ops_log "audit first host.file.render run"
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
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat host.file.render run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete rendered file through stack"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "test ! -e '${remote_file}'" >"${OPS_RUN_DIR}/ssh/delete-check.out" 2>"${OPS_RUN_DIR}/ssh/delete-check.stderr"

ops_log "verify host.file.render evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${remote_file}" \
  "${apply_run_id}" \
  "${repeat_run_id}" <<'PY'
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
apply_run_id = sys.argv[6]
repeat_run_id = sys.argv[7]

def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path, doc):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("nodeId") == "host.file.render/render-config" and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
plan = read_json(run_dir / "stack" / "plan.json")
apply_audit = read_json(run_dir / "stack" / "audit-apply.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
rendered = (run_dir / "ssh" / "rendered.conf").read_text(encoding="utf-8")
stat = (run_dir / "ssh" / "rendered.stat").read_text(encoding="utf-8").strip().split()
sha = (run_dir / "ssh" / "rendered.sha256").read_text(encoding="utf-8").strip()
expected = f"task=OPS-HOST-002\nrun={run_id}\ncomponent=file-render\n"
if rendered != expected:
    errors.append("rendered content mismatch")
if not stat or stat[0] != "600":
    errors.append(f"mode is {stat[0] if stat else 'missing'}, want 600")
if len(stat) < 3 or stat[1] != "root" or stat[2] != "root":
    errors.append(f"owner/group is {' '.join(stat[1:3]) if len(stat) >= 3 else 'missing'}, want root root")
if sha != hashlib.sha256(expected.encode("utf-8")).hexdigest():
    errors.append("remote sha256 mismatch")
nodes = {node.get("id"): node for node in plan.get("nodes", [])}
if "host.file.render/render-config" not in nodes:
    errors.append("plan missing host.file.render/render-config")
for label, audit in (("apply", apply_audit), ("repeat", repeat_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")
apply_receipt = artifact(apply_audit, "host-file-apply.json")
apply_diff = artifact(apply_audit, "host-file-diff.json")
apply_verify = artifact(apply_audit, "host-file-verify.json")
repeat_receipt = artifact(repeat_audit, "host-file-apply.json")
if apply_receipt.get("status") != "succeeded" or apply_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed render")
if (apply_diff.get("changes") or {}).get("content") is not True or apply_diff.get("diffQuality") != "exact":
    errors.append("first diff receipt did not record exact content diff")
if apply_verify.get("status") != "succeeded" or apply_verify.get("actualDigest") != apply_verify.get("desiredDigest"):
    errors.append("verify receipt did not match actual/desired digest")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
for name in ("host-file-observe.json", "host-file-plan.json", "host-file-diff.json", "host-file-apply.json", "host-file-verify.json", "host-file-render.json"):
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
        {"id": "file/rendered-conf", "type": "file", "pathDigest": hashlib.sha256(remote_file.encode("utf-8")).hexdigest()},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-host-file-render-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "host.file.render",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "applyRunId": apply_run_id,
    "repeatRunId": repeat_run_id,
    "renderedMode": stat[0] if stat else "",
    "renderedOwner": stat[1] if len(stat) > 1 else "",
    "renderedGroup": stat[2] if len(stat) > 2 else "",
    "renderedSha256": sha,
    "applyChanged": apply_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
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
    "nodeKind": "host.file.render",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
