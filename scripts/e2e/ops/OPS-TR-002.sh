#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-TR-002.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

OPS-TR-002 proves the local transport primitive. It uses the same shared
operation receipt contract as the SSH transport, runs localhost commands,
uploads and downloads a temp file through local copy primitives, records a
bounded timeout, redacts command/output evidence, verifies cleanup, and exports
a standard ops evidence bundle.
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

ops_require_cmd go

ops_init_run "OPS-TR-002"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-tr-002.XXXXXX")"
started_at="$(ops_utc_now)"
local_transport_root="${scratch_root}/local-transport"

cleanup_lab_resources() {
  local status="succeeded"
  local local_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      status="failed"
      local_status="failed"
    else
      local_status="deleted"
    fi
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    labProfiles="lab.local" \
    scratchRoot="${scratch_root}" \
    local="${local_status}" \
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

mkdir -p "${OPS_RUN_DIR}/go" "${OPS_RUN_DIR}/transport" "${OPS_RUN_DIR}/verification"
proof_json="${OPS_RUN_DIR}/transport/local-transport-proof.json"
go_test_json="${OPS_RUN_DIR}/go/localtransport-test.jsonl"

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
  printf 'secret://ops/tr-002#token\n'
} | ops_redact_stdin "${OPS_RUN_DIR}/transport/redaction-probe.txt"

ops_log "run local transport package tests"
if go test -json ./internal/ops/transport/local >"${go_test_json}"; then
  package_test_status="succeeded"
else
  package_test_status="failed"
fi

ops_log "run lab.local transport proof"
if TORQUE_OPS_TR_LOCAL_E2E_OUTPUT="${proof_json}" \
  TORQUE_OPS_TR_LOCAL_E2E_ROOT="${local_transport_root}" \
  TORQUE_OPS_TR_LOCAL_E2E_CANARY="${OPS_SECRET_CANARY}" \
  go test ./internal/ops/transport/local -run TestE2EEnvLocalTransport -count=1 >"${OPS_RUN_DIR}/go/e2e-local-transport.out" 2>&1; then
  transport_status="succeeded"
else
  transport_status="failed"
fi

ops_log "verify local transport proof redaction and shape"
python3 - "${proof_json}" "${OPS_RUN_DIR}/transport/redaction-check.json" "${OPS_SECRET_CANARY}" <<'PY'
import json
import sys
from pathlib import Path

proof_path = Path(sys.argv[1])
output = Path(sys.argv[2])
canary = sys.argv[3]
errors = []
raw = ""
proof = {}
if proof_path.is_file():
    raw = proof_path.read_text(encoding="utf-8")
    proof = json.loads(raw)
else:
    errors.append("missing local transport proof")

operations = proof.get("operations", {})
for name in ["connect", "prepare", "upload", "copy", "download"]:
    if operations.get(name, {}).get("status") != "succeeded":
        errors.append(f"{name} operation did not succeed")
timeout = operations.get("timeout", {})
if timeout.get("status") != "timeout" or timeout.get("timedOut") is not True:
    errors.append("timeout operation did not record bounded timeout")
if proof.get("downloadContentMatch") is not True:
    errors.append("downloaded file did not match uploaded temp file")
if proof.get("evidenceShapeMatchesSSH") is not True:
    errors.append("local evidence shape does not match SSH")
if proof.get("status") != "succeeded":
    errors.append("proof status was not succeeded")
if canary in raw:
    errors.append("secret canary leaked into proof")
if "secret://" in raw:
    errors.append("secret reference leaked into proof")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLocalTransportRedactionCheck",
    "status": "succeeded" if not errors else "failed",
    "secretCanaryLeak": canary in raw,
    "secretReferenceLeak": "secret://" in raw,
    "downloadContentMatch": proof.get("downloadContentMatch", False),
    "timeoutRecorded": timeout.get("timedOut") is True,
    "evidenceShapeMatchesSSH": proof.get("evidenceShapeMatchesSSH", False),
    "errors": errors,
}
output.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${proof_json}" \
  "${OPS_RUN_DIR}/transport/redaction-check.json" \
  "${package_test_status}" \
  "${transport_status}" <<'PY'
import json
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
proof_path = Path(sys.argv[5])
redaction_check_path = Path(sys.argv[6])
package_test_status = sys.argv[7]
transport_status = sys.argv[8]


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


proof = json.load(proof_path.open(encoding="utf-8")) if proof_path.is_file() else {}
redaction_check = json.load(redaction_check_path.open(encoding="utf-8")) if redaction_check_path.is_file() else {}
operations = proof.get("operations", {})
errors = []
if package_test_status != "succeeded":
    errors.append("local transport package tests failed")
if transport_status != "succeeded":
    errors.append("local transport E2E failed")
for name in ["connect", "prepare", "upload", "copy", "download"]:
    if operations.get(name, {}).get("status") != "succeeded":
        errors.append(f"{name} operation failed")
if operations.get("timeout", {}).get("status") != "timeout" or operations.get("timeout", {}).get("timedOut") is not True:
    errors.append("timeout proof missing")
if proof.get("downloadContentMatch") is not True:
    errors.append("upload/download content proof failed")
if proof.get("evidenceShapeMatchesSSH") is not True:
    errors.append("local/ssh evidence shape proof failed")
if redaction_check.get("status") != "succeeded":
    errors.append("redaction check failed")
if redaction_check.get("secretCanaryLeak") or redaction_check.get("secretReferenceLeak"):
    errors.append("redaction leak found")

status = "succeeded" if not errors else "failed"
lab_profiles = ["lab.local"]
target_digest = proof.get("targetDigest", "")
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(lab_profiles),
        "transport": "local",
    },
)
write_json(
    run_dir / "target-snapshot.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsLabTargetSnapshot",
        "taskId": task_id,
        "runId": run_id,
        "profiles": lab_profiles,
        "targets": [
            {
                "targetId": "local/controller",
                "type": "local",
                "profile": "lab.local",
                "transport": "local",
                "targetDigest": target_digest,
            }
        ],
        "targetCount": 1,
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded" if not errors else "blocked",
        "decision": "allow" if not errors else "block",
        "taskId": task_id,
        "runId": run_id,
        "reason": "local-transport-contract-proof",
        "labProfiles": lab_profiles,
        "packageTests": package_test_status,
        "transport": transport_status,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": status,
        "taskId": task_id,
        "runId": run_id,
        "labProfiles": lab_profiles,
        "targetDigest": target_digest,
        "connectStatus": operations.get("connect", {}).get("status", ""),
        "runStatus": operations.get("copy", {}).get("status", ""),
        "uploadStatus": operations.get("upload", {}).get("status", ""),
        "downloadStatus": operations.get("download", {}).get("status", ""),
        "timeoutStatus": operations.get("timeout", {}).get("status", ""),
        "timeoutRecorded": operations.get("timeout", {}).get("timedOut") is True,
        "downloadContentMatch": proof.get("downloadContentMatch", False),
        "evidenceShapeMatchesSSH": proof.get("evidenceShapeMatchesSSH", False),
        "secretCanaryLeak": redaction_check.get("secretCanaryLeak"),
        "secretReferenceLeak": redaction_check.get("secretReferenceLeak"),
        "errors": errors,
        "verifiedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    },
)
write_json(
    run_dir / "result.json",
    {
        "status": status,
        "taskId": task_id,
        "runId": run_id,
        "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "labProfiles": lab_profiles,
        "transport": "local",
        "connectStatus": operations.get("connect", {}).get("status", ""),
        "runStatus": operations.get("copy", {}).get("status", ""),
        "uploadStatus": operations.get("upload", {}).get("status", ""),
        "downloadStatus": operations.get("download", {}).get("status", ""),
        "timeoutRecorded": operations.get("timeout", {}).get("timedOut") is True,
        "downloadContentMatch": proof.get("downloadContentMatch", False),
        "evidenceShapeMatchesSSH": proof.get("evidenceShapeMatchesSSH", False),
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
