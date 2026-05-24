#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-LAB-002.sh [options]

Options:
  --evidence-root DIR     Evidence root. Defaults to a temp directory.
  --cleanup               Clean temporary negative fixtures. Default.
  --no-cleanup            Leave temporary negative fixtures for debugging.
  -h, --help              Show this help.

OPS-LAB-002 proves the reusable ops E2E evidence contract. It creates one valid
local evidence bundle, validates it, then creates intentionally broken fixtures
and proves the validator rejects missing target snapshot, decision,
verification, cleanup, and export artifacts.
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

ops_init_run "OPS-LAB-002"
negative_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-lab-002-negative.XXXXXX")"

cleanup_lab_resources() {
  local status="succeeded"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${negative_root}"
    if [[ -e "${negative_root}" ]]; then
      status="failed"
    fi
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    negativeFixtures="${negative_root}" \
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

write_valid_fixture() {
  local root="$1"
  local bundle="$2"
  local task_id="$3"
  local run_id="$4"

  mkdir -p "${root}/verification" "${root}/cleanup" "${root}/contract"
  ops_write_json_object "${root}/metadata.json" \
    taskId="${task_id}" \
    runId="${run_id}" \
    startedAt="$(ops_utc_now)" \
    profiles=lab.local
  python3 - "${root}/target-snapshot.json" "${task_id}" "${run_id}" <<'PY'
import json
import sys

path, task_id, run_id = sys.argv[1:4]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "profiles": ["lab.local"],
    "targets": [
        {
            "configured": True,
            "profile": "lab.local",
            "transport": "local",
            "type": "local",
        }
    ],
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
  ops_write_json_object "${root}/decision.json" \
    status=succeeded \
    decision=allow \
    taskId="${task_id}" \
    runId="${run_id}" \
    reason=contract-fixture \
    decidedAt="$(ops_utc_now)"
  ops_write_json_object "${root}/verification/receipt.json" \
    status=succeeded \
    taskId="${task_id}" \
    runId="${run_id}" \
    verifiedAt="$(ops_utc_now)"
  ops_write_json_object "${root}/cleanup/receipt.json" \
    status=succeeded \
    taskId="${task_id}" \
    runId="${run_id}" \
    finishedAt="$(ops_utc_now)"
  ops_write_json_object "${root}/result.json" \
    status=succeeded \
    taskId="${task_id}" \
    runId="${run_id}" \
    finishedAt="$(ops_utc_now)"
  printf 'password=%s\n' "${OPS_SECRET_CANARY}" | ops_redact_stdin "${root}/contract/redacted-output.txt"
  ops_scan_for_secret_material "${root}" "${root}/redaction-report.json"
  local old_task="${OPS_TASK_ID}"
  local old_run="${OPS_RUN_ID}"
  OPS_TASK_ID="${task_id}"
  OPS_RUN_ID="${run_id}"
  ops_write_manifest "${root}" "${root}/manifest.json"
  ops_export_bundle "${root}" "${bundle}"
  OPS_TASK_ID="${old_task}"
  OPS_RUN_ID="${old_run}"
}

expect_validation_failure() {
  local name="$1"
  local root="$2"
  local bundle="$3"
  local expected="$4"
  local report="${OPS_RUN_DIR}/contract/${name}.json"

  if ops_validate_evidence_contract "${root}" "${bundle}" >"${report}" 2>/dev/null; then
    ops_fail "expected validation failure for ${name}"
  fi
  if ! grep -q "${expected}" "${report}"; then
    echo "validator report for ${name} did not contain expected text: ${expected}" >&2
    sed -n '1,160p' "${report}" >&2
    exit 1
  fi
}

ops_log "create valid contract fixture"
mkdir -p "${OPS_RUN_DIR}/contract"
valid_fixture="${negative_root}/valid"
valid_bundle="${negative_root}/valid.tgz"
write_valid_fixture "${valid_fixture}" "${valid_bundle}" "OPS-LAB-002-FIXTURE" "valid-${OPS_RUN_ID}"
ops_validate_evidence_contract "${valid_fixture}" "${valid_bundle}" >"${OPS_RUN_DIR}/contract/valid-fixture.json"

ops_log "prove required artifact failures"
root_run_id="${OPS_RUN_ID}"
declare -a cases=(
  "missing-target-snapshot:target-snapshot.json:missing required artifact: target-snapshot.json"
  "missing-decision:decision.json:missing required artifact: decision.json"
  "missing-verification:verification/receipt.json:missing required artifact: verification/receipt.json"
  "missing-cleanup:cleanup/receipt.json:missing required artifact: cleanup/receipt.json"
)

for item in "${cases[@]}"; do
  IFS=: read -r name missing expected <<<"${item}"
  fixture="${negative_root}/${name}"
  bundle="${negative_root}/${name}.tgz"
  cp -R "${valid_fixture}" "${fixture}"
  rm -f "${fixture}/${missing}"
  OPS_TASK_ID="OPS-LAB-002-FIXTURE"
  OPS_RUN_ID="valid-${root_run_id}"
  ops_write_manifest "${fixture}" "${fixture}/manifest.json"
  ops_export_bundle "${fixture}" "${bundle}"
  OPS_TASK_ID="OPS-LAB-002"
  OPS_RUN_ID="${root_run_id}"
  expect_validation_failure "${name}" "${fixture}" "${bundle}" "${expected}"
done

ops_log "prove missing export failure"
missing_export_fixture="${negative_root}/missing-export"
cp -R "${valid_fixture}" "${missing_export_fixture}"
expect_validation_failure \
  "missing-export" \
  "${missing_export_fixture}" \
  "${negative_root}/missing-export-does-not-exist.tgz" \
  "missing evidence bundle"

ops_write_json_object "${OPS_RUN_DIR}/metadata.json" \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  startedAt="$(ops_utc_now)" \
  profiles=lab.local
python3 - "${OPS_RUN_DIR}/target-snapshot.json" "${OPS_TASK_ID}" "${OPS_RUN_ID}" <<'PY'
import json
import sys

path, task_id, run_id = sys.argv[1:4]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "profiles": ["lab.local"],
    "targets": [{"profile": "lab.local", "type": "local", "transport": "local", "configured": True}],
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
ops_write_json_object "${OPS_RUN_DIR}/decision.json" \
  status=succeeded \
  decision=allow \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  reason=evidence-contract-validated \
  decidedAt="$(ops_utc_now)"
mkdir -p "${OPS_RUN_DIR}/verification"
ops_write_json_object "${OPS_RUN_DIR}/verification/receipt.json" \
  status=succeeded \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  validFixture=contract/valid-fixture.json \
  missingTargetSnapshot=contract/missing-target-snapshot.json \
  missingDecision=contract/missing-decision.json \
  missingVerification=contract/missing-verification.json \
  missingCleanup=contract/missing-cleanup.json \
  missingExport=contract/missing-export.json \
  verifiedAt="$(ops_utc_now)"
ops_write_json_object "${OPS_RUN_DIR}/result.json" \
  status=succeeded \
  taskId="${OPS_TASK_ID}" \
  runId="${OPS_RUN_ID}" \
  finishedAt="$(ops_utc_now)" \
  negativeCases=5
