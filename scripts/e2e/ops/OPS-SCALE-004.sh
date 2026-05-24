#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-004.sh [options]

Options:
  --targets N            Synthetic target count. Default: 10000.
  --shard-size N         Targets per shard. Default: 250.
  --conflicts N          Synthetic conflicting write attempts. Default: 200.
  --max-bundle-bytes N   Fail if exported evidence exceeds this size.
                          Default: 1048576.
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --skip-ssh-canary      Skip real lab.ssh-linux canary. Debug only.
  --cleanup              Clean lab resources. Default.
  --no-cleanup           Leave lab resources for debugging.
  -h, --help             Show this help.

OPS-SCALE-004 proves distributed target/object locks. It simulates target and
object locks across workers, proves conflicting synthetic writes are blocked,
then runs a real lab.ssh-linux canary using an isolated remote temp directory
and an atomic lock directory.

Environment for real canary:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@lab-host
  TORQUE_LAB_SSH_IDENTITY=/path/to/key       optional
  TORQUE_LAB_SSH_OPTS="..."                 optional
EOF
}

target_count=10000
shard_size=250
conflicts=200
max_bundle_bytes=1048576
cleanup_enabled=1
skip_ssh_canary=0

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
    --conflicts)
      [[ $# -ge 2 ]] || ops_fail "--conflicts requires a value"
      conflicts="$2"
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
    --skip-ssh-canary)
      skip_ssh_canary=1
      shift
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
case "${conflicts}" in
  ''|*[!0-9]*) ops_fail "--conflicts must be a positive integer" ;;
esac
case "${max_bundle_bytes}" in
  ''|*[!0-9]*) ops_fail "--max-bundle-bytes must be a positive integer" ;;
esac
[[ "${target_count}" -gt 0 ]] || ops_fail "--targets must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"
[[ "${conflicts}" -gt 0 ]] || ops_fail "--conflicts must be > 0"

if [[ "${skip_ssh_canary}" != "1" ]]; then
  [[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux canary without TORQUE_OPS_E2E_CONFIRM=1"
  ops_require_env TORQUE_LAB_SSH
  ops_require_cmd ssh
fi

ops_init_run "OPS-SCALE-004"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-004.XXXXXX")"
started_at="$(ops_utc_now)"
ssh_remote_root=""

cleanup_lab_resources() {
  local status="succeeded"
  local ssh_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      status="failed"
    fi
    if [[ -n "${ssh_remote_root}" && -n "${TORQUE_LAB_SSH:-}" ]]; then
      ops_set_ssh_base_args
      if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "rm -rf '${ssh_remote_root}' && test ! -e '${ssh_remote_root}'"; then
        ssh_status="deleted"
      else
        ssh_status="failed"
        status="failed"
      fi
    fi
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    labProfiles="lab.scale-sim,lab.ssh-linux" \
    scratchRoot="${scratch_root}" \
    ssh="${ssh_status}" \
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

ops_log "simulate distributed locks for ${target_count} targets"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${conflicts}" \
  "${max_bundle_bytes}" \
  "${skip_ssh_canary}" <<'PY'
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
conflicts = int(sys.argv[7])
max_bundle_bytes = int(sys.argv[8])
skip_ssh_canary = sys.argv[9] == "1"

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
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


def object_scope(index: int) -> str:
    return f"{target_id(index)}:/etc/torque-scale-lock-{index % 17}.conf"


locks: dict[str, str] = {}
events: list[dict] = []
blocked = 0
allowed = 0
duplicate_writes = 0
mutation_owner: dict[str, str] = {}
worker_conflicts = {f"worker-{i:02d}": {"allowed": 0, "blocked": 0} for i in range(worker_count)}
conflict_count = min(conflicts, target_count)
conflict_indices = {
    min(target_count - 1, max(0, math.floor((index + 1) * target_count / (conflict_count + 1))))
    for index in range(conflict_count)
}


def try_lock(scope: str, worker: str, holder_hint=None) -> bool:
    global blocked, allowed
    if scope in locks:
        blocked += 1
        worker_conflicts[worker]["blocked"] += 1
        events.append(
            {
                "event": "lock-blocked",
                "scope": scope,
                "workerId": worker,
                "holder": locks[scope],
                "holderHint": holder_hint or "",
            }
        )
        return False
    locks[scope] = worker
    allowed += 1
    worker_conflicts[worker]["allowed"] += 1
    events.append({"event": "lock-acquired", "scope": scope, "workerId": worker})
    return True


def unlock(scope: str, worker: str) -> None:
    if locks.get(scope) == worker:
        del locks[scope]
        events.append({"event": "lock-released", "scope": scope, "workerId": worker})


shard_summaries = []
for shard in range(shard_count):
    start_index = shard * shard_size
    end_index = min(start_index + shard_size, target_count)
    shard_allowed = 0
    shard_blocked = 0
    shard_duplicates = 0
    for index in range(start_index, end_index):
        scope = object_scope(index)
        worker = f"worker-{shard % worker_count:02d}"
        if try_lock(scope, worker):
            mutation_id = f"{worker}:{scope}"
            if scope in mutation_owner:
                duplicate_writes += 1
                shard_duplicates += 1
            mutation_owner[scope] = mutation_id
            shard_allowed += 1
            if index in conflict_indices:
                other_worker = f"worker-{(shard + 1) % worker_count:02d}"
                if not try_lock(scope, other_worker, holder_hint=worker):
                    shard_blocked += 1
            unlock(scope, worker)
    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleLockShard",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "targetRange": {"start": start_index, "endExclusive": end_index},
        "targetCount": end_index - start_index,
        "allowedWrites": shard_allowed,
        "blockedConflicts": shard_blocked,
        "duplicateWrites": shard_duplicates,
        "lockDigest": digest({"shard": shard, "allowed": shard_allowed, "blocked": shard_blocked}),
    }
    write_json(run_dir / "scale" / "shards" / f"shard-{shard:04d}.json", shard_doc)
    shard_summaries.append(shard_doc)

write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": "lab.scale-sim,lab.ssh-linux" if not skip_ssh_canary else "lab.scale-sim",
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "conflicts": conflicts,
    },
)
write_json(
    run_dir / "target-snapshot.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsLabTargetSnapshot",
        "taskId": task_id,
        "runId": run_id,
        "profiles": ["lab.scale-sim"] + ([] if skip_ssh_canary else ["lab.ssh-linux"]),
        "targets": [
            {
                "profile": "lab.scale-sim",
                "type": "synthetic-host-fleet",
                "transport": "simulated",
                "configured": True,
                "count": target_count,
            },
            {
                "profile": "lab.ssh-linux",
                "type": "linux-host-canary",
                "transport": "ssh",
                "configured": not skip_ssh_canary,
                "count": 0 if skip_ssh_canary else 1,
            },
        ],
        "targetCount": target_count,
        "shardCount": shard_count,
        "objectLockCount": len(mutation_owner),
        "conflictAttempts": conflicts,
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded",
        "decision": "allow",
        "taskId": task_id,
        "runId": run_id,
        "reason": "distributed-target-object-lock-proof",
        "labProfile": "lab.scale-sim,lab.ssh-linux" if not skip_ssh_canary else "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "scale" / "lock-events-sample.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleLockEventSample",
        "taskId": task_id,
        "runId": run_id,
        "eventCount": len(events),
        "sample": events[: min(250, len(events))],
    },
)
write_json(
    run_dir / "scale" / "summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleLockSummary",
        "taskId": task_id,
        "runId": run_id,
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "workerCount": worker_count,
        "objectLocks": len(mutation_owner),
        "allowedWrites": allowed,
        "blockedConflicts": blocked,
        "duplicateWrites": duplicate_writes,
        "expectedBlockedConflicts": len(conflict_indices),
        "conflictSampleIndices": sorted(conflict_indices)[:10],
        "remainingLocks": len(locks),
        "workerSummary": worker_conflicts,
        "shards": shard_summaries,
        "evidenceMode": "lock-summary-plus-shard-manifests",
        "bundleBudgetBytes": max_bundle_bytes,
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
        "targetsPerSecond": round(target_count / max(duration_ms / 1000, 0.001), 2),
    },
)
PY

ssh_canary_status="skipped"
ssh_canary_blocked="false"
if [[ "${skip_ssh_canary}" != "1" ]]; then
  ops_log "run real lab.ssh-linux lock canary"
  mkdir -p "${OPS_RUN_DIR}/ssh"
  ssh_remote_root="/tmp/torque-ops-scale-004-${OPS_RUN_ID}"
  ops_set_ssh_base_args
  ssh_target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  if ssh "${OPS_SSH_ARGS[@]}" "${ssh_target}" "RUN_ID='${OPS_RUN_ID}' REMOTE_ROOT='${ssh_remote_root}' bash -se" <<'REMOTE' | ops_redact_stdin "${OPS_RUN_DIR}/ssh/canary.redacted.json"
set -euo pipefail
rm -rf "$REMOTE_ROOT"
mkdir -p "$REMOTE_ROOT"
lock="$REMOTE_ROOT/object.lock"
protected="$REMOTE_ROOT/protected.txt"
events="$REMOTE_ROOT/events.jsonl"
writer1_status=unknown
writer2_status=unknown

(
  if mkdir "$lock" 2>/dev/null; then
    writer1_status=acquired
    printf '{"event":"lock-acquired","writer":"writer-1"}\n' >> "$events"
    printf 'writer-1-start %s\n' "$RUN_ID" >> "$protected"
    sleep 3
    printf 'writer-1-finish %s\n' "$RUN_ID" >> "$protected"
    rmdir "$lock"
    printf '{"event":"lock-released","writer":"writer-1"}\n' >> "$events"
  else
    writer1_status=blocked
    printf '{"event":"lock-blocked","writer":"writer-1"}\n' >> "$events"
  fi
  printf '%s\n' "$writer1_status" > "$REMOTE_ROOT/writer1.status"
) &
writer1_pid=$!

sleep 1
if mkdir "$lock" 2>/dev/null; then
  writer2_status=acquired
  printf '{"event":"lock-acquired","writer":"writer-2"}\n' >> "$events"
  printf 'writer-2-conflict %s\n' "$RUN_ID" >> "$protected"
  rmdir "$lock"
else
  writer2_status=blocked
  printf '{"event":"lock-blocked","writer":"writer-2"}\n' >> "$events"
fi
printf '%s\n' "$writer2_status" > "$REMOTE_ROOT/writer2.status"
wait "$writer1_pid"

writer1_status="$(cat "$REMOTE_ROOT/writer1.status")"
writer2_status="$(cat "$REMOTE_ROOT/writer2.status")"
writer2_lines="$(grep -c '^writer-2-' "$protected" 2>/dev/null || true)"
writer1_lines="$(grep -c '^writer-1-' "$protected" 2>/dev/null || true)"
blocked_ok=false
if [ "$writer1_status" = acquired ] && [ "$writer2_status" = blocked ] && [ "$writer2_lines" = 0 ] && [ "$writer1_lines" = 2 ]; then
  blocked_ok=true
fi
python3 - "$writer1_status" "$writer2_status" "$writer1_lines" "$writer2_lines" "$blocked_ok" "$REMOTE_ROOT" <<'PY'
import json
import sys

writer1, writer2, writer1_lines, writer2_lines, blocked_ok, remote_root = sys.argv[1:7]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleSSHLockCanary",
    "status": "succeeded" if blocked_ok == "true" else "failed",
    "remoteRoot": remote_root,
    "writer1": writer1,
    "writer2": writer2,
    "writer1Lines": int(writer1_lines),
    "writer2Lines": int(writer2_lines),
    "conflictingWriteBlocked": blocked_ok == "true",
}
print(json.dumps(doc, indent=2, sort_keys=True))
PY
test "$blocked_ok" = true
REMOTE
  then
    ssh_canary_status="succeeded"
    ssh_canary_blocked="true"
  else
    ssh_canary_status="failed"
    ssh_canary_blocked="false"
  fi
fi

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${ssh_canary_status}" \
  "${ssh_canary_blocked}" \
  "${skip_ssh_canary}" <<'PY'
import json
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
ssh_canary_status = sys.argv[4]
ssh_canary_blocked = sys.argv[5] == "true"
skip_ssh_canary = sys.argv[6] == "1"


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


summary = json.load((run_dir / "scale" / "summary.json").open())
errors = []
if summary["objectLocks"] != summary["targetCount"]:
    errors.append("object lock count mismatch")
if summary["allowedWrites"] != summary["targetCount"]:
    errors.append("allowed write count mismatch")
if summary["blockedConflicts"] != summary["expectedBlockedConflicts"]:
    errors.append("blocked conflict count mismatch")
if summary["duplicateWrites"] != 0:
    errors.append("duplicate writes detected")
if summary["remainingLocks"] != 0:
    errors.append("locks remained held after simulation")
if not skip_ssh_canary and (ssh_canary_status != "succeeded" or not ssh_canary_blocked):
    errors.append("ssh canary did not block conflicting write")

status = "succeeded" if not errors else "failed"
write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": status,
        "taskId": task_id,
        "runId": run_id,
        "labProfiles": ["lab.scale-sim"] + ([] if skip_ssh_canary else ["lab.ssh-linux"]),
        "targetCount": summary["targetCount"],
        "objectLocks": summary["objectLocks"],
        "allowedWrites": summary["allowedWrites"],
        "blockedConflicts": summary["blockedConflicts"],
        "duplicateWrites": summary["duplicateWrites"],
        "remainingLocks": summary["remainingLocks"],
        "sshCanaryStatus": ssh_canary_status,
        "sshCanaryBlocked": ssh_canary_blocked,
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
        "labProfiles": ["lab.scale-sim"] + ([] if skip_ssh_canary else ["lab.ssh-linux"]),
        "targetCount": summary["targetCount"],
        "objectLocks": summary["objectLocks"],
        "blockedConflicts": summary["blockedConflicts"],
        "duplicateWrites": summary["duplicateWrites"],
        "sshCanaryBlocked": ssh_canary_blocked,
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
