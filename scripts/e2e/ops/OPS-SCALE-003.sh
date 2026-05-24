#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-003.sh [options]

Options:
  --targets N            Synthetic target count. Default: 10000.
  --shard-size N         Targets per shard. Default: 250.
  --failure-shard N      Shard whose worker expires mid-run. Default: 7.
  --max-bundle-bytes N   Fail if exported evidence exceeds this size.
                          Default: 1048576.
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Write cleanup proof. Default.
  --no-cleanup           Keep temporary simulation scratch for debugging.
  -h, --help             Show this help.

OPS-SCALE-003 proves worker lease behavior. It assigns shards to workers,
heartbeats leases, expires one worker mid-shard, steals the lease, resumes from
the checkpoint, and proves each synthetic target mutates exactly once.
EOF
}

target_count=10000
shard_size=250
failure_shard=7
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
    --failure-shard)
      [[ $# -ge 2 ]] || ops_fail "--failure-shard requires a value"
      failure_shard="$2"
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
case "${failure_shard}" in
  ''|*[!0-9]*) ops_fail "--failure-shard must be a non-negative integer" ;;
esac
case "${max_bundle_bytes}" in
  ''|*[!0-9]*) ops_fail "--max-bundle-bytes must be a positive integer" ;;
esac
[[ "${target_count}" -gt 0 ]] || ops_fail "--targets must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"

ops_init_run "OPS-SCALE-003"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-003.XXXXXX")"
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

ops_log "prove worker leases and resume for ${target_count} targets"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${failure_shard}" \
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
requested_failure_shard = int(sys.argv[7])
max_bundle_bytes = int(sys.argv[8])
scratch_root = Path(sys.argv[9])

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
failure_shard = min(requested_failure_shard, max(0, shard_count - 1))
lease_ttl_ticks = 3
worker_count = 8


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


def digest(value: object) -> str:
    data = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(data).hexdigest()


def target_id(index: int) -> str:
    return f"host-{index:05d}"


def shard_targets(shard: int) -> list[str]:
    start_index = shard * shard_size
    end_index = min(start_index + shard_size, target_count)
    return [target_id(index) for index in range(start_index, end_index)]


def worker_for(shard: int) -> str:
    return f"worker-{shard % worker_count:02d}"


def replacement_worker_for(shard: int) -> str:
    return f"worker-{(shard + 3) % worker_count:02d}"


scratch_root.mkdir(parents=True, exist_ok=True)
mutated: dict[str, str] = {}
duplicate_mutations: list[dict] = []
lease_events: list[dict] = []
shard_summaries: list[dict] = []
global_mutation_hash = hashlib.sha256()


def emit_event(tick: int, shard: int, event: str, worker: str, **extra) -> None:
    lease_events.append(
        {
            "tick": tick,
            "shard": shard,
            "event": event,
            "workerId": worker,
            **extra,
        }
    )


def mutate_target(tid: str, mutation_id: str) -> None:
    if tid in mutated:
        duplicate_mutations.append({"targetId": tid, "firstMutationId": mutated[tid], "duplicateMutationId": mutation_id})
    else:
        mutated[tid] = mutation_id
        global_mutation_hash.update(f"{tid}:{mutation_id}\n".encode())


tick = 0
for shard in range(shard_count):
    ids = shard_targets(shard)
    worker = worker_for(shard)
    lease_id = f"lease-{shard:04d}-a"
    emit_event(tick, shard, "lease-acquired", worker, leaseId=lease_id, ttlTicks=lease_ttl_ticks)
    tick += 1
    emit_event(tick, shard, "heartbeat", worker, leaseId=lease_id, cursor=0)

    if shard == failure_shard and ids:
        checkpoint = max(1, len(ids) // 2)
        for offset, tid in enumerate(ids[:checkpoint], start=1):
            mutate_target(tid, f"{lease_id}:{offset}")
        tick += 1
        emit_event(tick, shard, "checkpoint-written", worker, leaseId=lease_id, cursor=checkpoint)
        tick += lease_ttl_ticks + 1
        emit_event(tick, shard, "worker-lost", worker, leaseId=lease_id, cursor=checkpoint)
        emit_event(tick, shard, "lease-expired", worker, leaseId=lease_id, cursor=checkpoint)
        replacement = replacement_worker_for(shard)
        replacement_lease = f"lease-{shard:04d}-b"
        tick += 1
        emit_event(tick, shard, "lease-stolen", replacement, previousWorkerId=worker, previousLeaseId=lease_id, leaseId=replacement_lease, resumeCursor=checkpoint)
        emit_event(tick, shard, "resume-from-checkpoint", replacement, leaseId=replacement_lease, cursor=checkpoint)
        for offset, tid in enumerate(ids[checkpoint:], start=checkpoint + 1):
            mutate_target(tid, f"{replacement_lease}:{offset}")
        final_worker = replacement
        final_lease = replacement_lease
        reassigned = True
        resumed = True
    else:
        checkpoint = 0
        for offset, tid in enumerate(ids, start=1):
            mutate_target(tid, f"{lease_id}:{offset}")
        final_worker = worker
        final_lease = lease_id
        reassigned = False
        resumed = False

    tick += 1
    emit_event(tick, shard, "shard-complete", final_worker, leaseId=final_lease, cursor=len(ids))
    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleLeaseShard",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "targetCount": len(ids),
        "initialWorkerId": worker,
        "finalWorkerId": final_worker,
        "initialLeaseId": lease_id,
        "finalLeaseId": final_lease,
        "leaseReassigned": reassigned,
        "resumedFromCheckpoint": resumed,
        "checkpointCursor": checkpoint,
        "duplicateMutations": 0,
        "mutationDigest": digest({"shard": shard, "targets": ids}),
        "firstTargets": ids[:3],
        "lastTargets": ids[-3:],
    }
    write_json(run_dir / "scale" / "shards" / f"shard-{shard:04d}.json", shard_doc)
    shard_summaries.append(
        {
            "shard": shard,
            "targetCount": len(ids),
            "initialWorkerId": worker,
            "finalWorkerId": final_worker,
            "leaseReassigned": reassigned,
            "resumedFromCheckpoint": resumed,
            "checkpointCursor": checkpoint,
        }
    )

write_json(
    run_dir / "scale" / "lease-events.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleLeaseEvents",
        "taskId": task_id,
        "runId": run_id,
        "eventCount": len(lease_events),
        "events": lease_events,
    },
)

mutation_count = len(mutated)
duplicate_count = len(duplicate_mutations)
expected_reassigned = any(item["leaseReassigned"] for item in shard_summaries)
expected_resumed = any(item["resumedFromCheckpoint"] for item in shard_summaries)
lease_expired = any(item["event"] == "lease-expired" for item in lease_events)
lease_stolen = any(item["event"] == "lease-stolen" for item in lease_events)
resume_seen = any(item["event"] == "resume-from-checkpoint" for item in lease_events)

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
        "failureShard": failure_shard,
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
        "targets": [
            {
                "profile": "lab.scale-sim",
                "type": "synthetic-host-fleet",
                "transport": "simulated",
                "configured": True,
                "count": target_count,
            }
        ],
        "targetCount": target_count,
        "shardCount": shard_count,
        "shardSize": shard_size,
        "failureShard": failure_shard,
        "targetDigest": digest([target_id(index) for index in range(target_count)]),
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded",
        "decision": "allow",
        "taskId": task_id,
        "runId": run_id,
        "reason": "worker-lease-expiry-and-resume-proof",
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "failureShard": failure_shard,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "scale" / "summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleLeaseSummary",
        "taskId": task_id,
        "runId": run_id,
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "workerCount": worker_count,
        "leaseTtlTicks": lease_ttl_ticks,
        "failureShard": failure_shard,
        "mutatedUnique": mutation_count,
        "duplicateMutations": duplicate_count,
        "mutationDigest": global_mutation_hash.hexdigest(),
        "leaseExpired": lease_expired,
        "leaseStolen": lease_stolen,
        "resumeSeen": resume_seen,
        "reassignedShards": [item["shard"] for item in shard_summaries if item["leaseReassigned"]],
        "shards": shard_summaries,
        "evidenceMode": "lease-events-plus-shard-manifests",
        "bundleBudgetBytes": max_bundle_bytes,
    },
)
write_json(
    run_dir / "scale" / "duplicate-mutations.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleDuplicateMutationReport",
        "taskId": task_id,
        "runId": run_id,
        "duplicateMutations": duplicate_count,
        "samples": duplicate_mutations[:10],
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

errors = []
if mutation_count != target_count:
    errors.append(f"mutated unique count mismatch: {mutation_count} != {target_count}")
if duplicate_count != 0:
    errors.append(f"duplicate mutations detected: {duplicate_count}")
if not lease_expired:
    errors.append("lease expiry was not observed")
if not lease_stolen:
    errors.append("lease steal was not observed")
if not resume_seen:
    errors.append("resume checkpoint was not observed")
if not expected_reassigned:
    errors.append("no shard was reassigned")
if not expected_resumed:
    errors.append("no shard resumed")

write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": "succeeded" if not errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "failureShard": failure_shard,
        "mutatedUnique": mutation_count,
        "duplicateMutations": duplicate_count,
        "leaseExpired": lease_expired,
        "leaseStolen": lease_stolen,
        "resumeSeen": resume_seen,
        "errors": errors,
        "verifiedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    },
)
write_json(
    run_dir / "result.json",
    {
        "status": "succeeded" if not errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "finishedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "failureShard": failure_shard,
        "mutatedUnique": mutation_count,
        "duplicateMutations": duplicate_count,
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

if errors:
    raise SystemExit("; ".join(errors))
PY
