#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-FC-MYSQL-001.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Delete lab resources after the run. Default.
  --no-cleanup         Leave Firecracker VMs running for debugging.
  -h, --help           Show this help.

STACK-FC-MYSQL-001 proves a stack-driven MySQL-compatible cluster on
Firecracker VMs. It plans, applies, reapplies for idempotence, audits, exports,
and optionally deletes the stack.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; stack defaults to this host
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live Firecracker/MySQL E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh
ops_require_cmd tar

repo_root="$(ops_repo_root)"
stack_root="${repo_root}/testdata/stack/e2e/22-firecracker-mysql-cluster"
remote_root="/var/lib/torque-firecracker-mysql/cluster"
ops_init_run "STACK-FC-MYSQL-001"
started_at="$(ops_utc_now)"
stack_applied=0
delete_status="succeeded"

finish() {
  local code=$?
  set +e
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  if [[ "${cleanup_enabled}" == "1" && "${stack_applied}" == "1" ]]; then
    ops_log "delete Firecracker MySQL stack"
    (
      cd "${repo_root}"
      ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
    ) >"${OPS_RUN_DIR}/cleanup/delete.jsonl" 2>"${OPS_RUN_DIR}/cleanup/delete.stderr"
    if [[ $? -eq 0 ]]; then
      delete_status="succeeded"
    else
      delete_status="failed"
      code=1
    fi
  fi
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    "apiVersion=torque.dev/e2e/v1" \
    "kind=OpsCleanupReceipt" \
    "taskId=${OPS_TASK_ID}" \
    "runId=${OPS_RUN_ID}" \
    "status=${delete_status}"
  ops_scan_for_secret_material "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/redaction-report.json" || code=1
  ops_write_manifest "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/manifest.json"
  ops_export_bundle "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}"
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/remote" "${OPS_RUN_DIR}/verification"
ops_write_json_object "${OPS_RUN_DIR}/metadata.json" \
  "apiVersion=torque.dev/e2e/v1" \
  "kind=OpsLabMetadata" \
  "taskId=${OPS_TASK_ID}" \
  "runId=${OPS_RUN_ID}" \
  "startedAt=${started_at}"
python3 - "${OPS_RUN_DIR}/target-snapshot.json" "${TORQUE_LAB_SSH}" <<'PY'
import json
import sys

path, lab_ssh = sys.argv[1:3]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetSnapshot",
    "targets": [
        {
            "id": "host/firecracker-mysql-lab",
            "type": "ssh-host",
            "transport": "ssh",
            "address": lab_ssh,
        },
        {
            "id": "vm/mysql-00..02",
            "type": "firecracker-vm-set",
            "transport": "ssh-via-lab-host",
            "count": 3,
            "subnet": "172.31.235.0/24",
        },
    ],
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
ops_write_json_object "${OPS_RUN_DIR}/decision.json" \
  "apiVersion=torque.dev/e2e/v1" \
  "kind=OpsDecision" \
  "taskId=${OPS_TASK_ID}" \
  "runId=${OPS_RUN_ID}" \
  "decision=allow" \
  "status=allowed" \
  "reason=explicit live Firecracker MySQL stack E2E confirmation"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "plan Firecracker MySQL stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply Firecracker MySQL stack"
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

ops_log "reapply Firecracker MySQL stack for idempotence"
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

ops_log "collect remote MySQL receipts"
ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/receipt.json'" >"${OPS_RUN_DIR}/remote/receipt.json"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/cluster-status.txt'" >"${OPS_RUN_DIR}/remote/cluster-status.txt" || true
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${remote_root}/verify-status.txt'" >"${OPS_RUN_DIR}/remote/verify-status.txt" || true

ops_log "audit and export stack run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${reapply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit.json" 2>"${OPS_RUN_DIR}/stack/audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${reapply_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "verify MySQL evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${OPS_RUN_DIR}/remote/receipt.json" \
  "${OPS_RUN_DIR}/stack/audit.json" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
receipt = json.loads(Path(sys.argv[5]).read_text(encoding="utf-8"))
audit = json.loads(Path(sys.argv[6]).read_text(encoding="utf-8"))
errors = []

if receipt.get("status") != "succeeded":
    errors.append("remote MySQL receipt failed")
if int(receipt.get("readyCount", 0)) != 3:
    errors.append("ready count mismatch")
if int(receipt.get("clusterSize", 0)) != 3:
    errors.append("cluster size mismatch")
if audit.get("status") != "succeeded":
    errors.append(f"stack audit status is {audit.get('status')}")
if not (run_dir / "stack" / "stack-export.tgz").is_file():
    errors.append("missing stack export bundle")

mysql_artifact = None
for artifact in audit.get("artifacts", []):
    if artifact.get("name") == "mysql-replication-verify.json":
        mysql_artifact = artifact
        break
if not mysql_artifact:
    errors.append("missing mysql.replication.verify artifact")
else:
    try:
        mysql_body = json.loads(mysql_artifact.get("body") or "{}")
        evidence = mysql_body.get("evidence") or mysql_body
        if mysql_body.get("nodeKind") != "mysql.replication.verify":
            errors.append("mysql verify artifact is not typed mysql.replication.verify")
        if mysql_body.get("status") != "succeeded" and evidence.get("status") != "succeeded":
            errors.append("mysql verify artifact did not succeed")
        if int(evidence.get("replicatedNodes", 0)) != 3:
            errors.append("mysql verify replicated node count mismatch")
    except Exception as exc:
        errors.append(f"failed to parse mysql verify artifact: {exc}")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackFirecrackerMySQL001Receipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if not errors else "failed",
    "startedAt": started_at,
    "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    "nodeCount": receipt.get("nodeCount"),
    "readyCount": receipt.get("readyCount"),
    "clusterSize": receipt.get("clusterSize"),
    "primaryIP": receipt.get("primaryIP"),
    "remoteSubnet": receipt.get("subnet"),
    "errors": errors,
}
(run_dir / "verification" / "receipt.json").write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
ops_write_json_object "${OPS_RUN_DIR}/result.json" \
  "apiVersion=torque.dev/e2e/v1" \
  "kind=OpsLabResult" \
  "taskId=${OPS_TASK_ID}" \
  "runId=${OPS_RUN_ID}" \
  "status=succeeded" \
  "finishedAt=$(ops_utc_now)"

ops_log "STACK-FC-MYSQL-001 passed"
