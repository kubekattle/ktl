#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-007.sh [options]

Options:
  --targets N              Synthetic target count. Default: 10000.
  --shard-size N           Targets per shard. Default: 250.
  --global-limit N         Maximum active targets globally. Default: 160.
  --per-shard-limit N      Maximum active targets in one shard. Default: 8.
  --adapter-limit N        Maximum active targets per adapter. Default: 70.
  --provider-limit N       Maximum active targets per provider. Default: 55.
  --retry-storm-targets N  Targets that fail transiently before pause.
                            Default: 640.
  --retry-attempts N       Transient failures per storm target. Default: 4.
  --pause-threshold N      Retry events that trigger a safe pause.
                            Default: 120.
  --max-bundle-bytes N     Fail if exported evidence exceeds this size.
                            Default: 1048576.
  --evidence-root DIR      Evidence root. Defaults to a temp directory.
  --cleanup                Clean temporary scratch. Default.
  --no-cleanup             Leave temporary scratch for debugging.
  -h, --help               Show this help.

OPS-SCALE-007 proves backpressure and blast-radius control. It simulates a
10,000-target run with per-shard, per-adapter, per-provider, and global rate
limits, injects a retry storm, pauses before broad fan-out can continue, and
records decision evidence proving no new mutations were admitted after pause.
EOF
}

target_count=10000
shard_size=250
global_limit=160
per_shard_limit=8
adapter_limit=70
provider_limit=55
retry_storm_targets=640
retry_attempts=4
pause_threshold=120
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
    --global-limit)
      [[ $# -ge 2 ]] || ops_fail "--global-limit requires a value"
      global_limit="$2"
      shift 2
      ;;
    --per-shard-limit)
      [[ $# -ge 2 ]] || ops_fail "--per-shard-limit requires a value"
      per_shard_limit="$2"
      shift 2
      ;;
    --adapter-limit)
      [[ $# -ge 2 ]] || ops_fail "--adapter-limit requires a value"
      adapter_limit="$2"
      shift 2
      ;;
    --provider-limit)
      [[ $# -ge 2 ]] || ops_fail "--provider-limit requires a value"
      provider_limit="$2"
      shift 2
      ;;
    --retry-storm-targets)
      [[ $# -ge 2 ]] || ops_fail "--retry-storm-targets requires a value"
      retry_storm_targets="$2"
      shift 2
      ;;
    --retry-attempts)
      [[ $# -ge 2 ]] || ops_fail "--retry-attempts requires a value"
      retry_attempts="$2"
      shift 2
      ;;
    --pause-threshold)
      [[ $# -ge 2 ]] || ops_fail "--pause-threshold requires a value"
      pause_threshold="$2"
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

for numeric in \
  "targets:${target_count}" \
  "shard-size:${shard_size}" \
  "global-limit:${global_limit}" \
  "per-shard-limit:${per_shard_limit}" \
  "adapter-limit:${adapter_limit}" \
  "provider-limit:${provider_limit}" \
  "retry-storm-targets:${retry_storm_targets}" \
  "retry-attempts:${retry_attempts}" \
  "pause-threshold:${pause_threshold}" \
  "max-bundle-bytes:${max_bundle_bytes}"; do
  name="${numeric%%:*}"
  value="${numeric#*:}"
  case "${value}" in
    ''|*[!0-9]*) ops_fail "--${name} must be a positive integer" ;;
  esac
  [[ "${value}" -gt 0 ]] || ops_fail "--${name} must be > 0"
done

[[ "${retry_storm_targets}" -le "${target_count}" ]] || ops_fail "--retry-storm-targets must be <= --targets"

ops_init_run "OPS-SCALE-007"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-007.XXXXXX")"
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

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
} | ops_redact_stdin "${OPS_RUN_DIR}/redaction/probe.redacted.txt"

ops_log "prove backpressure pause for ${target_count} targets"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${global_limit}" \
  "${per_shard_limit}" \
  "${adapter_limit}" \
  "${provider_limit}" \
  "${retry_storm_targets}" \
  "${retry_attempts}" \
  "${pause_threshold}" \
  "${max_bundle_bytes}" \
  "${scratch_root}" <<'PY'
from __future__ import annotations

import hashlib
import json
import math
import platform
import resource
import sys
import time
from collections import Counter, defaultdict, deque
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
target_count = int(sys.argv[5])
shard_size = int(sys.argv[6])
global_limit = int(sys.argv[7])
per_shard_limit = int(sys.argv[8])
adapter_limit = int(sys.argv[9])
provider_limit = int(sys.argv[10])
retry_storm_targets = int(sys.argv[11])
retry_attempts = int(sys.argv[12])
pause_threshold = int(sys.argv[13])
max_bundle_bytes = int(sys.argv[14])
scratch_root = Path(sys.argv[15])

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
adapters = [
    "host.command.run",
    "host.package.install",
    "host.service.manage",
    "http.check",
]
providers = [
    "ssh",
    "apt-mirror",
    "systemd",
    "http-probe",
]


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


def digest_json(value: object) -> str:
    data = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(data).hexdigest()


def target_id(index: int) -> str:
    return f"host-{index:05d}"


def shard_for(index: int) -> int:
    return index // shard_size


def adapter_for(index: int) -> str:
    bucket = index % 20
    if bucket < 11:
        return adapters[0]
    if bucket < 16:
        return adapters[1]
    if bucket < 19:
        return adapters[2]
    return adapters[3]


def provider_for(index: int) -> str:
    bucket = index % 20
    if bucket < 12:
        return providers[0]
    if bucket < 15:
        return providers[1]
    if bucket < 18:
        return providers[2]
    return providers[3]


def duration_for(index: int, attempt: int) -> int:
    return 2 + ((index + attempt) % 3)


def interleaved_targets() -> list[int]:
    ordered = []
    for offset in range(shard_size):
        for shard in range(shard_count):
            index = shard * shard_size + offset
            if index < target_count:
                ordered.append(index)
    return ordered


scratch_root.mkdir(parents=True, exist_ok=True)
shards_dir = run_dir / "scale" / "shards"
shards_dir.mkdir(parents=True, exist_ok=True)

targets = []
for index in range(target_count):
    target = {
        "index": index,
        "targetId": target_id(index),
        "shard": shard_for(index),
        "adapter": adapter_for(index),
        "provider": provider_for(index),
        "storm": index < retry_storm_targets,
    }
    targets.append(target)

intent_digest = digest_json(
    {
        "taskId": task_id,
        "targetCount": target_count,
        "shardSize": shard_size,
        "limits": {
            "global": global_limit,
            "perShard": per_shard_limit,
            "perAdapter": adapter_limit,
            "perProvider": provider_limit,
        },
        "retryStormTargets": retry_storm_targets,
        "retryAttempts": retry_attempts,
        "pauseThreshold": pause_threshold,
    }
)

pending = deque(interleaved_targets())
attempts = defaultdict(int)
active = []
completed = set()
checkpointed = set()
paused_backlog = set()
admitted_before_pause = set()
admissions_by_shard = Counter()
completed_by_shard = Counter()
checkpointed_by_shard = Counter()
retry_events_by_shard = Counter()
rate_blocks = Counter()
rate_blocks_by_shard = defaultdict(Counter)
rate_blocks_by_adapter = defaultdict(Counter)
rate_blocks_by_provider = defaultdict(Counter)
event_samples = []
max_active_global = 0
max_active_by_shard = Counter()
max_active_by_adapter = Counter()
max_active_by_provider = Counter()
retry_events = 0
mutation_receipts = 0
pause_tick = None
pause_reason = ""
mutations_after_pause = 0
admissions_after_pause = 0
max_ticks = max(1000, target_count * 2)


def sample_event(event: dict) -> None:
    if len(event_samples) < 120:
        event_samples.append(event)


def active_counts() -> tuple[Counter, Counter, Counter]:
    by_shard = Counter()
    by_adapter = Counter()
    by_provider = Counter()
    for item in active:
        target = targets[item["index"]]
        by_shard[target["shard"]] += 1
        by_adapter[target["adapter"]] += 1
        by_provider[target["provider"]] += 1
    return by_shard, by_adapter, by_provider


for tick in range(max_ticks):
    next_active = []
    for item in active:
        item["remaining"] -= 1
        index = item["index"]
        target = targets[index]
        if item["remaining"] > 0:
            next_active.append(item)
            continue

        if target["storm"] and item["attempt"] <= retry_attempts:
            retry_events += 1
            retry_events_by_shard[target["shard"]] += 1
            sample_event(
                {
                    "tick": tick,
                    "event": "transient-failure",
                    "targetId": target["targetId"],
                    "shard": target["shard"],
                    "adapter": target["adapter"],
                    "provider": target["provider"],
                    "attempt": item["attempt"],
                    "retryEvents": retry_events,
                }
            )
            if retry_events >= pause_threshold and pause_tick is None:
                pause_tick = tick
                pause_reason = "retry-storm-threshold-exceeded"
            else:
                pending.append(index)
            continue

        completed.add(index)
        completed_by_shard[target["shard"]] += 1
        mutation_receipts += 1
        if pause_tick is not None and tick > pause_tick:
            mutations_after_pause += 1
        sample_event(
            {
                "tick": tick,
                "event": "mutation-receipt",
                "targetId": target["targetId"],
                "shard": target["shard"],
                "adapter": target["adapter"],
                "provider": target["provider"],
                "attempt": item["attempt"],
            }
        )

    active = next_active

    if pause_tick is not None:
        for item in active:
            index = item["index"]
            checkpointed.add(index)
            checkpointed_by_shard[targets[index]["shard"]] += 1
        paused_backlog.update(pending)
        pending.clear()
        active = []
        sample_event(
            {
                "tick": tick,
                "event": "safe-pause",
                "reason": pause_reason,
                "retryEvents": retry_events,
                "checkpointedTargets": len(checkpointed),
                "pausedBacklogTargets": len(paused_backlog),
            }
        )
        break

    by_shard, by_adapter, by_provider = active_counts()
    admitted_this_tick = 0
    scanned = 0
    while pending and scanned < len(pending):
        if len(active) >= global_limit:
            rate_blocks["global"] += 1
            sample_event(
                {
                    "tick": tick,
                    "event": "rate-limited",
                    "scope": "global",
                    "active": len(active),
                    "limit": global_limit,
                }
            )
            break

        index = pending[0]
        target = targets[index]
        limit_scope = ""
        active_value = 0
        limit_value = 0
        if by_shard[target["shard"]] >= per_shard_limit:
            limit_scope = "shard"
            active_value = by_shard[target["shard"]]
            limit_value = per_shard_limit
        elif by_adapter[target["adapter"]] >= adapter_limit:
            limit_scope = "adapter"
            active_value = by_adapter[target["adapter"]]
            limit_value = adapter_limit
        elif by_provider[target["provider"]] >= provider_limit:
            limit_scope = "provider"
            active_value = by_provider[target["provider"]]
            limit_value = provider_limit

        if limit_scope:
            pending.rotate(-1)
            scanned += 1
            rate_blocks[limit_scope] += 1
            rate_blocks_by_shard[target["shard"]][limit_scope] += 1
            rate_blocks_by_adapter[target["adapter"]][limit_scope] += 1
            rate_blocks_by_provider[target["provider"]][limit_scope] += 1
            sample_event(
                {
                    "tick": tick,
                    "event": "rate-limited",
                    "scope": limit_scope,
                    "targetId": target["targetId"],
                    "shard": target["shard"],
                    "adapter": target["adapter"],
                    "provider": target["provider"],
                    "active": active_value,
                    "limit": limit_value,
                }
            )
            continue

        pending.popleft()
        attempts[index] += 1
        attempt = attempts[index]
        by_shard[target["shard"]] += 1
        by_adapter[target["adapter"]] += 1
        by_provider[target["provider"]] += 1
        active.append(
            {
                "index": index,
                "attempt": attempt,
                "remaining": duration_for(index, attempt),
            }
        )
        admitted_this_tick += 1
        admitted_before_pause.add(index)
        admissions_by_shard[target["shard"]] += 1
        max_active_global = max(max_active_global, len(active))
        max_active_by_shard[target["shard"]] = max(
            max_active_by_shard[target["shard"]], by_shard[target["shard"]]
        )
        max_active_by_adapter[target["adapter"]] = max(
            max_active_by_adapter[target["adapter"]], by_adapter[target["adapter"]]
        )
        max_active_by_provider[target["provider"]] = max(
            max_active_by_provider[target["provider"]], by_provider[target["provider"]]
        )
        sample_event(
            {
                "tick": tick,
                "event": "admitted",
                "targetId": target["targetId"],
                "shard": target["shard"],
                "adapter": target["adapter"],
                "provider": target["provider"],
                "attempt": attempt,
            }
        )

    if admitted_this_tick == 0 and not active and pending:
        pause_tick = tick
        pause_reason = "scheduler-deadlock"
        paused_backlog.update(pending)
        pending.clear()
        break

if pause_tick is None:
    pause_tick = max_ticks
    pause_reason = "pause-not-triggered"
    paused_backlog.update(pending)
    checkpointed.update(item["index"] for item in active)
    active = []
    pending.clear()

shard_summaries = []
for shard in range(shard_count):
    shard_targets = [
        target for target in targets if target["shard"] == shard
    ]
    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleBackpressureShardManifest",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "targetCount": len(shard_targets),
        "admittedBeforePause": admissions_by_shard[shard],
        "completedBeforePause": completed_by_shard[shard],
        "checkpointedAtPause": checkpointed_by_shard[shard],
        "pausedBacklog": sum(1 for target in shard_targets if target["index"] in paused_backlog),
        "retryEvents": retry_events_by_shard[shard],
        "rateLimitBlocks": dict(rate_blocks_by_shard[shard]),
        "maxActive": max_active_by_shard[shard],
        "intentDigest": intent_digest,
    }
    write_json(shards_dir / f"shard-{shard:04d}.json", shard_doc)
    shard_summaries.append(
        {
            "shard": shard,
            "targetCount": len(shard_targets),
            "admittedBeforePause": admissions_by_shard[shard],
            "completedBeforePause": completed_by_shard[shard],
            "checkpointedAtPause": checkpointed_by_shard[shard],
            "pausedBacklog": shard_doc["pausedBacklog"],
            "retryEvents": retry_events_by_shard[shard],
            "maxActive": max_active_by_shard[shard],
        }
    )

policy = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleBackpressurePolicy",
    "taskId": task_id,
    "runId": run_id,
    "targetCount": target_count,
    "shardSize": shard_size,
    "shardCount": shard_count,
    "intentDigest": intent_digest,
    "limits": {
        "globalActiveTargets": global_limit,
        "perShardActiveTargets": per_shard_limit,
        "perAdapterActiveTargets": adapter_limit,
        "perProviderActiveTargets": provider_limit,
        "retryStormPauseThreshold": pause_threshold,
    },
    "blastRadius": {
        "pauseOnRetryStorm": True,
        "admitAfterPause": False,
        "checkpointActiveTargets": True,
        "boundedEvidence": True,
    },
}

pause_decision = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleBackpressurePauseDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "pause",
    "reason": pause_reason,
    "pauseTick": pause_tick,
    "retryEvents": retry_events,
    "pauseThreshold": pause_threshold,
    "admittedBeforePause": len(admitted_before_pause),
    "completedBeforePause": len(completed),
    "checkpointedAtPause": len(checkpointed),
    "pausedBacklogTargets": len(paused_backlog),
    "admissionsAfterPause": admissions_after_pause,
    "mutationsAfterPause": mutations_after_pause,
}

rate_limit_summary = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleBackpressureRateLimitSummary",
    "taskId": task_id,
    "runId": run_id,
    "rateLimitBlocks": dict(rate_blocks),
    "maxActiveGlobal": max_active_global,
    "maxActiveByAdapter": dict(max_active_by_adapter),
    "maxActiveByProvider": dict(max_active_by_provider),
    "maxActiveByShard": {
        str(shard): max_active_by_shard[shard]
        for shard in range(shard_count)
        if max_active_by_shard[shard]
    },
    "adapterLimitBlocks": {
        adapter: dict(blocks)
        for adapter, blocks in sorted(rate_blocks_by_adapter.items())
    },
    "providerLimitBlocks": {
        provider: dict(blocks)
        for provider, blocks in sorted(rate_blocks_by_provider.items())
    },
}

retry_storm = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleRetryStorm",
    "taskId": task_id,
    "runId": run_id,
    "stormTargetCount": retry_storm_targets,
    "retryAttemptsPerTarget": retry_attempts,
    "retryEventsObserved": retry_events,
    "pauseThreshold": pause_threshold,
    "pauseTriggered": pause_reason == "retry-storm-threshold-exceeded",
    "retryEventsByShard": dict(retry_events_by_shard),
}

duration_ms = int((time.monotonic() - start) * 1000)
summary = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleBackpressureSummary",
    "taskId": task_id,
    "runId": run_id,
    "targetCount": target_count,
    "shardSize": shard_size,
    "shardCount": shard_count,
    "intentDigest": intent_digest,
    "pauseDecision": pause_decision,
    "rateLimitBlocks": dict(rate_blocks),
    "mutationReceiptsBeforePause": mutation_receipts,
    "admissionsAfterPause": admissions_after_pause,
    "mutationsAfterPause": mutations_after_pause,
    "shardManifestCount": len(shard_summaries),
    "bundleBudgetBytes": max_bundle_bytes,
    "durationMs": duration_ms,
    "pythonMaxRss": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss,
    "platform": platform.platform(),
    "shards": shard_summaries,
}

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
        "retryStormTargets": retry_storm_targets,
        "pauseThreshold": pause_threshold,
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
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "blocked",
        "decision": "pause",
        "taskId": task_id,
        "runId": run_id,
        "reason": pause_reason,
        "labProfile": "lab.scale-sim",
        "retryEvents": retry_events,
        "pauseThreshold": pause_threshold,
        "admissionsAfterPause": admissions_after_pause,
        "mutationsAfterPause": mutations_after_pause,
        "decidedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    },
)
write_json(run_dir / "scale" / "backpressure-policy.json", policy)
write_json(run_dir / "scale" / "pause-decision.json", pause_decision)
write_json(run_dir / "scale" / "rate-limit-summary.json", rate_limit_summary)
write_json(run_dir / "scale" / "retry-storm.json", retry_storm)
write_json(run_dir / "scale" / "event-samples.json", {"events": event_samples})
write_json(run_dir / "scale" / "summary.json", summary)

errors = []
if pause_reason != "retry-storm-threshold-exceeded":
    errors.append(f"expected retry storm pause, got {pause_reason}")
if retry_events < pause_threshold:
    errors.append("retry storm did not reach pause threshold")
if rate_blocks["global"] <= 0:
    errors.append("global rate limit was not exercised")
if rate_blocks["shard"] <= 0:
    errors.append("per-shard rate limit was not exercised")
if rate_blocks["adapter"] <= 0:
    errors.append("per-adapter rate limit was not exercised")
if rate_blocks["provider"] <= 0:
    errors.append("per-provider rate limit was not exercised")
if admissions_after_pause != 0:
    errors.append("scheduler admitted work after pause")
if mutations_after_pause != 0:
    errors.append("scheduler produced mutation receipts after pause")
if not checkpointed:
    errors.append("pause did not checkpoint active targets")
if not paused_backlog:
    errors.append("pause did not leave bounded backlog")
if len(shard_summaries) != shard_count:
    errors.append("shard manifest count mismatch")
if max_active_global > global_limit:
    errors.append("global active target limit was exceeded")
if any(value > per_shard_limit for value in max_active_by_shard.values()):
    errors.append("per-shard active target limit was exceeded")
if any(value > adapter_limit for value in max_active_by_adapter.values()):
    errors.append("per-adapter active target limit was exceeded")
if any(value > provider_limit for value in max_active_by_provider.values()):
    errors.append("per-provider active target limit was exceeded")

write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": "succeeded" if not errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "retryEvents": retry_events,
        "pauseThreshold": pause_threshold,
        "pauseReason": pause_reason,
        "rateLimitBlocks": dict(rate_blocks),
        "admissionsAfterPause": admissions_after_pause,
        "mutationsAfterPause": mutations_after_pause,
        "checkpointedAtPause": len(checkpointed),
        "pausedBacklogTargets": len(paused_backlog),
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
        "decision": "pause",
        "pauseReason": pause_reason,
        "retryEvents": retry_events,
        "rateLimitBlocks": dict(rate_blocks),
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

if errors:
    raise SystemExit("; ".join(errors))
PY
