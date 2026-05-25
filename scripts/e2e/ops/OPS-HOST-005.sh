#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-HOST-005.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test unit and local scratch. Default.
  --no-cleanup           Leave scratch/unit state for debugging.
  -h, --help             Show this help.

OPS-HOST-005 proves `host.service.manage` on lab.ssh-linux. It installs an
isolated systemd test unit, starts and enables it through a stack node, verifies
before/after service evidence, repeats apply as a no-op, proves restart, then
deletes through the stack and proves the unit was stopped and disabled.

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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux service E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-HOST-005"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-host-005.XXXXXX")"
start_stack_root="${scratch_root}/start-stack"
restart_stack_root="${scratch_root}/restart-stack"
unit_suffix="$(printf '%s' "${OPS_RUN_ID}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '-')"
unit_name="torque-ops-host-005-${unit_suffix}.service"
unit_path="/etc/systemd/system/${unit_name}"
start_run_id=""
repeat_run_id=""
restart_run_id=""
delete_run_id=""
cleanup_status="pending"

cleanup_lab_resources() {
  local status="succeeded"
  local scratch_status="not-requested"
  local service_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    if [[ -n "${unit_name}" ]]; then
      ops_set_ssh_base_args
      if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "systemctl stop '${unit_name}' >/dev/null 2>&1 || true; systemctl disable '${unit_name}' >/dev/null 2>&1 || true; rm -f '${unit_path}'; systemctl daemon-reload >/dev/null 2>&1 || true; systemctl reset-failed '${unit_name}' >/dev/null 2>&1 || true; test ! -e '${unit_path}'"; then
        service_status="removed"
      else
        service_status="failed"
        status="failed"
      fi
    else
      service_status="skipped"
    fi
  else
    scratch_status="skipped"
    service_status="skipped"
  fi
  cleanup_status="${status}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.ssh-linux" \
    service="${unit_name}" \
    unitPath="${unit_path}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    serviceCleanup="${service_status}" \
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
ops_log "verify systemd service manager on lab host"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "command -v systemctl >/dev/null 2>&1 && systemctl --version >/dev/null 2>&1 && test -d /run/systemd/system" >"${OPS_RUN_DIR}/ssh/systemd-preflight.out" 2>"${OPS_RUN_DIR}/ssh/systemd-preflight.stderr"

ops_log "install isolated test systemd unit"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "sh -s" "${unit_name}" "${unit_path}" >"${OPS_RUN_DIR}/ssh/unit-setup.out" 2>"${OPS_RUN_DIR}/ssh/unit-setup.stderr" <<'REMOTE'
set -eu
unit_name="$1"
unit_path="$2"
systemctl stop "$unit_name" >/dev/null 2>&1 || true
systemctl disable "$unit_name" >/dev/null 2>&1 || true
cat >"$unit_path" <<'UNIT'
[Unit]
Description=Torque OPS-HOST-005 isolated test service

[Service]
Type=simple
ExecStart=/bin/sh -c 'trap "exit 0" TERM INT; while :; do sleep 60; done'
Restart=no

[Install]
WantedBy=multi-user.target
UNIT
chmod 0644 "$unit_path"
systemctl daemon-reload
systemctl reset-failed "$unit_name" >/dev/null 2>&1 || true
systemctl show --property=LoadState --property=ActiveState --property=SubState --property=UnitFileState "$unit_name"
REMOTE

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create host.service.manage stack fixtures"
python3 - "${start_stack_root}" "${restart_stack_root}" "${unit_name}" <<'PY'
from pathlib import Path
import sys

start_root = Path(sys.argv[1])
restart_root = Path(sys.argv[2])
unit = sys.argv[3]
start_root.mkdir(parents=True, exist_ok=True)
restart_root.mkdir(parents=True, exist_ok=True)
start_root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-005-start
cli:
  inferDeps: false
nodes:
  - name: start-service
    kind: host.service.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      serviceManager: systemd
      service: {unit!r}
      state: started
      enabled: true
      stopOnDelete: true
      disableOnDelete: true
""",
    encoding="utf-8",
)
restart_root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-host-005-restart
cli:
  inferDeps: false
nodes:
  - name: restart-service
    kind: host.service.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      serviceManager: systemd
      service: {unit!r}
      state: restarted
""",
    encoding="utf-8",
)
PY

ops_log "plan host.service.manage start stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${start_stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan-start.json" 2>"${OPS_RUN_DIR}/stack/plan-start.stderr"

ops_log "apply host.service.manage start stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${start_stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply-start.jsonl" 2>"${OPS_RUN_DIR}/stack/apply-start.stderr"

start_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${start_stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${start_run_id}" ]] || ops_fail "failed to discover host.service.manage start run ID"
printf '%s\n' "${start_run_id}" >"${OPS_RUN_DIR}/stack/start-run-id.txt"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "systemctl show --property=LoadState --property=ActiveState --property=SubState --property=UnitFileState '${unit_name}'" >"${OPS_RUN_DIR}/ssh/service-state.apply"

ops_log "audit first host.service.manage run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${start_stack_root}" --run-id "${start_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-start.json" 2>"${OPS_RUN_DIR}/stack/audit-start.stderr"

ops_log "repeat apply to prove service no-op"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${start_stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/repeat-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/repeat-apply.stderr"

repeat_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${start_stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat host.service.manage run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${start_stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${start_stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "apply host.service.manage restart stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${restart_stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan-restart.json" 2>"${OPS_RUN_DIR}/stack/plan-restart.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${restart_stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply-restart.jsonl" 2>"${OPS_RUN_DIR}/stack/apply-restart.stderr"

restart_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${restart_stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${restart_run_id}" ]] || ops_fail "failed to discover host.service.manage restart run ID"
printf '%s\n' "${restart_run_id}" >"${OPS_RUN_DIR}/stack/restart-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${restart_stack_root}" --run-id "${restart_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-restart.json" 2>"${OPS_RUN_DIR}/stack/audit-restart.stderr"

ops_log "delete service state through stack"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${start_stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"

delete_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${start_stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${delete_run_id}" ]] || ops_fail "failed to discover host.service.manage delete run ID"
printf '%s\n' "${delete_run_id}" >"${OPS_RUN_DIR}/stack/delete-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${start_stack_root}" --run-id "${delete_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-delete.json" 2>"${OPS_RUN_DIR}/stack/audit-delete.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "systemctl show --property=LoadState --property=ActiveState --property=SubState --property=UnitFileState '${unit_name}'" >"${OPS_RUN_DIR}/ssh/service-state.delete"

ops_log "verify host.service.manage evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${unit_name}" \
  "${start_run_id}" \
  "${repeat_run_id}" \
  "${restart_run_id}" \
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
unit = sys.argv[5]
start_run_id = sys.argv[6]
repeat_run_id = sys.argv[7]
restart_run_id = sys.argv[8]
delete_run_id = sys.argv[9]

def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path, doc):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, node_id, name):
    for item in audit.get("artifacts", []):
        if item.get("nodeId") == node_id and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

def service_props(path):
    out = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            out[key] = value
    return out

errors = []
start_plan = read_json(run_dir / "stack" / "plan-start.json")
restart_plan = read_json(run_dir / "stack" / "plan-restart.json")
start_audit = read_json(run_dir / "stack" / "audit-start.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
restart_audit = read_json(run_dir / "stack" / "audit-restart.json")
delete_audit = read_json(run_dir / "stack" / "audit-delete.json")
apply_state = service_props(run_dir / "ssh" / "service-state.apply")
delete_state = service_props(run_dir / "ssh" / "service-state.delete")

start_node = "host.service.manage/start-service"
restart_node = "host.service.manage/restart-service"
if start_node not in {node.get("id") for node in start_plan.get("nodes", [])}:
    errors.append("plan missing host.service.manage/start-service")
if restart_node not in {node.get("id") for node in restart_plan.get("nodes", [])}:
    errors.append("plan missing host.service.manage/restart-service")
for label, audit in (("start", start_audit), ("repeat", repeat_audit), ("restart", restart_audit), ("delete", delete_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")

start_receipt = artifact(start_audit, start_node, "host-service-apply.json")
start_diff = artifact(start_audit, start_node, "host-service-diff.json")
start_verify = artifact(start_audit, start_node, "host-service-verify.json")
repeat_receipt = artifact(repeat_audit, start_node, "host-service-apply.json")
restart_receipt = artifact(restart_audit, restart_node, "host-service-apply.json")
delete_receipt = artifact(delete_audit, start_node, "host-service-apply.json")
if start_receipt.get("status") != "succeeded" or start_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed service start")
if (start_receipt.get("after") or {}).get("active") is not True:
    errors.append("first apply after state is not active")
if (start_receipt.get("after") or {}).get("enabled") is not True:
    errors.append("first apply after state is not enabled")
if start_receipt.get("serviceManager") != "systemd":
    errors.append(f"service manager receipt is {start_receipt.get('serviceManager')}, want systemd")
if (start_diff.get("changes") or {}).get("active") is not True or (start_diff.get("changes") or {}).get("enabled") is not True or start_diff.get("diffQuality") != "exact":
    errors.append("first diff receipt did not record exact active/enabled diff")
if start_verify.get("status") != "succeeded" or start_verify.get("active") is not True or start_verify.get("enabled") is not True:
    errors.append("verify receipt did not prove active enabled service")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
if restart_receipt.get("status") != "succeeded" or restart_receipt.get("changed") is not True or not (restart_receipt.get("changes") or {}).get("restart"):
    errors.append("restart receipt did not record restart change")
if delete_receipt.get("status") != "succeeded" or (delete_receipt.get("after") or {}).get("active") is not False or (delete_receipt.get("after") or {}).get("enabled") is not False:
    errors.append("delete receipt did not record service stop and disable")
for name in ("host-service-observe.json", "host-service-plan.json", "host-service-diff.json", "host-service-apply.json", "host-service-verify.json", "host-service.json"):
    if not artifact(start_audit, start_node, name):
        errors.append(f"start audit missing {name}")
stack_export = run_dir / "stack" / "stack-export.tgz"
if not stack_export.is_file() or stack_export.stat().st_size <= 0:
    errors.append("stack export bundle missing")
if apply_state.get("ActiveState") != "active" or apply_state.get("UnitFileState") != "enabled":
    errors.append(f"remote apply service state unexpected: {apply_state}")
if delete_state.get("ActiveState") == "active" or delete_state.get("UnitFileState") == "enabled":
    errors.append(f"remote delete service state unexpected: {delete_state}")

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
        {"id": "service/" + unit, "type": "service", "manager": "systemd", "serviceDigest": "sha256:" + hashlib.sha256(unit.encode("utf-8")).hexdigest()},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-host-service-manage-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "host.service.manage",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "service": unit,
    "serviceManager": "systemd",
    "startRunId": start_run_id,
    "repeatRunId": repeat_run_id,
    "restartRunId": restart_run_id,
    "deleteRunId": delete_run_id,
    "applyChanged": start_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
    "restartChanged": restart_receipt.get("changed"),
    "deleteActive": (delete_receipt.get("after") or {}).get("active"),
    "deleteEnabled": (delete_receipt.get("after") or {}).get("enabled"),
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
    "nodeKind": "host.service.manage",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
