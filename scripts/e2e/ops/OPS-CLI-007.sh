#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-CLI-007.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Write cleanup receipt. Default.
  --no-cleanup           Keep local scratch for debugging.
  -h, --help             Show this help.

OPS-CLI-007 proves `torque ops adapter capabilities`. It verifies the local
adapter contract catalog, table and JSON output, local host.command.run probe,
and one read-only lab.ssh-linux SSH probe.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
EOF
}

cleanup_enabled=1

while [[ $# -gt 0 ]]; do
  case "$1" in
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux capability E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-CLI-007"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-cli-007.XXXXXX")"
local_status=""
ssh_status=""
cleanup_status="pending"

finish() {
  local code=$?
  trap - EXIT

  set +e
  python3 - \
    "${OPS_RUN_DIR}" \
    "${OPS_TASK_ID}" \
    "${OPS_RUN_ID}" \
    "${started_at}" \
    "${TORQUE_LAB_SSH}" \
    "${scratch_root}" \
    "${local_status}" \
    "${ssh_status}" \
    "${cleanup_status}" \
    "${code}" <<'PY'
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
    scratch_root,
    local_status,
    ssh_status,
    cleanup_status,
    exit_code,
) = sys.argv[1:11]
run = Path(run_dir)
code = int(exit_code)

def load(rel):
    path = run / rel
    if not path.is_file():
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}

def write(rel, doc):
    path = run / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

proof_path = run / "verification" / "ops-cli-007-proof.json"
proof = load("verification/ops-cli-007-proof.json")
errors = list(proof.get("errors") or [])
if not proof_path.is_file():
    errors.append("ops-cli-007 proof missing")
if local_status != "passed":
    errors.append(f"local status is {local_status or 'missing'}")
if ssh_status != "passed":
    errors.append(f"ssh status is {ssh_status or 'missing'}")
if cleanup_status not in {"succeeded", "skipped"}:
    errors.append(f"cleanup status is {cleanup_status or 'missing'}")

finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
status = "succeeded" if code == 0 and not errors else "failed"
write("metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["lab.local", "lab.ssh-linux"],
    "host": lab_ssh,
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "local/controller", "type": "local-host", "transport": "local"},
        {"id": "host/lab-ssh", "type": "host", "transport": "ssh", "address": lab_ssh},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "verify-adapter-capability-contracts",
    "status": "succeeded" if status == "succeeded" else "blocked",
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "localStatus": local_status,
    "sshStatus": ssh_status,
    "errors": errors,
    "verifiedAt": finished_at,
})
write("cleanup/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabCleanupReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": "succeeded" if cleanup_status in {"succeeded", "skipped"} else "failed",
    "mode": cleanup_status,
})
write("result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "finishedAt": finished_at,
})
if status != "succeeded":
    sys.exit(1)
PY
  local receipt_code=$?
  set -e
  if [[ ${receipt_code} -ne 0 ]]; then
    code=1
  fi

  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/local" "${OPS_RUN_DIR}/ssh" "${OPS_RUN_DIR}/verification" "${OPS_RUN_DIR}/cleanup"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "capture local adapter capability catalog"
(
  cd "${repo_root}"
  ./bin/torque ops adapter capabilities --format json
) >"${OPS_RUN_DIR}/local/capabilities.json" 2>"${OPS_RUN_DIR}/local/capabilities.stderr"
(
  cd "${repo_root}"
  ./bin/torque ops adapter capabilities
) >"${OPS_RUN_DIR}/local/capabilities.table" 2>"${OPS_RUN_DIR}/local/capabilities-table.stderr"

ops_log "probe local host.command.run adapter"
(
  cd "${repo_root}"
  ./bin/torque ops adapter capabilities host.command.run --target local://ops-cli-007 --format json --timeout 15s
) >"${OPS_RUN_DIR}/local/host-command-probe.json" 2>"${OPS_RUN_DIR}/local/host-command-probe.stderr"

ops_log "probe lab.ssh-linux host.command.run adapter"
(
  cd "${repo_root}"
  ./bin/torque ops adapter capabilities host.command.run --target "${TORQUE_LAB_SSH}" --format json --timeout 30s
) >"${OPS_RUN_DIR}/ssh/host-command-probe.json" 2>"${OPS_RUN_DIR}/ssh/host-command-probe.stderr"

ops_log "verify adapter capability evidence"
python3 - \
  "${OPS_RUN_DIR}/local/capabilities.json" \
  "${OPS_RUN_DIR}/local/capabilities.table" \
  "${OPS_RUN_DIR}/local/host-command-probe.json" \
  "${OPS_RUN_DIR}/ssh/host-command-probe.json" \
  "${TORQUE_LAB_SSH}" \
  "${OPS_RUN_DIR}/verification/ops-cli-007-proof.json" <<'PY'
import json
import sys
from pathlib import Path

catalog_path, table_path, local_probe_path, ssh_probe_path = map(Path, sys.argv[1:5])
lab_ssh = sys.argv[5]
out_path = Path(sys.argv[6])
errors = []

def load(path):
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        errors.append(f"{path.name} is not valid JSON: {exc}")
        return {}

def adapters(doc):
    return {item.get("adapter"): item for item in doc.get("adapters", []) if isinstance(item, dict)}

catalog = load(catalog_path)
catalog_adapters = adapters(catalog)
host = catalog_adapters.get("host.command.run")
render = catalog_adapters.get("host.file.render")
copy_adapter = catalog_adapters.get("host.file.copy")
package = catalog_adapters.get("host.package.install")
service = catalog_adapters.get("host.service.manage")
user = catalog_adapters.get("host.user.manage")
if catalog.get("apiVersion") != "torque.dev/ops/adapter-capabilities/v1":
    errors.append("catalog apiVersion mismatch")
if (catalog.get("summary") or {}).get("implemented", 0) < 1:
    errors.append("catalog missing implemented adapter")
if (catalog.get("summary") or {}).get("planned", 0) < 1:
    errors.append("catalog missing planned adapters")
if not host or host.get("status") != "implemented":
    errors.append("host.command.run not implemented in catalog")
else:
    for artifact in ("host-command-observe.json", "host-command-plan.json", "host-command-execute.json", "host-command-verify.json"):
        if artifact not in (host.get("evidenceArtifacts") or []):
            errors.append(f"host.command.run missing artifact {artifact}")
    for phase in ("observe", "plan", "apply", "verify", "export"):
        if phase not in (host.get("supportedPhases") or []):
            errors.append(f"host.command.run missing phase {phase}")
if not render or render.get("status") != "implemented" or render.get("diffQuality") != "exact":
    errors.append("host.file.render implemented contract missing")
elif "host-file-diff.json" not in (render.get("evidenceArtifacts") or []):
    errors.append("host.file.render missing diff artifact")
if not copy_adapter or copy_adapter.get("status") != "implemented" or copy_adapter.get("diffQuality") != "exact":
    errors.append("host.file.copy implemented contract missing")
elif "host-file-copy-diff.json" not in (copy_adapter.get("evidenceArtifacts") or []):
    errors.append("host.file.copy missing diff artifact")
if not package or package.get("status") != "implemented" or package.get("diffQuality") != "exact":
    errors.append("host.package.install implemented contract missing")
elif package.get("requiredPrivilege") != "root or delegated sudo":
    errors.append("host.package.install privilege contract missing")
elif "host-package-diff.json" not in (package.get("evidenceArtifacts") or []):
    errors.append("host.package.install missing diff artifact")
if not service or service.get("status") != "implemented" or service.get("diffQuality") != "exact":
    errors.append("host.service.manage implemented contract missing")
elif "host-service-diff.json" not in (service.get("evidenceArtifacts") or []):
    errors.append("host.service.manage missing diff artifact")
if not user or user.get("status") != "implemented" or user.get("diffQuality") != "exact":
    errors.append("host.user.manage implemented contract missing")
elif "host-user-diff.json" not in (user.get("evidenceArtifacts") or []):
    errors.append("host.user.manage missing diff artifact")

table = table_path.read_text(encoding="utf-8")
for text in ("ADAPTER", "STATUS", "host.command.run", "host.file.render", "host.file.copy", "host.package.install", "host.service.manage", "host.user.manage"):
    if text not in table:
        errors.append(f"table output missing {text}")

def verify_probe(path, transport):
    doc = load(path)
    items = doc.get("adapters") or []
    if len(items) != 1:
        errors.append(f"{path.name} adapter count is {len(items)}, want 1")
        return {}
    item = items[0]
    probe = item.get("probe") or {}
    if item.get("adapter") != "host.command.run":
        errors.append(f"{path.name} probed {item.get('adapter')}, want host.command.run")
    if probe.get("status") != "succeeded":
        errors.append(f"{path.name} probe status is {probe.get('status')}")
    if probe.get("transport") != transport:
        errors.append(f"{path.name} transport is {probe.get('transport')}, want {transport}")
    if not probe.get("targetDigest"):
        errors.append(f"{path.name} missing target digest")
    checks = {check.get("name"): check for check in probe.get("checks") or []}
    for name in ("connect", "shell", "redaction"):
        if (checks.get(name) or {}).get("status") != "succeeded":
            errors.append(f"{path.name} check {name} did not succeed")
    raw = path.read_text(encoding="utf-8")
    if "torque-adapter-probe-secret" in raw:
        errors.append(f"{path.name} leaked raw probe secret")
    if "password=[REDACTED]" not in raw:
        errors.append(f"{path.name} missing redacted password proof")
    return probe

local_probe = verify_probe(local_probe_path, "local")
ssh_probe = verify_probe(ssh_probe_path, "ssh")
if str(lab_ssh) in ssh_probe_path.read_text(encoding="utf-8"):
    errors.append("ssh probe output leaked raw target address")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsCLI007Proof",
    "status": "succeeded" if not errors else "failed",
    "errors": errors,
    "catalogTotal": (catalog.get("summary") or {}).get("total", 0),
    "implemented": (catalog.get("summary") or {}).get("implemented", 0),
    "planned": (catalog.get("summary") or {}).get("planned", 0),
    "localProbeStatus": local_probe.get("status"),
    "sshProbeStatus": ssh_probe.get("status"),
    "sshTargetDigest": ssh_probe.get("targetDigest"),
}
out_path.parent.mkdir(parents=True, exist_ok=True)
out_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
local_status="passed"
ssh_status="passed"

if [[ "${cleanup_enabled}" == "1" ]]; then
  cleanup_status="succeeded"
else
  cleanup_status="skipped"
fi
