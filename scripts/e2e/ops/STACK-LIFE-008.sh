#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-LIFE-008.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --final-cleanup      Delete the recreated lab after proof is collected.
  --no-final-cleanup   Leave the recreated lab running. Default.
  -h, --help           Show this help.

STACK-LIFE-008 proves the Kubernetes lifecycle hardening loop on the real
GitLab Firecracker hybrid stack:

  destructive delete -> create/apply -> inspect -> derive targets -> cert renew
  -> verify -> rerun idempotent -> delete -> recreate/apply -> proof export

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

final_cleanup=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-root)
      [[ $# -ge 2 ]] || ops_fail "--evidence-root requires a value"
      OPS_EVIDENCE_ROOT="$2"
      shift 2
      ;;
    --final-cleanup)
      final_cleanup=1
      shift
      ;;
    --no-final-cleanup)
      final_cleanup=0
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing destructive lifecycle lab without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd curl
ops_require_cmd jq
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd sqlite3
ops_require_cmd ssh
ops_require_cmd tar

repo_root="$(ops_repo_root)"
ops_init_run "STACK-LIFE-008"
started_at="$(ops_utc_now)"
stack_config="${repo_root}/testdata/stack/e2e/19-firecracker-gitlab-hybrid/stack.yaml"
torque_bin="${repo_root}/bin/torque"
remote_root="/var/lib/torque-firecracker-gitlab/hybrid"
gitlab_host="gitlab.141.105.65.227.nip.io"
public_url="http://141.105.65.227/users/sign_in"

initial_delete_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-delete-initial"
create_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-create"
rerun_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-rerun"
delete_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-delete"
recreate_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-recreate"
final_cleanup_run_id="${OPS_TASK_ID}-${OPS_RUN_ID}-final-cleanup"

mkdir -p \
  "${OPS_RUN_DIR}/stack" \
  "${OPS_RUN_DIR}/verification" \
  "${OPS_RUN_DIR}/remote" \
  "${OPS_RUN_DIR}/cleanup" \
  "${OPS_RUN_DIR}/redaction"

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
} | ops_redact_stdin "${OPS_RUN_DIR}/redaction/probe.redacted.txt"

run_stack() {
  local phase="$1"
  local command="$2"
  local run_id="$3"
  ops_log "stack ${command}: ${phase} (${run_id})"
  "${torque_bin}" stack "${command}" \
    --config "${stack_config}" \
    --run-id "${run_id}" \
    --yes \
    >"${OPS_RUN_DIR}/stack/${phase}.jsonl" \
    2>"${OPS_RUN_DIR}/stack/${phase}.stderr"
}

audit_export_run() {
  local phase="$1"
  local run_id="$2"
  ops_log "audit/export: ${phase} (${run_id})"
  "${torque_bin}" stack audit \
    --config "${stack_config}" \
    --run-id "${run_id}" \
    --output json \
    --include-artifacts \
    >"${OPS_RUN_DIR}/stack/${phase}-audit.json"
  "${torque_bin}" stack export \
    --config "${stack_config}" \
    --run-id "${run_id}" \
    --out "${OPS_RUN_DIR}/stack/${phase}-export.tgz" \
    >"${OPS_RUN_DIR}/stack/${phase}-export.out"
}

extract_lifecycle_summary() {
  local phase="$1"
  local audit_path="${OPS_RUN_DIR}/stack/${phase}-audit.json"
  local out_path="${OPS_RUN_DIR}/verification/${phase}-lifecycle-summary.json"
  python3 - "${audit_path}" "${out_path}" "${phase}" <<'PY'
import json
import sys
from pathlib import Path

audit_path = Path(sys.argv[1])
out_path = Path(sys.argv[2])
phase = sys.argv[3]
audit = json.loads(audit_path.read_text(encoding="utf-8"))
summary_artifact = None
for artifact in audit.get("artifacts", []):
    if (
        artifact.get("nodeId") == "k8s.cluster.verify/gitlab-k8s-cluster-verify"
        and artifact.get("name") == "k8s-lifecycle-summary.json"
    ):
        summary_artifact = artifact
        break
if not summary_artifact:
    raise SystemExit(f"missing k8s-lifecycle-summary.json in {audit_path}")
summary = json.loads(summary_artifact.get("body") or "{}")
cert_renew = summary.get("certificateRenew") or {}
verify = summary.get("verify") or {}
inspect = summary.get("inspect") or {}
app_gate = summary.get("applicationGate") or {}
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLifecycleSummaryCheck",
    "phase": phase,
    "runId": audit.get("runId"),
    "status": summary.get("status"),
    "message": summary.get("message"),
    "summaryArtifact": {
        "nodeId": summary_artifact.get("nodeId"),
        "name": summary_artifact.get("name"),
        "sha256": summary_artifact.get("sha256"),
        "sizeBytes": summary_artifact.get("sizeBytes"),
    },
    "sourceArtifactCount": len(summary.get("sourceArtifacts") or []),
    "sourceArtifacts": [
        {
            "phase": item.get("phase"),
            "nodeId": item.get("nodeId"),
            "name": item.get("name"),
            "sha256": item.get("sha256"),
            "sizeBytes": item.get("sizeBytes"),
        }
        for item in summary.get("sourceArtifacts") or []
    ],
    "inspect": {
        "distribution": ((inspect.get("provider") or {}).get("distribution")),
        "topology": inspect.get("topology"),
    },
    "certificateRenew": {
        "status": cert_renew.get("status"),
        "message": cert_renew.get("message"),
        "derivedTargets": ((cert_renew.get("targetsFrom") or {}).get("derivedCount")),
        "targetsFromSourceDigest": ((cert_renew.get("targetsFrom") or {}).get("sourceArtifactDigest")),
        "targets": [
            {
                "id": target.get("id"),
                "role": target.get("role"),
                "checkpointStatus": target.get("checkpointStatus"),
                "checkpointPhase": target.get("checkpointPhase"),
                "skippedReason": target.get("skippedReason"),
            }
            for target in cert_renew.get("targets") or []
        ],
    },
    "applicationGate": {
        "status": app_gate.get("status"),
        "beforeCount": len(app_gate.get("beforeProbes") or []),
        "afterCount": len(app_gate.get("afterProbes") or []),
    },
    "verify": {
        "readyNodes": verify.get("readyNodes"),
        "totalNodes": verify.get("totalNodes"),
        "appProbes": [
            {
                "id": probe.get("id"),
                "matched": probe.get("matched"),
                "stdoutDigest": ((probe.get("receipt") or {}).get("stdoutDigest")),
                "stdoutBytes": ((probe.get("receipt") or {}).get("stdoutBytes")),
            }
            for probe in verify.get("appProbes") or []
        ],
    },
}
errors = []
if doc["status"] != "succeeded":
    errors.append(f"summary status is {doc['status']!r}")
if doc["sourceArtifactCount"] != 4:
    errors.append(f"expected 4 source artifacts, got {doc['sourceArtifactCount']}")
if doc["inspect"]["distribution"] != "k3s":
    errors.append(f"expected k3s distribution, got {doc['inspect']['distribution']!r}")
if doc["certificateRenew"]["derivedTargets"] != 3:
    errors.append(f"expected 3 derived targets, got {doc['certificateRenew']['derivedTargets']}")
if not doc["certificateRenew"]["targetsFromSourceDigest"]:
    errors.append("missing targetsFrom source artifact digest")
if doc["applicationGate"]["status"] != "passed":
    errors.append(f"application gate status is {doc['applicationGate']['status']!r}")
if doc["applicationGate"]["beforeCount"] < 1 or doc["applicationGate"]["afterCount"] < 1:
    errors.append(f"application gate probe counts invalid: {doc['applicationGate']}")
if doc["verify"]["readyNodes"] != 3 or doc["verify"]["totalNodes"] != 3:
    errors.append(f"expected 3/3 ready nodes, got {doc['verify']}")
if not doc["verify"]["appProbes"] or not doc["verify"]["appProbes"][0].get("matched"):
    errors.append("GitLab app probe did not match")
if errors:
    doc["status"] = "failed"
    doc["errors"] = errors
else:
    doc["status"] = "succeeded"
out_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
}

verify_export_contains_summary() {
  local phase="$1"
  local export_path="${OPS_RUN_DIR}/stack/${phase}-export.tgz"
  local out_path="${OPS_RUN_DIR}/verification/${phase}-export-summary-check.json"
  local tmpdir
  tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-life-export.XXXXXX")"
  tar -xzf "${export_path}" -C "${tmpdir}"
  local row
  row="$(sqlite3 "${tmpdir}/state.sqlite" "select node_id || '/' || artifact_name || '|' || sha256 || '|' || size_bytes from torque_stack_run_artifacts where artifact_name='k8s-lifecycle-summary.json';")"
  rm -rf "${tmpdir}"
  [[ -n "${row}" ]] || ops_fail "export ${phase} missing k8s-lifecycle-summary.json"
  python3 - "${out_path}" "${phase}" "${row}" <<'PY'
import json
import sys
path, phase, row = sys.argv[1:4]
name, sha, size = row.split("|", 2)
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLifecycleExportCheck",
    "phase": phase,
    "status": "succeeded",
    "artifact": name,
    "sha256": sha,
    "sizeBytes": int(size),
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
}

verify_remote_deleted() {
  local phase="$1"
  ops_set_ssh_base_args
  ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" \
    "set -euo pipefail
test ! -e '${remote_root}'
! ip link show tqfcgl >/dev/null 2>&1
! iptables -t nat -C PREROUTING -p tcp --dport 80 -j DNAT --to-destination '172.31.245.10:32080' >/dev/null 2>&1
" >"${OPS_RUN_DIR}/remote/${phase}-deleted.out" 2>"${OPS_RUN_DIR}/remote/${phase}-deleted.stderr"
  ops_write_json_object "${OPS_RUN_DIR}/verification/${phase}-delete-check.json" \
    status="succeeded" \
    phase="${phase}" \
    remoteRoot="${remote_root}" \
    bridge="tqfcgl"
}

probe_gitlab() {
  local phase="$1"
  local html="${OPS_RUN_DIR}/verification/${phase}-gitlab-sign-in.html"
  curl --connect-timeout 5 --max-time 30 -fsS -H "Host: ${gitlab_host}" "${public_url}" >"${html}"
  grep -qi 'GitLab' "${html}"
  python3 - "${html}" "${OPS_RUN_DIR}/verification/${phase}-app-probe.json" "${phase}" "${public_url}" <<'PY'
import hashlib
import json
import re
import sys
from pathlib import Path

html = Path(sys.argv[1]).read_text(encoding="utf-8", errors="replace")
title = ""
match = re.search(r"<title>([^<]+)", html, re.I)
if match:
    title = match.group(1).strip()
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLifecycleAppProbe",
    "phase": sys.argv[3],
    "status": "succeeded" if "gitlab" in html.lower() else "failed",
    "url": sys.argv[4],
    "title": title,
    "bodySHA256": hashlib.sha256(html.encode("utf-8")).hexdigest(),
    "bodyBytes": len(html.encode("utf-8")),
}
Path(sys.argv[2]).write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if doc["status"] != "succeeded":
    raise SystemExit("GitLab probe failed")
PY
}

write_standard_artifacts() {
  local cleanup_status="$1"
  local cleanup_performed="$2"
  python3 - \
    "${OPS_RUN_DIR}" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${started_at}" \
    "${TORQUE_LAB_SSH}" \
    "${remote_root}" \
    "${initial_delete_run_id}" \
    "${create_run_id}" \
    "${rerun_run_id}" \
    "${delete_run_id}" \
    "${recreate_run_id}" \
    "${final_cleanup_run_id}" \
    "${cleanup_status}" \
    "${cleanup_performed}" <<'PY'
import json
import sys
import time
from pathlib import Path

(
    run_dir,
    task_id,
    run_id,
    started_at,
    lab_ssh,
    remote_root,
    initial_delete_run_id,
    create_run_id,
    rerun_run_id,
    delete_run_id,
    recreate_run_id,
    final_cleanup_run_id,
    cleanup_status,
    cleanup_performed,
) = sys.argv[1:15]
run = Path(run_dir)

def load(rel: str) -> dict:
    path = run / rel
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}

create = load("verification/create-lifecycle-summary.json")
rerun = load("verification/rerun-lifecycle-summary.json")
recreate = load("verification/recreate-lifecycle-summary.json")
delete_check = load("verification/delete-delete-check.json")
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def write(rel: str, doc: dict) -> None:
    path = run / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "host": lab_ssh,
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "lab/firecracker-gitlab-host", "type": "ssh-host", "transport": "ssh", "address": lab_ssh},
        {"id": "cluster/gitlab-k3s", "type": "kubernetes", "distribution": "k3s", "nodeCount": 3},
        {"id": "app/gitlab", "type": "http", "url": "http://gitlab.141.105.65.227.nip.io/"},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-destructive-stack-lifecycle-hardening-lab",
    "status": "succeeded",
    "evidence": {
        "initialDeleteRunId": initial_delete_run_id,
        "createRunId": create_run_id,
        "rerunRunId": rerun_run_id,
        "deleteRunId": delete_run_id,
        "recreateRunId": recreate_run_id,
        "finalCleanupRunId": final_cleanup_run_id if cleanup_performed == "true" else "",
        "remoteRoot": remote_root,
        "createSummaryDigest": ((create.get("summaryArtifact") or {}).get("sha256")),
        "rerunSummaryDigest": ((rerun.get("summaryArtifact") or {}).get("sha256")),
        "recreateSummaryDigest": ((recreate.get("summaryArtifact") or {}).get("sha256")),
    },
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackLifecycleHardeningReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded",
    "sequence": [
        {"phase": "initial-delete", "command": "delete", "runId": initial_delete_run_id, "remoteDeleted": True},
        {"phase": "create", "command": "apply", "runId": create_run_id, "summary": "verification/create-lifecycle-summary.json"},
        {"phase": "rerun", "command": "apply", "runId": rerun_run_id, "summary": "verification/rerun-lifecycle-summary.json"},
        {"phase": "delete", "command": "delete", "runId": delete_run_id, "remoteDeleted": delete_check.get("status") == "succeeded"},
        {"phase": "recreate", "command": "apply", "runId": recreate_run_id, "summary": "verification/recreate-lifecycle-summary.json"},
    ],
    "checks": {
        "createReadyNodes": ((create.get("verify") or {}).get("readyNodes")),
        "rerunReadyNodes": ((rerun.get("verify") or {}).get("readyNodes")),
        "recreateReadyNodes": ((recreate.get("verify") or {}).get("readyNodes")),
        "rerunCertMessage": ((rerun.get("certificateRenew") or {}).get("message")),
        "recreateAppProbeMatched": bool((((recreate.get("verify") or {}).get("appProbes") or [{}])[0]).get("matched")),
    },
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": cleanup_status,
    "cleanupPerformed": cleanup_performed == "true",
    "finalState": "deleted" if cleanup_performed == "true" else "recreated-running",
    "remoteRoot": remote_root,
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded",
    "finishedAt": finished_at,
    "bundle": str(run.with_suffix(".tgz")),
})
PY
}

ops_log "build torque"
make -C "${repo_root}" build >"${OPS_RUN_DIR}/stack/build.out" 2>"${OPS_RUN_DIR}/stack/build.stderr"

run_stack "initial-delete" "delete" "${initial_delete_run_id}"
audit_export_run "initial-delete" "${initial_delete_run_id}"
verify_remote_deleted "initial"

run_stack "create" "apply" "${create_run_id}"
audit_export_run "create" "${create_run_id}"
extract_lifecycle_summary "create"
verify_export_contains_summary "create"
probe_gitlab "create"

run_stack "rerun" "apply" "${rerun_run_id}"
audit_export_run "rerun" "${rerun_run_id}"
extract_lifecycle_summary "rerun"
verify_export_contains_summary "rerun"
probe_gitlab "rerun"

run_stack "delete" "delete" "${delete_run_id}"
audit_export_run "delete" "${delete_run_id}"
verify_remote_deleted "delete"

run_stack "recreate" "apply" "${recreate_run_id}"
audit_export_run "recreate" "${recreate_run_id}"
extract_lifecycle_summary "recreate"
verify_export_contains_summary "recreate"
probe_gitlab "recreate"

cleanup_status="succeeded"
cleanup_performed="false"
if [[ "${final_cleanup}" == "1" ]]; then
  run_stack "final-cleanup" "delete" "${final_cleanup_run_id}"
  audit_export_run "final-cleanup" "${final_cleanup_run_id}"
  verify_remote_deleted "final-cleanup"
  cleanup_performed="true"
fi

write_standard_artifacts "${cleanup_status}" "${cleanup_performed}"
ops_scan_for_secret_material "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/redaction-report.json"
ops_write_manifest "${OPS_RUN_DIR}" "${OPS_RUN_DIR}/manifest.json"
ops_export_bundle "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}"
ops_validate_evidence_contract "${OPS_RUN_DIR}" "${OPS_BUNDLE_PATH}" >"${OPS_BUNDLE_PATH%.tgz}.contract.json"

bundle_size="$(wc -c <"${OPS_BUNDLE_PATH}" | tr -d '[:space:]')"
ops_log "evidence: ${OPS_RUN_DIR}"
ops_log "bundle: ${OPS_BUNDLE_PATH}"
ops_log "bundle bytes: ${bundle_size}"
