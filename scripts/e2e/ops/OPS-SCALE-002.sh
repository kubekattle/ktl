#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-002.sh [options]

Options:
  --targets N            Baseline synthetic target count. Default: 10000.
  --shard-size N         Planning shard size. Default: 250.
  --change-count N       Added/removed/metadata-changed targets. Default: 100.
  --max-bundle-bytes N   Fail if exported evidence exceeds this size.
                          Default: 1048576.
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Write cleanup proof. Default.
  --no-cleanup           Keep temporary simulation scratch for debugging.
  -h, --help             Show this help.

OPS-SCALE-002 proves deterministic shard planning. It creates a 10,000-target
baseline graph, repeats the same plan three times, then creates a changed graph
with added, removed, and metadata-changed targets. Existing target IDs that
remain in the graph must keep the same shard.
EOF
}

target_count=10000
shard_size=250
change_count=100
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
    --change-count)
      [[ $# -ge 2 ]] || ops_fail "--change-count requires a value"
      change_count="$2"
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
case "${change_count}" in
  ''|*[!0-9]*) ops_fail "--change-count must be a non-negative integer" ;;
esac
case "${max_bundle_bytes}" in
  ''|*[!0-9]*) ops_fail "--max-bundle-bytes must be a positive integer" ;;
esac
[[ "${target_count}" -gt 0 ]] || ops_fail "--targets must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"
[[ "${change_count}" -le "${target_count}" ]] || ops_fail "--change-count must be <= --targets"

ops_init_run "OPS-SCALE-002"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-002.XXXXXX")"
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

ops_log "prove deterministic shard planning for ${target_count} targets"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${change_count}" \
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
from collections import defaultdict
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
target_count = int(sys.argv[5])
shard_size = int(sys.argv[6])
change_count = int(sys.argv[7])
max_bundle_bytes = int(sys.argv[8])
scratch_root = Path(sys.argv[9])

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
region_names = ["us-east", "us-west", "eu-central", "ap-south"]
zone_names = ["a", "b", "c", "d"]
role_names = ["web", "api", "worker", "db-client", "cache-client"]


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


def digest(value: object) -> str:
    data = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(data).hexdigest()


def sha256_text(text: str) -> str:
    return hashlib.sha256(text.encode()).hexdigest()


def shard_for(target_id: str) -> int:
    # Shard identity is based only on stable target ID. Metadata changes cannot
    # move an existing target to another shard.
    return int(hashlib.sha256(target_id.encode()).hexdigest()[:16], 16) % shard_count


def target_identity(index: int, mutated: bool = False, added: bool = False) -> dict:
    role = role_names[index % len(role_names)]
    zone = zone_names[index % len(zone_names)]
    if mutated:
        role = f"{role}-mutated"
        zone = f"{zone}-changed"
    return {
        "id": f"host-{index:05d}",
        "region": region_names[index % len(region_names)],
        "zone": zone,
        "role": role,
        "os": "linux",
        "source": "added" if added else "baseline",
        "metadataRevision": 2 if mutated else 1,
    }


def build_graph(start_index: int, count: int, removed=None, mutated=None, added: bool = False) -> list[dict]:
    removed = removed or set()
    mutated = mutated or set()
    out = []
    for index in range(start_index, start_index + count):
        tid = f"host-{index:05d}"
        if tid in removed:
            continue
        out.append(target_identity(index, mutated=tid in mutated, added=added))
    return out


def plan_graph(name: str, graph: list[dict], attempt: int, write_shards: bool) -> dict:
    shards: dict[int, list[dict]] = defaultdict(list)
    for target in graph:
        shards[shard_for(target["id"])].append(target)

    shard_summaries = []
    membership_hash = hashlib.sha256()
    plan_dir = run_dir / "scale" / name
    for shard in range(shard_count):
        targets = sorted(shards.get(shard, []), key=lambda item: item["id"])
        ids = [target["id"] for target in targets]
        id_text = "\n".join(ids)
        shard_digest = sha256_text(id_text)
        membership_hash.update(f"{shard}:{shard_digest}\n".encode())
        summary = {
            "shard": shard,
            "targetCount": len(ids),
            "membershipDigest": shard_digest,
            "firstTargets": ids[:3],
            "lastTargets": ids[-3:],
        }
        shard_summaries.append(summary)
        if write_shards:
            write_json(
                plan_dir / "shards" / f"shard-{shard:04d}.json",
                {
                    "apiVersion": "torque.dev/e2e/v1",
                    "kind": "OpsScaleShardPlan",
                    "taskId": task_id,
                    "runId": run_id,
                    "graph": name,
                    "attempt": attempt,
                    "shard": shard,
                    "planner": "stable-target-id-hash-v1",
                    "targetCount": len(ids),
                    "membershipDigest": shard_digest,
                    "firstTargets": ids[:10],
                    "lastTargets": ids[-10:],
                    "planDigest": digest({"graph": name, "shard": shard, "ids": ids}),
                },
            )

    graph_digest = digest({"targets": sorted(graph, key=lambda item: item["id"])})
    plan = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleShardPlanSummary",
        "taskId": task_id,
        "runId": run_id,
        "graph": name,
        "attempt": attempt,
        "planner": "stable-target-id-hash-v1",
        "targetCount": len(graph),
        "shardCount": shard_count,
        "shardSize": shard_size,
        "graphDigest": graph_digest,
        "membershipDigest": membership_hash.hexdigest(),
        "shards": shard_summaries,
    }
    write_json(plan_dir / f"attempt-{attempt}.json", plan)
    return plan


def assignment_map(graph: list[dict]) -> dict[str, int]:
    return {target["id"]: shard_for(target["id"]) for target in graph}


baseline_graph = build_graph(0, target_count)
removed_ids = {f"host-{index:05d}" for index in range(0, change_count)}
mutated_ids = {f"host-{target_count - index - 1:05d}" for index in range(change_count)}
changed_existing = build_graph(0, target_count, removed=removed_ids, mutated=mutated_ids)
added_graph = build_graph(target_count, change_count, added=True)
changed_graph = changed_existing + added_graph

baseline_attempts = [plan_graph("baseline", baseline_graph, attempt, write_shards=(attempt == 1)) for attempt in range(1, 4)]
changed_plan = plan_graph("changed", changed_graph, 1, write_shards=True)
baseline_assignments = assignment_map(baseline_graph)
changed_assignments = assignment_map(changed_graph)

stable_checked = 0
moved = []
for target_id, baseline_shard in baseline_assignments.items():
    if target_id not in changed_assignments:
        continue
    stable_checked += 1
    if changed_assignments[target_id] != baseline_shard:
        moved.append(
            {
                "targetId": target_id,
                "baselineShard": baseline_shard,
                "changedShard": changed_assignments[target_id],
            }
        )

retry_digests = [plan["membershipDigest"] for plan in baseline_attempts]
retry_graph_digests = [plan["graphDigest"] for plan in baseline_attempts]
retry_stable = len(set(retry_digests)) == 1 and len(set(retry_graph_digests)) == 1
changed_added_ids = sorted(target["id"] for target in added_graph)
changed_kept_ids = sorted(set(changed_assignments) & set(baseline_assignments))

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
        "changeCount": change_count,
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
        "baseline": {
            "targetCount": len(baseline_graph),
            "graphDigest": baseline_attempts[0]["graphDigest"],
            "membershipDigest": baseline_attempts[0]["membershipDigest"],
        },
        "changed": {
            "targetCount": len(changed_graph),
            "graphDigest": changed_plan["graphDigest"],
            "membershipDigest": changed_plan["membershipDigest"],
            "removed": len(removed_ids),
            "added": len(added_graph),
            "metadataChanged": len(mutated_ids),
        },
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded",
        "decision": "allow",
        "taskId": task_id,
        "runId": run_id,
        "reason": "deterministic-shard-planner-proof",
        "labProfile": "lab.scale-sim",
        "planner": "stable-target-id-hash-v1",
        "targetCount": target_count,
        "shardCount": shard_count,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "scale" / "membership-diff.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleMembershipDiff",
        "taskId": task_id,
        "runId": run_id,
        "baselineTargetCount": len(baseline_graph),
        "changedTargetCount": len(changed_graph),
        "removedTargetCount": len(removed_ids),
        "addedTargetCount": len(added_graph),
        "metadataChangedCount": len(mutated_ids),
        "stableTargetsChecked": stable_checked,
        "movedExistingTargets": len(moved),
        "movedSamples": moved[:10],
        "addedSamples": changed_added_ids[:10],
        "keptSamples": changed_kept_ids[:10],
    },
)
write_json(
    run_dir / "scale" / "summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleShardPlannerSummary",
        "taskId": task_id,
        "runId": run_id,
        "planner": "stable-target-id-hash-v1",
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "changeCount": change_count,
        "retryAttempts": len(baseline_attempts),
        "retryMembershipDigests": retry_digests,
        "retryStable": retry_stable,
        "baselineMembershipDigest": baseline_attempts[0]["membershipDigest"],
        "changedMembershipDigest": changed_plan["membershipDigest"],
        "unchangedTargetsMoved": len(moved),
        "stableTargetsChecked": stable_checked,
        "evidenceMode": "per-shard-plan-manifests",
        "bundleBudgetBytes": max_bundle_bytes,
    },
)
scratch_root.mkdir(parents=True, exist_ok=True)
write_json(
    scratch_root / "debug.json",
    {
        "baselineMembershipDigest": baseline_attempts[0]["membershipDigest"],
        "changedMembershipDigest": changed_plan["membershipDigest"],
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
        "targetsPerSecond": round((target_count * 4) / max(duration_ms / 1000, 0.001), 2),
    },
)

errors = []
if not retry_stable:
    errors.append("baseline retry attempts produced different membership digests")
if moved:
    errors.append(f"{len(moved)} existing targets moved shards after changed graph")
if stable_checked != target_count - len(removed_ids):
    errors.append("stable target check count mismatch")
if len(changed_graph) != target_count - len(removed_ids) + len(added_graph):
    errors.append("changed graph target count mismatch")
if changed_plan["membershipDigest"] == baseline_attempts[0]["membershipDigest"]:
    errors.append("changed graph membership digest should differ after add/remove")

write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": "succeeded" if not errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "labProfile": "lab.scale-sim",
        "planner": "stable-target-id-hash-v1",
        "targetCount": target_count,
        "shardCount": shard_count,
        "retryAttempts": len(baseline_attempts),
        "retryStable": retry_stable,
        "stableTargetsChecked": stable_checked,
        "unchangedTargetsMoved": len(moved),
        "changedGraphTargetCount": len(changed_graph),
        "removedTargetCount": len(removed_ids),
        "addedTargetCount": len(added_graph),
        "metadataChangedCount": len(mutated_ids),
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
        "retryStable": retry_stable,
        "unchangedTargetsMoved": len(moved),
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

if errors:
    raise SystemExit("; ".join(errors))
PY
