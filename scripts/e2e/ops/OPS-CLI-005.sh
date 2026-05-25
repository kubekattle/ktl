#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-CLI-005.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --source-run-dir DIR   Reuse an existing OPS-CLI-004b evidence run directory.
  --cleanup              Pass cleanup to the nested Firecracker run. Default.
  --no-cleanup           Leave the nested Firecracker VM running for debugging.
  -h, --help             Show this help.

OPS-CLI-005 proves `torque stack audit` for ops runs. It audits the exported
safe run bundle from OPS-CLI-004b with --from-bundle, then audits a tampered
copy whose host command verify receipt is missing and whose execute receipt
contains raw sensitive output. The audit must be read-only, pass the safe
bundle, and fail the tampered bundle.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

cleanup_enabled=1
source_run_dir=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --source-run-dir)
      [[ $# -ge 2 ]] || ops_fail "--source-run-dir requires a value"
      source_run_dir="$2"
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
ops_require_cmd tar

repo_root="$(ops_repo_root)"
ops_init_run "OPS-CLI-005"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-cli-005.XXXXXX")"
safe_export=""
tampered_export=""
safe_audit_status=""
tampered_audit_status=""

finish() {
  local code=$?
  trap - EXIT
  rm -rf "${scratch_root}"

  set +e
  python3 - \
    "${OPS_RUN_DIR}" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${started_at}" \
    "${TORQUE_LAB_SSH}" \
    "${source_run_dir}" \
    "${safe_export}" \
    "${tampered_export}" \
    "${safe_audit_status}" \
    "${tampered_audit_status}" \
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
    source_run_dir,
    safe_export,
    tampered_export,
    safe_audit_status,
    tampered_audit_status,
    exit_code,
) = sys.argv[1:12]
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

proof_path = run / "verification" / "ops-cli-005-proof.json"
proof = load("verification/ops-cli-005-proof.json")
errors = list(proof.get("errors") or [])
if not proof_path.is_file():
    errors.append("ops-cli-005 proof missing")
if not source_run_dir:
    errors.append("source OPS-CLI-004b run directory missing")
if not safe_export:
    errors.append("safe source export path missing")
if not tampered_export:
    errors.append("tampered export path missing")
if not safe_audit_status:
    errors.append("safe audit status missing")
if not tampered_audit_status:
    errors.append("tampered audit status missing")
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
status = "succeeded" if code == 0 and not errors else "failed"
write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["lab.vm", "lab.ssh-linux"],
    "host": lab_ssh,
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "host/firecracker-lab", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {"id": "stack-run/ops-cli-004b-safe", "type": "stack-run-bundle", "path": safe_export},
        {"id": "stack-run/ops-cli-005-tampered", "type": "stack-run-bundle", "path": tampered_export},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "verify-ops-run-bundles-read-only",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "sourceRunDir": source_run_dir,
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "safeAuditStatus": safe_audit_status,
    "tamperedAuditStatus": tampered_audit_status,
    "sourceRunDir": source_run_dir,
    "errors": errors,
    "verifiedAt": finished_at,
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded",
    "mode": "nested-run-handled-cleanup",
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "finishedAt": finished_at,
    "sourceRunDir": source_run_dir,
})
if status != "succeeded":
    sys.exit(1)
PY
  local receipt_code=$?
  set -e
  if [[ ${receipt_code} -ne 0 ]]; then
    code=1
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/source" "${OPS_RUN_DIR}/audit" "${OPS_RUN_DIR}/tamper" "${OPS_RUN_DIR}/verification"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

if [[ -z "${source_run_dir}" ]]; then
  ops_log "run OPS-CLI-004b Firecracker replay evidence"
  nested_cleanup="--cleanup"
  if [[ "${cleanup_enabled}" == "0" ]]; then
    nested_cleanup="--no-cleanup"
  fi
  set +e
  (
    cd "${repo_root}"
    env -u OPS_TASK_ID -u OPS_RUN_ID -u OPS_RUN_DIR -u OPS_BUNDLE_PATH -u OPS_SECRET_CANARY \
      TORQUE_OPS_E2E_CONFIRM="${TORQUE_OPS_E2E_CONFIRM}" \
      TORQUE_LAB_SSH="${TORQUE_LAB_SSH}" \
      OPS_EVIDENCE_ROOT="${OPS_EVIDENCE_ROOT}" \
      scripts/e2e/ops/OPS-CLI-004b.sh --evidence-root "${OPS_EVIDENCE_ROOT}" "${nested_cleanup}"
  ) >"${OPS_RUN_DIR}/source/ops-cli-004b.out" 2>"${OPS_RUN_DIR}/source/ops-cli-004b.stderr"
  nested_code=$?
  set -e
  printf '%s\n' "${nested_code}" >"${OPS_RUN_DIR}/source/ops-cli-004b.exit"
  if [[ "${nested_code}" -ne 0 ]]; then
    ops_fail "OPS-CLI-004b source run failed; see ${OPS_RUN_DIR}/source/ops-cli-004b.stderr"
  fi
  source_run_dir="$(
    python3 - "${OPS_RUN_DIR}/source/ops-cli-004b.out" "${OPS_RUN_DIR}/source/ops-cli-004b.stderr" <<'PY'
import re
import sys
from pathlib import Path

for path in map(Path, sys.argv[1:]):
    if not path.is_file():
        continue
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        match = re.search(r"evidence:\s+(.+)$", line)
        if match:
            print(match.group(1).strip())
            raise SystemExit(0)
raise SystemExit(1)
PY
  )" || ops_fail "could not parse OPS-CLI-004b evidence path"
else
  ops_log "reuse OPS-CLI-004b evidence at ${source_run_dir}"
fi

safe_export="${source_run_dir}/stack/safe-export.tgz"
[[ -s "${safe_export}" ]] || ops_fail "missing safe OPS-CLI-004b export: ${safe_export}"
printf '%s\n' "${source_run_dir}" >"${OPS_RUN_DIR}/source/source-run-dir.txt"
printf '%s\n' "${safe_export}" >"${OPS_RUN_DIR}/source/safe-export.txt"

ops_log "audit safe ops run bundle read-only"
(
  cd "${repo_root}"
  ./bin/torque stack audit --from-bundle "${safe_export}" --output json --include-plan --include-events --include-artifacts
) >"${OPS_RUN_DIR}/audit/safe-from-bundle.json" 2>"${OPS_RUN_DIR}/audit/safe-from-bundle.stderr"
safe_audit_status="passed"

ops_log "create tampered ops run bundle"
tampered_export="${OPS_RUN_DIR}/tamper/tampered-safe-export.tgz"
python3 - "${safe_export}" "${scratch_root}/tampered" "${tampered_export}" <<'PY'
import hashlib
import json
import shutil
import sqlite3
import sys
import tarfile
from pathlib import Path

source = Path(sys.argv[1])
work = Path(sys.argv[2])
out = Path(sys.argv[3])
if work.exists():
    shutil.rmtree(work)
work.mkdir(parents=True)
with tarfile.open(source, "r:gz") as tar:
    tar.extractall(work)

state = work / "state.sqlite"
manifest_path = work / "manifest.json"
conn = sqlite3.connect(state)
try:
    conn.execute("DELETE FROM torque_stack_run_artifacts WHERE artifact_name = 'host-command-verify.json'")
    leaked = '{"status":"succeeded","stdout":"password=clear-text-audit-leak\\n"}'
    conn.execute(
        "UPDATE torque_stack_run_artifacts SET body_text = ?, sha256 = '', size_bytes = ? WHERE artifact_name = 'host-command-execute.json'",
        (leaked, len(leaked.encode("utf-8"))),
    )
    conn.commit()
finally:
    conn.close()

manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
manifest["stateSha256"] = hashlib.sha256(state.read_bytes()).hexdigest()
manifest_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
out.parent.mkdir(parents=True, exist_ok=True)
with tarfile.open(out, "w:gz") as tar:
    tar.add(state, arcname="state.sqlite")
    tar.add(manifest_path, arcname="manifest.json")
PY

ops_log "audit tampered ops run bundle and expect verification failure"
set +e
(
  cd "${repo_root}"
  ./bin/torque stack audit --from-bundle "${tampered_export}" --output json --include-plan --include-events --include-artifacts
) >"${OPS_RUN_DIR}/audit/tampered-from-bundle.json" 2>"${OPS_RUN_DIR}/audit/tampered-from-bundle.stderr"
tampered_code=$?
set -e
printf '%s\n' "${tampered_code}" >"${OPS_RUN_DIR}/audit/tampered-from-bundle.exit"
if [[ "${tampered_code}" -eq 0 ]]; then
  ops_fail "tampered bundle audit unexpectedly succeeded"
fi
tampered_audit_status="failed"

ops_log "verify ops audit JSON"
python3 - \
  "${OPS_RUN_DIR}/audit/safe-from-bundle.json" \
  "${OPS_RUN_DIR}/audit/tampered-from-bundle.json" \
  "${OPS_RUN_DIR}/verification/ops-cli-005-proof.json" <<'PY'
import json
import sys
from pathlib import Path

safe_path, tampered_path, out_path = map(Path, sys.argv[1:4])
errors = []

def load(path):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        errors.append(f"{path.name} is not valid JSON: {exc}")
        return {}

def finding_codes(doc):
    return {item.get("code") for item in doc.get("ops", {}).get("findings", [])}

safe = load(safe_path)
tampered = load(tampered_path)
safe_ops = safe.get("ops") or {}
tampered_ops = tampered.get("ops") or {}
safe_commands = safe_ops.get("hostCommands") or []
if safe.get("status") != "succeeded":
    errors.append(f"safe audit run status is {safe.get('status')}")
if safe_ops.get("verification", {}).get("status") != "passed":
    errors.append("safe ops audit did not pass")
if safe_ops.get("replay", {}).get("status") != "eligible":
    errors.append("safe replay gate was not eligible")
if safe_ops.get("preflight", {}).get("status") != "eligible":
    errors.append("safe preflight gate was not eligible")
if not safe_commands:
    errors.append("safe audit has no host command receipts")
for item in safe_commands:
    if not all(item.get(k) for k in ("observePresent", "planPresent", "executePresent", "verifyPresent")):
        errors.append(f"safe host command receipts incomplete for {item.get('nodeId')}")
    if item.get("guardMode") != "ops":
        errors.append(f"safe host command guard mode is {item.get('guardMode')}")
    if item.get("redactionVerified") is not True:
        errors.append(f"safe host command redaction is not verified for {item.get('nodeId')}")

codes = finding_codes(tampered)
if tampered_ops.get("verification", {}).get("status") != "failed":
    errors.append("tampered ops audit did not fail")
for code in ("ops.host_command.verify_missing", "ops.host_command.redaction_leak"):
    if code not in codes:
        errors.append(f"tampered audit missing finding {code}")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsCLI005Proof",
    "status": "succeeded" if not errors else "failed",
    "errors": errors,
    "safeOpsStatus": safe_ops.get("verification", {}).get("status"),
    "tamperedOpsStatus": tampered_ops.get("verification", {}).get("status"),
    "tamperedFindingCodes": sorted(c for c in codes if c),
}
out_path.parent.mkdir(parents=True, exist_ok=True)
out_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
