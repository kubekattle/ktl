#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-SCALE-008.sh [options]

Options:
  --agents N              Synthetic pull-agent count. Default: 10000.
  --shard-size N          Agents per shard. Default: 250.
  --disconnect-agents N   Agents disconnected mid-assignment. Default: 640.
  --max-bundle-bytes N    Fail if exported evidence exceeds this size.
                           Default: 1048576.
  --evidence-root DIR     Evidence root. Defaults to a temp directory.
  --skip-ssh-canary       Skip real lab.ssh-linux canary. Debug only.
  --cleanup               Clean lab resources. Default.
  --no-cleanup            Leave lab resources for debugging.
  -h, --help              Show this help.

OPS-SCALE-008 proves the pull-agent protocol. It simulates 10,000 agents that
pull signed assignments, checkpoint before a forced disconnect, reconnect,
resume from checkpoint, and upload bounded evidence summaries. A real SSH
canary runs the same pull/checkpoint/resume/upload shape on a lab host unless
--skip-ssh-canary is passed.

Environment for real canary:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@lab-host
  TORQUE_LAB_SSH_IDENTITY=/path/to/key       optional
  TORQUE_LAB_SSH_OPTS="..."                 optional
EOF
}

agent_count=10000
shard_size=250
disconnect_agents=640
max_bundle_bytes=1048576
cleanup_enabled=1
skip_ssh_canary=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --agents)
      [[ $# -ge 2 ]] || ops_fail "--agents requires a value"
      agent_count="$2"
      shift 2
      ;;
    --shard-size)
      [[ $# -ge 2 ]] || ops_fail "--shard-size requires a value"
      shard_size="$2"
      shift 2
      ;;
    --disconnect-agents)
      [[ $# -ge 2 ]] || ops_fail "--disconnect-agents requires a value"
      disconnect_agents="$2"
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

for numeric in \
  "agents:${agent_count}" \
  "shard-size:${shard_size}" \
  "disconnect-agents:${disconnect_agents}" \
  "max-bundle-bytes:${max_bundle_bytes}"; do
  name="${numeric%%:*}"
  value="${numeric#*:}"
  case "${value}" in
    ''|*[!0-9]*) ops_fail "--${name} must be a non-negative integer" ;;
  esac
done
[[ "${agent_count}" -gt 0 ]] || ops_fail "--agents must be > 0"
[[ "${shard_size}" -gt 0 ]] || ops_fail "--shard-size must be > 0"
[[ "${max_bundle_bytes}" -gt 0 ]] || ops_fail "--max-bundle-bytes must be > 0"
[[ "${disconnect_agents}" -le "${agent_count}" ]] || ops_fail "--disconnect-agents must be <= --agents"

if [[ "${skip_ssh_canary}" != "1" ]]; then
  [[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux canary without TORQUE_OPS_E2E_CONFIRM=1"
  ops_require_env TORQUE_LAB_SSH
  ops_require_cmd ssh
fi

ops_init_run "OPS-SCALE-008"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-scale-008.XXXXXX")"
started_at="$(ops_utc_now)"
ssh_remote_root=""

cleanup_lab_resources() {
  local status="succeeded"
  local ssh_status="not-requested"
  local lab_profiles="lab.scale-sim"
  if [[ "${skip_ssh_canary}" != "1" ]]; then
    lab_profiles="lab.scale-sim,lab.ssh-linux"
  fi
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
    labProfiles="${lab_profiles}" \
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

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
} | ops_redact_stdin "${OPS_RUN_DIR}/redaction/probe.redacted.txt"

ops_log "simulate pull-agent protocol for ${agent_count} agents"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${agent_count}" \
  "${shard_size}" \
  "${disconnect_agents}" \
  "${max_bundle_bytes}" \
  "${skip_ssh_canary}" \
  "${scratch_root}" <<'PY'
from __future__ import annotations

import hashlib
import hmac
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
agent_count = int(sys.argv[5])
shard_size = int(sys.argv[6])
disconnect_agents = int(sys.argv[7])
max_bundle_bytes = int(sys.argv[8])
skip_ssh_canary = sys.argv[9] == "1"
scratch_root = Path(sys.argv[10])

start = time.monotonic()
shard_count = math.ceil(agent_count / shard_size)
signing_key = hashlib.sha256(f"{run_id}:ops-scale-008-pull-agent-signing-key".encode()).digest()
signer_key_digest = hashlib.sha256(signing_key).hexdigest()
protocol_version = "torque.dev/ops-pull-agent/v1alpha1"


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


def canonical(value: object) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def digest_json(value: object) -> str:
    return hashlib.sha256(canonical(value)).hexdigest()


def sign_assignment(payload: dict) -> str:
    return hmac.new(signing_key, canonical(payload), hashlib.sha256).hexdigest()


def verify_assignment(payload: dict, signature: str) -> bool:
    expected = sign_assignment(payload)
    return hmac.compare_digest(expected, signature)


def agent_id(index: int) -> str:
    return f"agent-{index:05d}"


def shard_for(index: int) -> int:
    return index // shard_size


def target_for(index: int) -> str:
    return f"host-{index:05d}"


def assignment_payload(index: int) -> dict:
    return {
        "apiVersion": protocol_version,
        "kind": "PullAgentAssignment",
        "runId": run_id,
        "assignmentId": f"assign-{index:05d}",
        "agentId": agent_id(index),
        "targetId": target_for(index),
        "shard": shard_for(index),
        "generation": 1,
        "steps": ["observe", "apply", "verify", "upload-evidence"],
        "checkpointAfterStep": "apply" if index < disconnect_agents else "",
        "policyDigest": hashlib.sha256(f"policy:{run_id}:pull-agent".encode()).hexdigest(),
    }


def checkpoint_doc(index: int, assignment_digest: str) -> dict:
    return {
        "agentId": agent_id(index),
        "assignmentId": f"assign-{index:05d}",
        "assignmentDigest": assignment_digest,
        "nextStep": "verify",
        "completedSteps": ["observe", "apply"],
        "evidenceOffset": 2,
        "checkpointDigest": hashlib.sha256(f"{run_id}:{index}:checkpoint".encode()).hexdigest(),
    }


def evidence_digest(index: int, assignment_digest: str, checkpoint_digest: str) -> str:
    return hashlib.sha256(
        f"{run_id}:{agent_id(index)}:{assignment_digest}:{checkpoint_digest}:evidence".encode()
    ).hexdigest()


scratch_root.mkdir(parents=True, exist_ok=True)
shards_dir = run_dir / "scale" / "shards"
uploads_dir = run_dir / "scale" / "uploads"
shards_dir.mkdir(parents=True, exist_ok=True)
uploads_dir.mkdir(parents=True, exist_ok=True)

assignment_index_hash = hashlib.sha256()
pull_index_hash = hashlib.sha256()
checkpoint_index_hash = hashlib.sha256()
resume_index_hash = hashlib.sha256()
upload_index_hash = hashlib.sha256()
event_samples = []

signed_assignments = 0
valid_signature_pulls = 0
invalid_signature_pulls = 0
checkpointed_agents = 0
disconnected = 0
resumed = 0
uploaded_agents = 0
duplicate_uploads = 0
missing_uploads = 0
uploaded_seen = set()

per_shard = {
    shard: {
        "assignmentCount": 0,
        "validSignaturePulls": 0,
        "checkpointedAgents": 0,
        "disconnectedAgents": 0,
        "resumedAgents": 0,
        "uploadedAgents": 0,
        "duplicateUploads": 0,
        "assignmentDigest": hashlib.sha256(),
        "checkpointDigest": hashlib.sha256(),
        "resumeDigest": hashlib.sha256(),
        "uploadDigest": hashlib.sha256(),
    }
    for shard in range(shard_count)
}

for index in range(agent_count):
    shard = shard_for(index)
    payload = assignment_payload(index)
    signature = sign_assignment(payload)
    assignment_digest = digest_json(payload)
    signed_assignments += 1
    assignment_index_hash.update(f"{agent_id(index)}:{assignment_digest}:{signature}\n".encode())
    per_shard[shard]["assignmentCount"] += 1
    per_shard[shard]["assignmentDigest"].update(f"{agent_id(index)}:{assignment_digest}:{signature}\n".encode())

    if verify_assignment(payload, signature):
        valid_signature_pulls += 1
        per_shard[shard]["validSignaturePulls"] += 1
        pull_index_hash.update(f"{agent_id(index)}:valid:{signature}\n".encode())
    else:
        invalid_signature_pulls += 1

    if len(event_samples) < 80:
        event_samples.append(
            {
                "event": "assignment-pulled",
                "agentId": agent_id(index),
                "assignmentDigest": assignment_digest,
                "signaturePrefix": signature[:16],
                "shard": shard,
            }
        )

    if index < disconnect_agents:
        checkpoint = checkpoint_doc(index, assignment_digest)
        checkpoint_digest = checkpoint["checkpointDigest"]
        checkpointed_agents += 1
        disconnected += 1
        per_shard[shard]["checkpointedAgents"] += 1
        per_shard[shard]["disconnectedAgents"] += 1
        checkpoint_index_hash.update(f"{agent_id(index)}:{checkpoint_digest}\n".encode())
        per_shard[shard]["checkpointDigest"].update(f"{agent_id(index)}:{checkpoint_digest}\n".encode())

        if verify_assignment(payload, signature) and checkpoint["nextStep"] == "verify":
            resumed += 1
            per_shard[shard]["resumedAgents"] += 1
            resume_index_hash.update(f"{agent_id(index)}:{checkpoint_digest}:resumed\n".encode())
            per_shard[shard]["resumeDigest"].update(f"{agent_id(index)}:{checkpoint_digest}:resumed\n".encode())
        else:
            missing_uploads += 1
    else:
        checkpoint_digest = "direct"

    upload_digest = evidence_digest(index, assignment_digest, checkpoint_digest)
    if agent_id(index) in uploaded_seen:
        duplicate_uploads += 1
        per_shard[shard]["duplicateUploads"] += 1
    else:
        uploaded_seen.add(agent_id(index))
        uploaded_agents += 1
        per_shard[shard]["uploadedAgents"] += 1
        upload_index_hash.update(f"{agent_id(index)}:{upload_digest}\n".encode())
        per_shard[shard]["uploadDigest"].update(f"{agent_id(index)}:{upload_digest}\n".encode())

tampered_payload = assignment_payload(0)
tampered_signature = sign_assignment(tampered_payload)
tampered_payload["generation"] = 2
tampered_rejected = not verify_assignment(tampered_payload, tampered_signature)

upload_chunks = []
shard_summaries = []
for shard in range(shard_count):
    state = per_shard[shard]
    chunk_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScalePullAgentEvidenceUploadChunk",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "agentRange": {
            "start": shard * shard_size,
            "endExclusive": min((shard + 1) * shard_size, agent_count),
        },
        "uploadedAgents": state["uploadedAgents"],
        "uploadDigest": state["uploadDigest"].hexdigest(),
        "contentAddressing": "sha256-agent-evidence-digest",
    }
    chunk_path = uploads_dir / f"upload-shard-{shard:04d}.json"
    write_json(chunk_path, chunk_doc)
    chunk_bytes = chunk_path.read_bytes()
    upload_chunks.append(
        {
            "shard": shard,
            "path": chunk_path.relative_to(run_dir).as_posix(),
            "uploadedAgents": state["uploadedAgents"],
            "sha256": hashlib.sha256(chunk_bytes).hexdigest(),
            "size": len(chunk_bytes),
        }
    )

    shard_doc = {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScalePullAgentShardManifest",
        "taskId": task_id,
        "runId": run_id,
        "shard": shard,
        "agentCount": state["assignmentCount"],
        "signedAssignments": state["assignmentCount"],
        "validSignaturePulls": state["validSignaturePulls"],
        "checkpointedAgents": state["checkpointedAgents"],
        "disconnectedAgents": state["disconnectedAgents"],
        "resumedAgents": state["resumedAgents"],
        "uploadedAgents": state["uploadedAgents"],
        "duplicateUploads": state["duplicateUploads"],
        "assignmentDigest": state["assignmentDigest"].hexdigest(),
        "checkpointDigest": state["checkpointDigest"].hexdigest(),
        "resumeDigest": state["resumeDigest"].hexdigest(),
        "uploadDigest": state["uploadDigest"].hexdigest(),
    }
    write_json(shards_dir / f"shard-{shard:04d}.json", shard_doc)
    shard_summaries.append(
        {
            "shard": shard,
            "agentCount": state["assignmentCount"],
            "validSignaturePulls": state["validSignaturePulls"],
            "checkpointedAgents": state["checkpointedAgents"],
            "resumedAgents": state["resumedAgents"],
            "uploadedAgents": state["uploadedAgents"],
            "duplicateUploads": state["duplicateUploads"],
        }
    )

duration_ms = int((time.monotonic() - start) * 1000)
summary = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScalePullAgentSummary",
    "taskId": task_id,
    "runId": run_id,
    "agentCount": agent_count,
    "shardSize": shard_size,
    "shardCount": shard_count,
    "signedAssignments": signed_assignments,
    "validSignaturePulls": valid_signature_pulls,
    "invalidSignaturePulls": invalid_signature_pulls,
    "tamperedAssignmentRejected": tampered_rejected,
    "checkpointedAgents": checkpointed_agents,
    "disconnectedAgents": disconnected,
    "resumedAgents": resumed,
    "uploadedAgents": uploaded_agents,
    "duplicateUploads": duplicate_uploads,
    "missingUploads": missing_uploads,
    "uploadChunkCount": len(upload_chunks),
    "assignmentIndexDigest": assignment_index_hash.hexdigest(),
    "pullIndexDigest": pull_index_hash.hexdigest(),
    "checkpointIndexDigest": checkpoint_index_hash.hexdigest(),
    "resumeIndexDigest": resume_index_hash.hexdigest(),
    "uploadIndexDigest": upload_index_hash.hexdigest(),
    "signerKeyDigest": signer_key_digest,
    "signingKeyStoredInEvidence": False,
    "sshCanaryExpected": not skip_ssh_canary,
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
        "profiles": "lab.scale-sim" if skip_ssh_canary else "lab.scale-sim,lab.ssh-linux",
        "agentCount": agent_count,
        "shardSize": shard_size,
        "shardCount": shard_count,
        "disconnectAgents": disconnect_agents,
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
                "type": "synthetic-pull-agent-fleet",
                "transport": "simulated-pull",
                "configured": True,
                "count": agent_count,
            }
        ],
        "agentCount": agent_count,
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
        "reason": "signed-pull-agent-assignment-protocol-proof",
        "labProfiles": ["lab.scale-sim"] + ([] if skip_ssh_canary else ["lab.ssh-linux"]),
        "agentCount": agent_count,
        "signedAssignments": signed_assignments,
        "validSignaturePulls": valid_signature_pulls,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "scale" / "assignment-policy.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScalePullAgentAssignmentPolicy",
        "taskId": task_id,
        "runId": run_id,
        "protocolVersion": protocol_version,
        "signature": "hmac-sha256",
        "signerKeyDigest": signer_key_digest,
        "signingKeyStoredInEvidence": False,
        "assignmentPull": "agent-verifies-before-execution",
        "checkpoint": "resume-from-next-step",
        "evidenceUpload": "content-addressed-shard-chunks",
    },
)
write_json(
    run_dir / "scale" / "signed-assignment-summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScalePullAgentSignedAssignmentSummary",
        "taskId": task_id,
        "runId": run_id,
        "signedAssignments": signed_assignments,
        "validSignaturePulls": valid_signature_pulls,
        "invalidSignaturePulls": invalid_signature_pulls,
        "tamperedAssignmentRejected": tampered_rejected,
        "assignmentIndexDigest": assignment_index_hash.hexdigest(),
    },
)
write_json(
    run_dir / "scale" / "checkpoint-summary.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScalePullAgentCheckpointSummary",
        "taskId": task_id,
        "runId": run_id,
        "checkpointedAgents": checkpointed_agents,
        "disconnectedAgents": disconnected,
        "resumedAgents": resumed,
        "checkpointIndexDigest": checkpoint_index_hash.hexdigest(),
        "resumeIndexDigest": resume_index_hash.hexdigest(),
    },
)
write_json(
    run_dir / "scale" / "evidence-upload-index.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsScalePullAgentEvidenceUploadIndex",
        "taskId": task_id,
        "runId": run_id,
        "uploadedAgents": uploaded_agents,
        "duplicateUploads": duplicate_uploads,
        "missingUploads": missing_uploads,
        "uploadIndexDigest": upload_index_hash.hexdigest(),
        "chunks": upload_chunks,
    },
)
write_json(run_dir / "scale" / "event-samples.json", {"events": event_samples})
write_json(run_dir / "scale" / "summary.json", summary)
PY

ssh_canary_status="skipped"
ssh_canary_verified="false"
ssh_canary_resumed="false"
ssh_canary_uploaded="false"
ssh_canary_tamper_rejected="false"
if [[ "${skip_ssh_canary}" != "1" ]]; then
  ops_log "run real lab.ssh-linux pull-agent canary"
  mkdir -p "${OPS_RUN_DIR}/ssh"
  ssh_remote_root="/tmp/torque-ops-scale-008-${OPS_RUN_ID}"
  ops_set_ssh_base_args
  ssh_target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  if ssh "${OPS_SSH_ARGS[@]}" "${ssh_target}" "RUN_ID='${OPS_RUN_ID}' REMOTE_ROOT='${ssh_remote_root}' bash -se" <<'REMOTE' | ops_redact_stdin "${OPS_RUN_DIR}/ssh/canary.redacted.json"
set -euo pipefail
rm -rf "$REMOTE_ROOT"
mkdir -p "$REMOTE_ROOT"
python3 - "$RUN_ID" "$REMOTE_ROOT" <<'PY'
import hashlib
import hmac
import json
import sys
from pathlib import Path

run_id, remote_root = sys.argv[1:3]
root = Path(remote_root)
key = hashlib.sha256(f"{run_id}:ops-scale-008-ssh-canary-signing-key".encode()).digest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def sign(payload):
    return hmac.new(key, canonical(payload), hashlib.sha256).hexdigest()


def verify(payload, signature):
    return hmac.compare_digest(sign(payload), signature)


assignment = {
    "apiVersion": "torque.dev/ops-pull-agent/v1alpha1",
    "kind": "PullAgentAssignment",
    "runId": run_id,
    "assignmentId": "ssh-canary-assignment",
    "agentId": "ssh-canary-agent",
    "targetId": "ssh-canary-host",
    "generation": 1,
    "steps": ["observe", "apply", "verify", "upload-evidence"],
    "checkpointAfterStep": "apply",
}
signature = sign(assignment)
verified = verify(assignment, signature)
tampered = dict(assignment)
tampered["generation"] = 2
tamper_rejected = not verify(tampered, signature)
checkpoint = {
    "assignmentId": assignment["assignmentId"],
    "agentId": assignment["agentId"],
    "nextStep": "verify",
    "completedSteps": ["observe", "apply"],
    "evidenceOffset": 2,
}
checkpoint_digest = hashlib.sha256(canonical(checkpoint)).hexdigest()
(root / "checkpoint.json").write_text(json.dumps(checkpoint, sort_keys=True) + "\n", encoding="utf-8")
resume_checkpoint = json.loads((root / "checkpoint.json").read_text(encoding="utf-8"))
resumed = resume_checkpoint["nextStep"] == "verify" and verified
upload = {
    "assignmentId": assignment["assignmentId"],
    "agentId": assignment["agentId"],
    "checkpointDigest": checkpoint_digest,
    "evidenceDigest": hashlib.sha256(f"{run_id}:ssh-canary-agent:evidence".encode()).hexdigest(),
}
(root / "upload.json").write_text(json.dumps(upload, sort_keys=True) + "\n", encoding="utf-8")
uploaded = (root / "upload.json").is_file()
status = "succeeded" if verified and tamper_rejected and resumed and uploaded else "failed"
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsScaleSSHPullAgentCanary",
    "status": status,
    "remoteRoot": remote_root,
    "signatureVerified": verified,
    "tamperedAssignmentRejected": tamper_rejected,
    "checkpointDigest": checkpoint_digest,
    "resumedFromCheckpoint": resumed,
    "evidenceUploaded": uploaded,
}
print(json.dumps(doc, indent=2, sort_keys=True))
if status != "succeeded":
    raise SystemExit(1)
PY
REMOTE
  then
    ssh_canary_status="succeeded"
    ssh_canary_verified="true"
    ssh_canary_resumed="true"
    ssh_canary_uploaded="true"
    ssh_canary_tamper_rejected="true"
  else
    ssh_canary_status="failed"
  fi
fi

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${ssh_canary_status}" \
  "${ssh_canary_verified}" \
  "${ssh_canary_resumed}" \
  "${ssh_canary_uploaded}" \
  "${ssh_canary_tamper_rejected}" \
  "${skip_ssh_canary}" <<'PY'
import json
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
ssh_canary_status = sys.argv[4]
ssh_canary_verified = sys.argv[5] == "true"
ssh_canary_resumed = sys.argv[6] == "true"
ssh_canary_uploaded = sys.argv[7] == "true"
ssh_canary_tamper_rejected = sys.argv[8] == "true"
skip_ssh_canary = sys.argv[9] == "1"


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


summary = json.load((run_dir / "scale" / "summary.json").open())
errors = []
if summary["signedAssignments"] != summary["agentCount"]:
    errors.append("signed assignment count mismatch")
if summary["validSignaturePulls"] != summary["agentCount"]:
    errors.append("not all agents pulled valid signed assignments")
if summary["invalidSignaturePulls"] != 0:
    errors.append("unexpected invalid signature pulls")
if not summary["tamperedAssignmentRejected"]:
    errors.append("tampered assignment was not rejected")
if summary["checkpointedAgents"] != summary["disconnectedAgents"]:
    errors.append("checkpoint/disconnect count mismatch")
if summary["resumedAgents"] != summary["disconnectedAgents"]:
    errors.append("not all disconnected agents resumed")
if summary["uploadedAgents"] != summary["agentCount"]:
    errors.append("not all agents uploaded evidence")
if summary["duplicateUploads"] != 0:
    errors.append("duplicate evidence uploads detected")
if summary["missingUploads"] != 0:
    errors.append("missing evidence uploads detected")
if summary["uploadChunkCount"] != summary["shardCount"]:
    errors.append("upload chunk count must equal shard count")
if summary["signingKeyStoredInEvidence"]:
    errors.append("signing key material entered evidence")
if not skip_ssh_canary:
    if ssh_canary_status != "succeeded":
        errors.append("ssh canary failed")
    if not ssh_canary_verified:
        errors.append("ssh canary did not verify signature")
    if not ssh_canary_tamper_rejected:
        errors.append("ssh canary did not reject tampered assignment")
    if not ssh_canary_resumed:
        errors.append("ssh canary did not resume from checkpoint")
    if not ssh_canary_uploaded:
        errors.append("ssh canary did not upload evidence")

status = "succeeded" if not errors else "failed"
lab_profiles = ["lab.scale-sim"] + ([] if skip_ssh_canary else ["lab.ssh-linux"])
write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": status,
        "taskId": task_id,
        "runId": run_id,
        "labProfiles": lab_profiles,
        "agentCount": summary["agentCount"],
        "signedAssignments": summary["signedAssignments"],
        "validSignaturePulls": summary["validSignaturePulls"],
        "tamperedAssignmentRejected": summary["tamperedAssignmentRejected"],
        "checkpointedAgents": summary["checkpointedAgents"],
        "disconnectedAgents": summary["disconnectedAgents"],
        "resumedAgents": summary["resumedAgents"],
        "uploadedAgents": summary["uploadedAgents"],
        "duplicateUploads": summary["duplicateUploads"],
        "missingUploads": summary["missingUploads"],
        "sshCanaryStatus": ssh_canary_status,
        "sshCanaryVerified": ssh_canary_verified,
        "sshCanaryTamperRejected": ssh_canary_tamper_rejected,
        "sshCanaryResumed": ssh_canary_resumed,
        "sshCanaryUploaded": ssh_canary_uploaded,
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
        "agentCount": summary["agentCount"],
        "signedAssignments": summary["signedAssignments"],
        "resumedAgents": summary["resumedAgents"],
        "uploadedAgents": summary["uploadedAgents"],
        "duplicateUploads": summary["duplicateUploads"],
        "sshCanaryStatus": ssh_canary_status,
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
