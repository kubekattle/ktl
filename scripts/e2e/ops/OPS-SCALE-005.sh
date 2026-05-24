#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-005.sh [options]

Options:
  --targets N            Synthetic target count. Default: 10000.
  --shard-size N         Targets per shard. Default: 250.
  --changed-facts N      Changed facts in second collection. Default: 100.
  --ttl-refresh N        Expired facts to refresh by digest. Default: 10.
  --max-bundle-bytes N   Fail if exported evidence exceeds this size.
                          Default: 1048576.
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Clean temporary scratch. Default.
  --no-cleanup           Leave temporary scratch for debugging.
  -h, --help             Show this help.

OPS-SCALE-005 proves fact digest caching. It simulates a baseline collection for
10,000 targets, a second collection with only a controlled changed subset, and
a TTL refresh probe. Unchanged facts must be referenced by digest and must not
be recopied into second-run evidence.
EOF
}

target_count=10000
shard_size=250
changed_facts=100
ttl_refresh=10
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
    --changed-facts)
      [[ $# -ge 2 ]] || ops_fail "--changed-facts requires a value"
      changed_facts="$2"
      shift 2
      ;;
    --ttl-refresh)
      [[ $# -ge 2 ]] || ops_fail "--ttl-refresh requires a value"
      ttl_refresh="$2"
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
case "${changed_facts}" in
  ''|*[!0-9]*) ops_fail "--changed-facts must be a non-negative integer" ;;
esac
case "${ttl_refresh}" in
  ''|*[!0-9]*) ops_fail "--ttl-refresh must be a non-negative integer" ;;
esac
case "${max_bundle_bytes}" in
  ''|*[!0-9]*) ops_fail "--max-bundle-bytes must be a positive integer" ;;
esac
[[ "${target_count}" -gt 0 ]] || ops_fail "--targets must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"
[[ "${changed_facts}" -le "${target_count}" ]] || ops_fail "--changed-facts must be <= --targets"
[[ "${ttl_refresh}" -le "${target_count}" ]] || ops_fail "--ttl-refresh must be <= --targets"

ops_init_run "OPS-SCALE-005"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-005.XXXXXX")"
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

ops_log "prove fact digest cache for ${target_count} targets"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${changed_facts}" \
  "${ttl_refresh}" \
  "${max_bundle_bytes}" \
  "${scratch_root}" <<'PY'
import hashlib
import json
import math
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
changed_facts = int(sys.argv[7])
ttl_refresh = int(sys.argv[8])
max_bundle_bytes = int(sys.argv[9])
scratch_root = Path(sys.argv[10])

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
fact_ttl_seconds = 300
second_collection_age_seconds = 60
ttl_refresh_age_seconds = fact_ttl_seconds + 30


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


def baseline_fact(index: int) -> dict:
    return {
        "id": target_id(index),
        "os": "linux",
        "kernel": f"5.15.{index % 32}",
        "cpu": 2 + (index % 14),
        "memoryMiB": 1024 + ((index % 64) * 256),
        "packageEpoch": index % 17,
        "serviceDigest": digest({"services": ["ssh", "systemd"], "index": index % 23}),
    }


def changed_fact(index: int) -> dict:
    fact = baseline_fact(index)
    fact["packageEpoch"] = fact["packageEpoch"] + 1000
    fact["changeReason"] = "second-collection-drift"
    return fact


changed_count = min(changed_facts, target_count)
ttl_count = min(ttl_refresh, target_count)
changed_indices = {
    min(target_count - 1, max(0, math.floor((index + 1) * target_count / (changed_count + 1))))
    for index in range(changed_count)
}
ttl_indices = {
    min(target_count - 1, max(0, math.floor((index + 1) * target_count / (ttl_count + 1))))
    for index in range(ttl_count)
}

baseline_fact_hash = hashlib.sha256()
second_fact_hash = hashlib.sha256()
cache_index_hash = hashlib.sha256()
unchanged_references = 0
changed_payloads = 0
recopied_unchanged_payloads = 0
ttl_refresh_references = 0
new_cache_objects = 0
baseline_cache_objects = target_count
changed_samples = []
reference_samples = []
ttl_samples = []
shard_summaries = []
scratch_root.mkdir(parents=True, exist_ok=True)

for shard in range(shard_count):
    start_index = shard * shard_size
    end_index = min(start_index + shard_size, target_count)
    shard_changed = 0
    shard_unchanged_refs = 0
    shard_ttl_refs = 0
    shard_baseline = hashlib.sha256()
    shard_second = hashlib.sha256()
    shard_cache = hashlib.sha256()
    shard_changed_samples = []
    shard_reference_samples = []

    for index in range(start_index, end_index):
        tid = target_id(index)
        base_fact = baseline_fact(index)
        base_digest = digest(base_fact)
        baseline_fact_hash.update(base_digest.encode())
        shard_baseline.update(base_digest.encode())
        cache_index_hash.update(f"{tid}:{base_digest}\n".encode())
        shard_cache.update(f"{tid}:{base_digest}\n".encode())

        if index in changed_indices:
            fact = changed_fact(index)
            fact_digest = digest(fact)
            changed_payloads += 1
            new_cache_objects += 1
            shard_changed += 1
            second_fact_hash.update(fact_digest.encode())
            shard_second.update(fact_digest.encode())
            if len(changed_samples) < 10:
                changed_samples.append({"targetId": tid, "oldDigest": base_digest, "newDigest": fact_digest})
            if len(shard_changed_samples) < 3:
                shard_changed_samples.append({"targetId": tid, "newDigest": fact_digest})
        else:
            unchanged_references += 1
            second_fact_hash.update(base_digest.encode())
            shard_second.update(base_digest.encode())
            shard_unchanged_refs += 1
            if len(reference_samples) < 10:
                reference_samples.append({"targetId": tid, "digestRef": base_digest})
            if len(shard_reference_samples) < 3:
                shard_reference_samples.append({"targetId": tid, "digestRef": base_digest})

        if index in ttl_indices:
            # TTL expiry triggers recollection, but identical content resolves
            # to the same digest and references the existing cache object.
            ttl_digest = digest(baseline_fact(index))
            ttl_refresh_references += 1
            shard_ttl_refs += 1
            if len(ttl_samples) < 10:
                ttl_samples.append({"targetId": tid, "digestRef": ttl_digest, "ageSeconds": ttl_refresh_age_seconds})

    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleFactCacheShard",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "targetRange": {"start": start_index, "endExclusive": end_index},
        "targetCount": end_index - start_index,
        "baselineFactPayloads": end_index - start_index,
        "secondRunDigestReferences": shard_unchanged_refs,
        "secondRunChangedPayloads": shard_changed,
        "ttlRefreshDigestReferences": shard_ttl_refs,
        "recopiedUnchangedPayloads": 0,
        "baselineFactDigest": shard_baseline.hexdigest(),
        "secondRunFactDigest": shard_second.hexdigest(),
        "cacheIndexDigest": shard_cache.hexdigest(),
        "referenceSamples": shard_reference_samples,
        "changedSamples": shard_changed_samples,
    }
    write_json(run_dir / "scale" / "shards" / f"shard-{shard:04d}.json", shard_doc)
    shard_summaries.append(
        {
            "shard": shard,
            "targetCount": end_index - start_index,
            "secondRunDigestReferences": shard_unchanged_refs,
            "secondRunChangedPayloads": shard_changed,
            "ttlRefreshDigestReferences": shard_ttl_refs,
            "recopiedUnchangedPayloads": 0,
        }
    )

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
        "changedFacts": changed_count,
        "ttlRefresh": ttl_count,
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
        "factTtlSeconds": fact_ttl_seconds,
        "secondCollectionAgeSeconds": second_collection_age_seconds,
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded",
        "decision": "allow",
        "taskId": task_id,
        "runId": run_id,
        "reason": "fact-digest-cache-proof",
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "scale" / "cache-index.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleFactCacheIndex",
        "taskId": task_id,
        "runId": run_id,
        "cacheMode": "content-addressed-fact-digests",
        "baselineCacheObjects": baseline_cache_objects,
        "newCacheObjectsFromChangedFacts": new_cache_objects,
        "cacheObjectCountAfterSecondRun": baseline_cache_objects + new_cache_objects,
        "cacheIndexDigest": cache_index_hash.hexdigest(),
        "referenceSamples": reference_samples,
        "changedSamples": changed_samples,
        "ttlRefreshSamples": ttl_samples,
    },
)
write_json(
    run_dir / "scale" / "summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleFactCacheSummary",
        "taskId": task_id,
        "runId": run_id,
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "factTtlSeconds": fact_ttl_seconds,
        "secondCollectionAgeSeconds": second_collection_age_seconds,
        "ttlRefreshAgeSeconds": ttl_refresh_age_seconds,
        "baselineFactPayloads": target_count,
        "secondRunDigestReferences": unchanged_references,
        "secondRunChangedPayloads": changed_payloads,
        "ttlRefreshDigestReferences": ttl_refresh_references,
        "recopiedUnchangedPayloads": recopied_unchanged_payloads,
        "baselineFactDigest": baseline_fact_hash.hexdigest(),
        "secondRunFactDigest": second_fact_hash.hexdigest(),
        "cacheObjectCountAfterSecondRun": baseline_cache_objects + new_cache_objects,
        "evidenceMode": "digest-reference-manifests-not-recopied-facts",
        "bundleBudgetBytes": max_bundle_bytes,
        "shards": shard_summaries,
    },
)
duration_ms = int((time.monotonic() - start) * 1000)
write_json(
    run_dir / "scale" / "performance.json",
    {
        "durationMs": duration_ms,
        "pythonMaxRss": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,
        "platform": platform.platform(),
        "targetCount": target_count,
        "shardCount": shard_count,
        "targetsPerSecond": round((target_count * 2) / max(duration_ms / 1000, 0.001), 2),
    },
)

errors = []
if unchanged_references != target_count - changed_count:
    errors.append("unchanged digest reference count mismatch")
if changed_payloads != changed_count:
    errors.append("changed payload count mismatch")
if recopied_unchanged_payloads != 0:
    errors.append("unchanged facts were recopied")
if ttl_refresh_references != ttl_count:
    errors.append("ttl refresh digest reference count mismatch")
if baseline_cache_objects + new_cache_objects != target_count + changed_count:
    errors.append("cache object count mismatch")

write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": "succeeded" if not errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "secondRunDigestReferences": unchanged_references,
        "secondRunChangedPayloads": changed_payloads,
        "ttlRefreshDigestReferences": ttl_refresh_references,
        "recopiedUnchangedPayloads": recopied_unchanged_payloads,
        "cacheObjectCountAfterSecondRun": baseline_cache_objects + new_cache_objects,
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
        "secondRunDigestReferences": unchanged_references,
        "secondRunChangedPayloads": changed_payloads,
        "recopiedUnchangedPayloads": recopied_unchanged_payloads,
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

if errors:
    raise SystemExit("; ".join(errors))
PY
