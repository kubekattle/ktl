#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-006.sh [options]

Options:
  --targets N             Synthetic target count. Default: 10000.
  --shard-size N          Targets per shard. Default: 250.
  --payload-variants N    Unique evidence payload variants. Default: 512.
  --chunk-records N       Unique payload records per compressed chunk.
                           Default: 64.
  --min-dedupe-ratio N    Fail unless virtual raw evidence is at least this
                           many times larger than compressed chunks. Default: 8.
  --max-bundle-bytes N    Fail if exported evidence exceeds this size.
                           Default: 1048576.
  --evidence-root DIR     Evidence root. Defaults to a temp directory.
  --cleanup               Clean temporary scratch. Default.
  --no-cleanup            Leave temporary scratch for debugging.
  -h, --help              Show this help.

OPS-SCALE-006 proves evidence chunking, compression, dedupe, and summarization.
It simulates a 10,000-target run where full per-host payloads are large and
repetitive, writes only unique payloads into content-addressed gzip chunks, and
records shard manifests with digest references instead of copied host evidence.
EOF
}

target_count=10000
shard_size=250
payload_variants=512
chunk_records=64
min_dedupe_ratio=8
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
    --payload-variants)
      [[ $# -ge 2 ]] || ops_fail "--payload-variants requires a value"
      payload_variants="$2"
      shift 2
      ;;
    --chunk-records)
      [[ $# -ge 2 ]] || ops_fail "--chunk-records requires a value"
      chunk_records="$2"
      shift 2
      ;;
    --min-dedupe-ratio)
      [[ $# -ge 2 ]] || ops_fail "--min-dedupe-ratio requires a value"
      min_dedupe_ratio="$2"
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
case "${payload_variants}" in
  ''|*[!0-9]*) ops_fail "--payload-variants must be a positive integer" ;;
esac
case "${chunk_records}" in
  ''|*[!0-9]*) ops_fail "--chunk-records must be a positive integer" ;;
esac
case "${min_dedupe_ratio}" in
  ''|*[!0-9]*) ops_fail "--min-dedupe-ratio must be a positive integer" ;;
esac
case "${max_bundle_bytes}" in
  ''|*[!0-9]*) ops_fail "--max-bundle-bytes must be a positive integer" ;;
esac
[[ "${target_count}" -gt 0 ]] || ops_fail "--targets must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"
[[ "${payload_variants}" -gt 0 ]] || ops_fail "--payload-variants must be > 0"
[[ "${chunk_records}" -gt 0 ]] || ops_fail "--chunk-records must be > 0"
[[ "${min_dedupe_ratio}" -gt 0 ]] || ops_fail "--min-dedupe-ratio must be > 0"
[[ "${payload_variants}" -le "${target_count}" ]] || ops_fail "--payload-variants must be <= --targets"

ops_init_run "OPS-SCALE-006"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-006.XXXXXX")"
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

ops_log "prove chunked evidence export for ${target_count} targets"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${target_count}" \
  "${shard_size}" \
  "${payload_variants}" \
  "${chunk_records}" \
  "${min_dedupe_ratio}" \
  "${max_bundle_bytes}" \
  "${scratch_root}" <<'PY'
import gzip
import hashlib
import json
import math
import platform
import resource
import sys
import time
from collections import Counter
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
target_count = int(sys.argv[5])
shard_size = int(sys.argv[6])
payload_variants = int(sys.argv[7])
chunk_records = int(sys.argv[8])
min_dedupe_ratio = int(sys.argv[9])
max_bundle_bytes = int(sys.argv[10])
scratch_root = Path(sys.argv[11])

start = time.monotonic()
shard_count = math.ceil(target_count / shard_size)
role_names = ["web", "api", "worker", "db-client", "cache-client"]
region_names = ["us-east", "us-west", "eu-central", "ap-south"]
zone_names = ["a", "b", "c", "d"]


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


def digest_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def digest_json(value: object) -> str:
    return digest_bytes(json.dumps(value, sort_keys=True, separators=(",", ":")).encode())


def target_id(index: int) -> str:
    return f"host-{index:05d}"


def payload_variant(index: int) -> int:
    return index % payload_variants


def payload_doc(variant: int) -> dict:
    role = role_names[variant % len(role_names)]
    region = region_names[variant % len(region_names)]
    zone = zone_names[variant % len(zone_names)]
    service_lines = [
        f"{role}:{region}:{zone}:unit-{line % 19}:state=ready:epoch={variant % 37}:line={line:03d}"
        for line in range(80)
    ]
    packages = [
        {
            "name": f"pkg-{pkg % 31:02d}",
            "version": f"{1 + (variant % 9)}.{pkg % 17}.{variant % 23}",
            "wanted": True,
        }
        for pkg in range(36)
    ]
    return {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "SyntheticHostEvidence",
        "variant": variant,
        "role": role,
        "region": region,
        "zone": zone,
        "kernel": f"5.15.{variant % 32}",
        "serviceLines": service_lines,
        "packages": packages,
        "assertions": {
            "sshReachable": True,
            "systemdReady": True,
            "desiredStateDigest": digest_json({"role": role, "variant": variant % 13}),
        },
    }


def payload_bytes(variant: int) -> bytes:
    return json.dumps(payload_doc(variant), sort_keys=True, separators=(",", ":")).encode()


scratch_root.mkdir(parents=True, exist_ok=True)
chunks_dir = run_dir / "scale" / "chunks"
shards_dir = run_dir / "scale" / "shards"
chunks_dir.mkdir(parents=True, exist_ok=True)
shards_dir.mkdir(parents=True, exist_ok=True)

payload_records = []
payload_by_variant = {}
payload_by_digest = {}
payload_ref_counts = Counter(payload_variant(index) for index in range(target_count))
role_rollup = Counter(role_names[payload_variant(index) % len(role_names)] for index in range(target_count))
region_rollup = Counter(region_names[payload_variant(index) % len(region_names)] for index in range(target_count))
virtual_raw_evidence_bytes = 0

for variant in range(payload_variants):
    raw = payload_bytes(variant)
    payload_digest = digest_bytes(raw)
    payload_by_variant[variant] = {
        "digest": payload_digest,
        "rawBytes": len(raw),
    }
    payload_by_digest[payload_digest] = variant

for index in range(target_count):
    variant = payload_variant(index)
    virtual_raw_evidence_bytes += payload_by_variant[variant]["rawBytes"]

chunk_entries = []
chunk_index_hash = hashlib.sha256()
chunked_uncompressed_bytes = 0
chunked_compressed_bytes = 0

for chunk_no, start_variant in enumerate(range(0, payload_variants, chunk_records)):
    end_variant = min(start_variant + chunk_records, payload_variants)
    chunk_path = chunks_dir / f"chunk-{chunk_no:04d}.ndjson.gz"
    record_digests = []
    with gzip.open(chunk_path, "wt", encoding="utf-8") as f:
        for variant in range(start_variant, end_variant):
            raw = payload_bytes(variant)
            payload_digest = payload_by_variant[variant]["digest"]
            record = {
                "digest": payload_digest,
                "variant": variant,
                "rawBytes": len(raw),
                "targetReferenceCount": payload_ref_counts[variant],
                "payload": payload_doc(variant),
            }
            line = json.dumps(record, sort_keys=True, separators=(",", ":"))
            f.write(line + "\n")
            encoded = (line + "\n").encode()
            chunked_uncompressed_bytes += len(encoded)
            record_digest = digest_bytes(encoded)
            record_digests.append(record_digest)
            chunk_index_hash.update(f"{payload_digest}:{chunk_no}:{variant}\n".encode())
    compressed_bytes = chunk_path.read_bytes()
    chunk_entry = {
        "chunk": chunk_no,
        "path": chunk_path.relative_to(run_dir).as_posix(),
        "firstVariant": start_variant,
        "lastVariant": end_variant - 1,
        "recordCount": end_variant - start_variant,
        "recordDigest": digest_json(record_digests),
        "compressedSha256": digest_bytes(compressed_bytes),
        "compressedBytes": len(compressed_bytes),
    }
    chunked_compressed_bytes += len(compressed_bytes)
    chunk_entries.append(chunk_entry)

payload_to_chunk = {}
for entry in chunk_entries:
    for variant in range(entry["firstVariant"], entry["lastVariant"] + 1):
        payload_to_chunk[payload_by_variant[variant]["digest"]] = {
            "chunk": entry["chunk"],
            "path": entry["path"],
            "variant": variant,
        }

target_reference_hash = hashlib.sha256()
payload_count_hash = hashlib.sha256()
for variant in range(payload_variants):
    payload_count_hash.update(
        f"{payload_by_variant[variant]['digest']}:{payload_ref_counts[variant]}\n".encode()
    )

resolved_references = 0
missing_references = 0
reconstruction_samples = []
shard_summaries = []

for shard in range(shard_count):
    start_index = shard * shard_size
    end_index = min(start_index + shard_size, target_count)
    shard_hash = hashlib.sha256()
    shard_payloads = Counter()
    shard_samples = []
    for index in range(start_index, end_index):
        tid = target_id(index)
        variant = payload_variant(index)
        payload_digest = payload_by_variant[variant]["digest"]
        target_reference_hash.update(f"{tid}:{payload_digest}\n".encode())
        shard_hash.update(f"{tid}:{payload_digest}\n".encode())
        shard_payloads[payload_digest] += 1
        if payload_digest in payload_to_chunk:
            resolved_references += 1
        else:
            missing_references += 1
        ref = payload_to_chunk.get(payload_digest, {})
        if len(shard_samples) < 5:
            shard_samples.append(
                {
                    "targetId": tid,
                    "payloadDigest": payload_digest,
                    "chunk": ref.get("chunk"),
                    "chunkPath": ref.get("path"),
                }
            )
        if len(reconstruction_samples) < 20:
            reconstruction_samples.append(
                {
                    "targetId": tid,
                    "payloadDigest": payload_digest,
                    "variant": variant,
                    "chunkPath": ref.get("path"),
                    "recordDigest": digest_bytes(payload_bytes(variant)),
                }
            )
    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleEvidenceShardManifest",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "targetRange": {"start": start_index, "endExclusive": end_index},
        "targetCount": end_index - start_index,
        "uniquePayloadReferences": len(shard_payloads),
        "payloadReferenceDigest": shard_hash.hexdigest(),
        "sampleReferences": shard_samples,
        "perHostPayloadFilesWritten": 0,
    }
    write_json(shards_dir / f"shard-{shard:04d}.json", shard_doc)
    shard_summaries.append(
        {
            "shard": shard,
            "targetCount": end_index - start_index,
            "uniquePayloadReferences": len(shard_payloads),
            "payloadReferenceDigest": shard_hash.hexdigest(),
        }
    )

payload_ref_entries = [
    {
        "payloadDigest": payload_by_variant[variant]["digest"],
        "variant": variant,
        "targetReferenceCount": payload_ref_counts[variant],
        "chunk": payload_to_chunk[payload_by_variant[variant]["digest"]]["chunk"],
        "chunkPath": payload_to_chunk[payload_by_variant[variant]["digest"]]["path"],
    }
    for variant in range(payload_variants)
]
duplicate_payload_references = target_count - payload_variants
dedupe_ratio_floor = virtual_raw_evidence_bytes // max(chunked_compressed_bytes, 1)

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
        "payloadVariants": payload_variants,
        "chunkRecords": chunk_records,
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
        "status": "succeeded",
        "decision": "allow",
        "taskId": task_id,
        "runId": run_id,
        "reason": "chunked-deduped-evidence-export-proof",
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "scale" / "chunk-index.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleEvidenceChunkIndex",
        "taskId": task_id,
        "runId": run_id,
        "chunkFormat": "gzip-ndjson",
        "addressing": "sha256-payload-digest",
        "chunkCount": len(chunk_entries),
        "chunkRecords": chunk_records,
        "uniquePayloadCount": payload_variants,
        "targetReferenceCount": target_count,
        "chunkIndexDigest": chunk_index_hash.hexdigest(),
        "targetReferenceDigest": target_reference_hash.hexdigest(),
        "payloadCountDigest": payload_count_hash.hexdigest(),
        "chunkedUncompressedBytes": chunked_uncompressed_bytes,
        "chunkedCompressedBytes": chunked_compressed_bytes,
        "chunks": chunk_entries,
    },
)
write_json(
    run_dir / "scale" / "payload-ref-counts.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleEvidencePayloadReferenceCounts",
        "taskId": task_id,
        "runId": run_id,
        "payloadCountDigest": payload_count_hash.hexdigest(),
        "payloads": payload_ref_entries,
    },
)
write_json(
    run_dir / "scale" / "reconstruction-samples.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleEvidenceReconstructionSamples",
        "taskId": task_id,
        "runId": run_id,
        "samples": reconstruction_samples,
    },
)
write_json(
    run_dir / "scale" / "summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScaleEvidenceChunkSummary",
        "taskId": task_id,
        "runId": run_id,
        "targetCount": target_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "shardManifestCount": shard_count,
        "uniquePayloadCount": payload_variants,
        "chunkCount": len(chunk_entries),
        "chunkedUncompressedBytes": chunked_uncompressed_bytes,
        "chunkedCompressedBytes": chunked_compressed_bytes,
        "virtualRawEvidenceBytes": virtual_raw_evidence_bytes,
        "dedupeRatioFloor": dedupe_ratio_floor,
        "minDedupeRatio": min_dedupe_ratio,
        "duplicatePayloadReferences": duplicate_payload_references,
        "resolvedTargetReferences": resolved_references,
        "missingTargetReferences": missing_references,
        "perHostPayloadFilesWritten": 0,
        "payloadReferenceDigest": target_reference_hash.hexdigest(),
        "roleRollup": dict(sorted(role_rollup.items())),
        "regionRollup": dict(sorted(region_rollup.items())),
        "bundleBudgetBytes": max_bundle_bytes,
        "evidenceMode": "chunked-compressed-deduped-artifacts-with-shard-manifests",
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
        "chunkCount": len(chunk_entries),
        "targetsPerSecond": round(target_count / max(duration_ms / 1000, 0.001), 2),
    },
)

errors = []
if len(chunk_entries) < 2:
    errors.append("chunking did not create multiple chunks")
if resolved_references != target_count:
    errors.append("not all target evidence references resolve to chunks")
if missing_references != 0:
    errors.append("target evidence references are missing from chunk index")
if duplicate_payload_references <= 0:
    errors.append("dedupe proof has no duplicate payload references")
if dedupe_ratio_floor < min_dedupe_ratio:
    errors.append("dedupe ratio below required minimum")
if chunked_compressed_bytes >= virtual_raw_evidence_bytes:
    errors.append("compressed chunks are not smaller than virtual raw evidence")
if shard_count != len(list(shards_dir.glob("shard-*.json"))):
    errors.append("shard manifest count mismatch")
if list((run_dir / "scale").glob("per-host-*")):
    errors.append("per-host payload files were written")

write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": "succeeded" if not errors else "failed",
        "taskId": task_id,
        "runId": run_id,
        "labProfile": "lab.scale-sim",
        "targetCount": target_count,
        "shardCount": shard_count,
        "chunkCount": len(chunk_entries),
        "uniquePayloadCount": payload_variants,
        "virtualRawEvidenceBytes": virtual_raw_evidence_bytes,
        "chunkedCompressedBytes": chunked_compressed_bytes,
        "dedupeRatioFloor": dedupe_ratio_floor,
        "duplicatePayloadReferences": duplicate_payload_references,
        "resolvedTargetReferences": resolved_references,
        "missingTargetReferences": missing_references,
        "perHostPayloadFilesWritten": 0,
        "portableArtifactGraph": True,
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
        "chunkCount": len(chunk_entries),
        "dedupeRatioFloor": dedupe_ratio_floor,
        "perHostPayloadFilesWritten": 0,
        "bundleBudgetBytes": max_bundle_bytes,
    },
)

if errors:
    raise SystemExit("; ".join(errors))
PY
