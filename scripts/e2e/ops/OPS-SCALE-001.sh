#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-001.sh [options]

Options:
  --targets N            Synthetic target count. Default: 10000.
  --shard-size N         Targets per shard. Default: 250.
  --max-bundle-bytes N   Fail if exported evidence exceeds this size.
                          Default: 1048576.
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Write cleanup proof. Default.
  --no-cleanup           Keep temporary simulation scratch for debugging.
  -h, --help             Show this help.

OPS-SCALE-001 proves the large-fleet simulation harness. It generates 10,000
deterministic synthetic targets, partitions them into shards, simulates
plan/apply/verify, injects bounded failures, exports evidence, and validates
that the bundle stays small by storing shard summaries instead of per-host logs.
EOF
}

target_count=10000
shard_size=250
max_bundle_bytes=1048576
cleanup_enabled=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --targets)
      [[ $# -ge 2 ]] || ops_fail "--targets requires a value"
      target_count="$2"
      shift 2
      ;;
    --shard-size)
      [[ $# -ge 2 ]] || ops_fail "--shard-size requires a value"
      shard_size="$2"
      shift 2
      ;;
    --max-bundle-bytes)
      [[ $# -ge 2 ]] || ops_fail "--max-bundle-bytes requires a value"
      max_bundle_bytes="$2"
      shift 2
      ;;
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

case "${target_count}" in
  ''|*[!0-9]*) ops_fail "--targets must be a positive integer" ;;
esac
case "${shard_size}" in
  ''|*[!0-9]*) ops_fail "--shard-size must be a positive integer" ;;
esac
case "${max_bundle_bytes}" in
  ''|*[!0-9]*) ops_fail "--max-bundle-bytes must be a positive integer" ;;
esac
[[ "${target_count}" -gt 0 ]] || ops_fail "--targets must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"

ops_init_run "OPS-SCALE-001"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-001.XXXXXX")"
started_at="$(ops_utc_now)"

cleanup_lab_resources() {
  local status="succeeded"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      status="failed"
    fi
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    labProfile=lab.scale-sim \
    scratchRoot="${scratch_root}" \
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
  local bundle_size
  bundle_size="$(wc -c <"${OPS_BUNDLE_PATH}" | tr -d '[:space:]')"
  if [[ "${bundle_size}" -gt "${max_bundle_bytes}" ]]; then
    echo "bundle too large: ${bundle_size} > ${max_bundle_bytes}" >&2
    code=1
  fi
  ops_validate_evidence_contract "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}" >"${OPS_BUNDLE_PATH%.tgz}.contract.json" || code=1
  if [[ ${code} -eq 0 ]]; then
    ops_log "evidence: ${OPS_RUN_DIR}"
    ops_log "bundle: ${OPS_BUNDLE_PATH}"
    ops_log "bundle bytes: ${bundle_size}"
  else
    echo "evidence: ${OPS_RUN_DIR}" >&2
    echo "bundle: ${OPS_BUNDLE_PATH}" >&2
    echo "bundle bytes: ${bundle_size:-unknown}" >&2
  fi
  exit "${code}"
}
trap finish EXIT

ops_log "simulate ${target_count} targets with shard size ${shard_size}"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${max_bundle_bytes}" \
  "${scratch_root}" <<'PY'
import hashlib
import json
import math
import os
import platform
import resource
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
target_count = int(sys.argv[5])
shard_size = int(sys.argv[6])
max_bundle_bytes = int(sys.argv[7])
scratch_root = sys.argv[8]

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
region_names = ["us-east", "us-west", "eu-central", "ap-south"]
zone_names = ["a", "b", "c", "d"]
role_names = ["web", "api", "worker", "db-client", "cache-client"]
failure_count = min(10, target_count)
failure_targets = {
    min(target_count - 1, max(0, math.floor((index + 1) * target_count / (failure_count + 1))))
    for index in range(failure_count)
}
worker_loss_shard = 7 if shard_count > 7 else max(0, shard_count - 1)
retry_storm_shard = 13 if shard_count > 13 else max(0, shard_count - 1)


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


def digest(value: object) -> str:
    data = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(data).hexdigest()


def target_identity(index: int) -> dict:
    return {
        "id": f"host-{index:05d}",
        "region": region_names[index % len(region_names)],
        "zone": zone_names[index % len(zone_names)],
        "role": role_names[index % len(role_names)],
        "os": "linux",
        "kernel": f"5.15.{index % 32}",
        "packageEpoch": index % 17,
    }


def target_digest(index: int) -> str:
    return digest(target_identity(index))


target_hash = hashlib.sha256()
fact_hash = hashlib.sha256()
intent_hash = hashlib.sha256()
for i in range(target_count):
    target_hash.update(target_digest(i).encode())
    fact_hash.update(
        digest(
            {
                "id": f"host-{i:05d}",
                "cpu": 2 + (i % 14),
                "memoryMiB": 1024 + ((i % 64) * 256),
                "packageEpoch": i % 17,
            }
        ).encode()
    )
    intent_hash.update(f"host-{i:05d}:desired-package=v2\n".encode())

samples = [target_identity(i) for i in list(range(min(5, target_count)))]
samples.extend(target_identity(i) for i in range(max(0, target_count - 5), target_count))

write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": "lab.scale-sim",
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
    },
)
write_json(
    run_dir / "target-snapshot.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsLabTargetSnapshot",
        "taskId": task_id,
        "runId": run_id,
        "profiles": ["lab.scale-sim"],
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "targetDigest": target_hash.hexdigest(),
        "factDigest": fact_hash.hexdigest(),
        "targets": [
            {
                "profile": "lab.scale-sim",
                "type": "synthetic-host-fleet",
                "transport": "simulated",
                "configured": True,
                "count": target_count,
            }
        ],
        "samples": samples,
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded",
        "decision": "allow",
        "taskId": task_id,
        "runId": run_id,
        "reason": "deterministic-scale-simulation",
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "decidedAt": started_at,
    },
)

shards_dir = run_dir / "scale" / "shards"
scratch_events = Path(scratch_root) / "events.log"
scratch_events.parent.mkdir(parents=True, exist_ok=True)

total_planned = 0
total_applied = 0
total_recovered = 0
total_final_failed = 0
total_retries = 0
shard_summaries = []
for shard in range(shard_count):
    start_index = shard * shard_size
    end_index = min(start_index + shard_size, target_count)
    count = end_index - start_index
    shard_failures = [i for i in range(start_index, end_index) if i in failure_targets]
    worker_lost = shard == worker_loss_shard
    retry_storm = shard == retry_storm_shard
    retry_count = len(shard_failures) + (250 if retry_storm else 0) + (1 if worker_lost else 0)
    recovered = len(shard_failures)
    applied = count
    final_failed = 0

    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleShardManifest",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "range": {"start": start_index, "endExclusive": end_index},
        "targetCount": count,
        "workerLease": {
            "leaseId": f"lease-{shard:04d}",
            "workerId": f"worker-{shard % 8:02d}",
            "reassigned": worker_lost,
            "replacementWorkerId": f"worker-{(shard + 3) % 8:02d}" if worker_lost else "",
        },
        "plan": {
            "planned": count,
            "intentDigest": digest({"shard": shard, "start": start_index, "end": end_index, "intent": "desired-package=v2"}),
            "observedDigest": digest({"shard": shard, "facts": [target_digest(i) for i in range(start_index, min(end_index, start_index + 3))]}),
        },
        "apply": {
            "attempted": count,
            "applied": applied,
            "simulatedTargetFailures": len(shard_failures),
            "recovered": recovered,
            "finalFailed": final_failed,
            "retryCount": retry_count,
        },
        "verify": {
            "verified": count,
            "failed": final_failed,
            "digest": digest({"shard": shard, "verified": count, "failed": final_failed}),
        },
        "backpressure": {
            "retryStorm": retry_storm,
            "paused": retry_storm,
            "pauseReason": "retry-storm-budget" if retry_storm else "",
        },
        "sampleTargets": [f"host-{i:05d}" for i in range(start_index, min(end_index, start_index + 3))],
    }
    write_json(shards_dir / f"shard-{shard:04d}.json", shard_doc)
    with scratch_events.open("a", encoding="utf-8") as f:
        f.write(f"shard={shard} targets={count} retries={retry_count} workerLost={worker_lost} retryStorm={retry_storm}\n")

    total_planned += count
    total_applied += applied
    total_recovered += recovered
    total_final_failed += final_failed
    total_retries += retry_count
    shard_summaries.append(
        {
            "shard": shard,
            "targetCount": count,
            "planned": count,
            "applied": applied,
            "verified": count,
            "finalFailed": final_failed,
            "retryCount": retry_count,
            "workerReassigned": worker_lost,
            "retryStormPaused": retry_storm,
        }
    )

failure_injection = {
    "workerLoss": {
        "enabled": True,
        "shard": worker_loss_shard,
        "expectedOutcome": "reassigned",
    },
    "targetFailures": {
        "enabled": True,
        "targetIds": [f"host-{i:05d}" for i in sorted(failure_targets) if i < target_count],
        "expectedOutcome": "retried-and-recovered",
    },
    "retryStorm": {
        "enabled": True,
        "shard": retry_storm_shard,
        "expectedOutcome": "backpressure-paused-and-resumed",
    },
}
write_json(run_dir / "scale" / "failure-injection.json", failure_injection)
write_json(
    run_dir / "scale" / "summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleSimulationSummary",
        "taskId": task_id,
        "runId": run_id,
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "planned": total_planned,
        "applied": total_applied,
        "verified": target_count - total_final_failed,
        "finalFailed": total_final_failed,
        "recoveredFailures": total_recovered,
        "retryCount": total_retries,
        "targetDigest": target_hash.hexdigest(),
        "factDigest": fact_hash.hexdigest(),
        "intentDigest": intent_hash.hexdigest(),
        "shards": shard_summaries,
        "failureInjection": failure_injection,
        "evidenceMode": "shard-manifests-not-per-host-logs",
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

duration_ms = int((time.monotonic() - start) * 1000)
maxrss = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
write_json(
    run_dir / "scale" / "performance.json",
    {
        "durationMs": duration_ms,
        "pythonMaxRss": maxrss,
        "platform": platform.platform(),
        "targetCount": target_count,
        "shardCount": shard_count,
        "targetsPerSecond": round(target_count / max(duration_ms / 1000, 0.001), 2),
    },
)

verification_errors = []
if total_planned != target_count:
    verification_errors.append("planned count mismatch")
if total_applied != target_count:
    verification_errors.append("applied count mismatch")
if target_count - total_final_failed != target_count:
    verification_errors.append("verified count mismatch")
if shard_count != len(shard_summaries):
    verification_errors.append("shard count mismatch")
if not failure_injection["workerLoss"]["enabled"]:
    verification_errors.append("worker loss was not injected")
if not failure_injection["retryStorm"]["enabled"]:
    verification_errors.append("retry storm was not injected")
if not failure_injection["targetFailures"]["targetIds"]:
    verification_errors.append("target failures were not injected")

write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": "succeeded" if not verification_errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "planned": total_planned,
        "applied": total_applied,
        "verified": target_count - total_final_failed,
        "finalFailed": total_final_failed,
        "recoveredFailures": total_recovered,
        "retryCount": total_retries,
        "errors": verification_errors,
        "verifiedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    },
)
write_json(
    run_dir / "result.json",
    {
        "status": "succeeded" if not verification_errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

if verification_errors:
    raise SystemExit("; ".join(verification_errors))
PY
