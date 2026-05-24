#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-FACT-003.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --ttl DURATION       Fact freshness TTL. Defaults to 2s.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

OPS-FACT-003 proves host fact cache and staleness handling on lab.ssh-linux.
It refreshes a missing cache, proves a fresh cache hit avoids recollection,
intentionally expires the cached snapshot to block a plan, then refreshes the
stale cache and exports the decision evidence.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@lab-host
  TORQUE_LAB_SSH_IDENTITY=/path/to/key       optional
  TORQUE_LAB_SSH_OPTS="..."                 optional
EOF
}

cleanup_enabled=1
fact_ttl="2s"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --ttl)
      [[ $# -ge 2 ]] || ops_fail "--ttl requires a value"
      fact_ttl="$2"
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux fact cache E2E without TORQUE_OPS_E2E_CONFIRM=1"
ops_require_env TORQUE_LAB_SSH
ops_require_cmd go
ops_require_cmd ssh

ops_init_run "OPS-FACT-003"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-fact-003.XXXXXX")"
started_at="$(ops_utc_now)"

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
    labProfiles="lab.ssh-linux" \
    scratchRoot="${scratch_root}" \
    local="${local_status}" \
    remote="not-required" \
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

mkdir -p "${OPS_RUN_DIR}/facts" "${OPS_RUN_DIR}/go" "${OPS_RUN_DIR}/verification" "${scratch_root}/cache"
proof_json="${OPS_RUN_DIR}/facts/host-fact-cache-proof.json"
check_json="${OPS_RUN_DIR}/facts/cache-staleness-check.json"
go_test_json="${OPS_RUN_DIR}/go/hostfacts-test.jsonl"

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
  printf 'secret://ops/fact-003#token\n'
} | ops_redact_stdin "${OPS_RUN_DIR}/facts/redaction-probe.txt"

ops_log "run host fact cache package tests"
if go test -json ./internal/ops/facts/host >"${go_test_json}"; then
  package_test_status="succeeded"
else
  package_test_status="failed"
fi

ops_log "prove fact cache freshness, stale block, and refresh on lab.ssh-linux"
if TORQUE_OPS_FACT_CACHE_E2E_TARGET="${TORQUE_LAB_SSH}" \
  TORQUE_OPS_FACT_CACHE_E2E_OUTPUT="${proof_json}" \
  TORQUE_OPS_FACT_CACHE_E2E_DIR="${scratch_root}/cache" \
  TORQUE_OPS_FACT_CACHE_E2E_CANARY="${OPS_SECRET_CANARY}" \
  TORQUE_OPS_FACT_CACHE_E2E_TTL="${fact_ttl}" \
  go test ./internal/ops/facts/host -run TestE2EEnvHostFactCacheStaleness -count=1 >"${OPS_RUN_DIR}/go/e2e-host-fact-cache.out" 2>&1; then
  cache_e2e_status="succeeded"
else
  cache_e2e_status="failed"
fi

ops_log "verify cache decision evidence"
python3 - "${proof_json}" "${check_json}" "${OPS_SECRET_CANARY}" <<'PY'
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
    errors.append("missing host fact cache proof")

def phase(name):
    value = proof.get(name, {})
    return value if isinstance(value, dict) else {}

def decision(value):
    item = value.get("decision", {})
    return item if isinstance(item, dict) else {}

def previous(value):
    item = value.get("previousDecision", {})
    return item if isinstance(item, dict) else {}

initial = phase("initial")
fresh = phase("fresh")
stale = phase("stale")
refresh = phase("refresh")
initial_decision = decision(initial)
fresh_decision = decision(fresh)
stale_decision = decision(stale)
refresh_decision = decision(refresh)

if proof.get("status") != "succeeded":
    errors.append("proof status was not succeeded")
if initial.get("source") != "refresh" or not initial.get("refreshed"):
    errors.append("initial missing-cache phase did not refresh")
if previous(initial).get("status") != "missing":
    errors.append("initial phase did not record missing-cache decision")
if initial_decision.get("status") != "fresh" or initial_decision.get("blocked"):
    errors.append("initial refresh did not produce fresh facts")
if fresh.get("source") != "cache" or fresh.get("refreshed"):
    errors.append("fresh phase did not use cache without refresh")
if fresh_decision.get("status") != "fresh" or fresh_decision.get("decision") != "allow":
    errors.append("fresh cache phase did not allow")
if stale.get("source") != "cache":
    errors.append("stale phase did not inspect cache")
if stale_decision.get("status") != "stale" or stale_decision.get("decision") != "block" or not stale_decision.get("blocked"):
    errors.append("stale phase did not block")
if refresh.get("source") != "refresh" or not refresh.get("refreshed"):
    errors.append("refresh phase did not refresh stale facts")
if previous(refresh).get("status") != "stale":
    errors.append("refresh phase did not record previous stale decision")
if refresh_decision.get("status") != "fresh" or refresh_decision.get("blocked"):
    errors.append("refresh phase did not produce fresh facts")
for name, value in [("initial", initial), ("fresh", fresh), ("stale", stale), ("refresh", refresh)]:
    if not str(value.get("snapshotDigest", "")).startswith("sha256:"):
        errors.append(f"{name} snapshot digest missing")
    if not str(value.get("targetDigest", "")).startswith("sha256:"):
        errors.append(f"{name} target digest missing")
if canary in raw:
    errors.append("secret canary leaked into proof")
if "secret://" in raw:
    errors.append("secret reference leaked into proof")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsHostFactCacheStalenessCheck",
    "status": "succeeded" if not errors else "failed",
    "ttl": proof.get("ttl", ""),
    "initialStatus": initial_decision.get("status", ""),
    "freshStatus": fresh_decision.get("status", ""),
    "staleStatus": stale_decision.get("status", ""),
    "refreshStatus": refresh_decision.get("status", ""),
    "staleDecision": stale_decision.get("decision", ""),
    "secretCanaryLeak": canary in raw,
    "secretReferenceLeak": "secret://" in raw,
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
  "${check_json}" \
  "${package_test_status}" \
  "${cache_e2e_status}" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
proof_path = Path(sys.argv[5])
check_path = Path(sys.argv[6])
package_test_status = sys.argv[7]
cache_e2e_status = sys.argv[8]

def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")

proof = json.load(proof_path.open(encoding="utf-8")) if proof_path.is_file() else {}
check = json.load(check_path.open(encoding="utf-8")) if check_path.is_file() else {}
refresh = proof.get("refresh", {}) if isinstance(proof.get("refresh"), dict) else {}
stale = proof.get("stale", {}) if isinstance(proof.get("stale"), dict) else {}
errors = []
if package_test_status != "succeeded":
    errors.append("host fact package tests failed")
if cache_e2e_status != "succeeded":
    errors.append("host fact cache E2E failed")
if check.get("status") != "succeeded":
    errors.append("cache staleness check failed")
if check.get("secretCanaryLeak") or check.get("secretReferenceLeak"):
    errors.append("redaction leak found")
if stale.get("decision", {}).get("status") != "stale":
    errors.append("stale decision evidence missing")
if refresh.get("decision", {}).get("status") != "fresh":
    errors.append("refresh decision evidence missing")

status = "succeeded" if not errors else "failed"
lab_profiles = ["lab.ssh-linux"]
target_digest = refresh.get("targetDigest", "")
snapshot_digest = refresh.get("snapshotDigest", "")
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(lab_profiles),
        "factKind": "host.fact.cache",
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
                "targetId": "host/lab-ssh",
                "type": "host",
                "profile": "lab.ssh-linux",
                "transport": "ssh",
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
        "reason": "host-fact-cache-staleness-proof",
        "labProfiles": lab_profiles,
        "packageTests": package_test_status,
        "cacheE2E": cache_e2e_status,
        "staleDecision": stale.get("decision", {}),
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
        "snapshotDigest": snapshot_digest,
        "ttl": proof.get("ttl", ""),
        "initialStatus": check.get("initialStatus", ""),
        "freshStatus": check.get("freshStatus", ""),
        "staleStatus": check.get("staleStatus", ""),
        "staleDecision": check.get("staleDecision", ""),
        "refreshStatus": check.get("refreshStatus", ""),
        "secretCanaryLeak": check.get("secretCanaryLeak"),
        "secretReferenceLeak": check.get("secretReferenceLeak"),
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
        "factKind": "host.fact.cache",
        "snapshotDigest": snapshot_digest,
        "staleStatus": check.get("staleStatus", ""),
        "refreshStatus": check.get("refreshStatus", ""),
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
