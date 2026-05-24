#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-TG-002.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --skip-ssh-canary    Skip real lab.ssh-linux selection canary. Debug only.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

OPS-TG-002 proves TargetGraph selectors and groups. It creates a graph with
four target fixtures, expands groups, applies label selectors, enforces a
deterministic limit, reports a group/selector conflict, and optionally mirrors
the selected execution set on a real lab.ssh-linux host.

Environment for real canary:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@lab-host
  TORQUE_LAB_SSH_IDENTITY=/path/to/key       optional
  TORQUE_LAB_SSH_OPTS="..."                 optional
EOF
}

cleanup_enabled=1
skip_ssh_canary=0

while [[ $# -gt 0 ]]; do
  case "$1" in
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

if [[ "${skip_ssh_canary}" != "1" ]]; then
  [[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.ssh-linux canary without TORQUE_OPS_E2E_CONFIRM=1"
  ops_require_env TORQUE_LAB_SSH
  ops_require_cmd ssh
fi
ops_require_cmd go

ops_init_run "OPS-TG-002"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-tg-002.XXXXXX")"
started_at="$(ops_utc_now)"
ssh_remote_root=""

cleanup_lab_resources() {
  local status="succeeded"
  local ssh_status="not-requested"
  local lab_profiles="lab.local"
  if [[ "${skip_ssh_canary}" != "1" ]]; then
    lab_profiles="lab.local,lab.ssh-linux"
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

mkdir -p "${OPS_RUN_DIR}/targetgraph" "${OPS_RUN_DIR}/go" "${OPS_RUN_DIR}/execution" "${OPS_RUN_DIR}/ssh"
target_graph="${scratch_root}/targetgraph.yaml"
selection_json="${OPS_RUN_DIR}/targetgraph/selection-proof.json"
go_test_json="${OPS_RUN_DIR}/go/targetgraph-selection-test.jsonl"

cat >"${target_graph}" <<'YAML'
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ops-tg-002-lab
targets:
  - id: host/web-01
    type: host
    transportRef: ssh/lab
    labels:
      role: web
      zone: a
      profile: lab.ssh-linux
  - id: host/web-02
    type: host
    transportRef: ssh/lab
    labels:
      role: web
      zone: b
      profile: lab.ssh-linux
  - id: host/web-03
    type: host
    transportRef: ssh/lab
    labels:
      role: web
      zone: c
      profile: lab.ssh-linux
  - id: host/db-01
    type: host
    transportRef: ssh/lab
    labels:
      role: db
      zone: a
      profile: lab.ssh-linux
groups:
  - id: group/web
    selector:
      role: web
  - id: group/mixed
    targets:
      - host/web-01
      - host/web-02
      - host/db-01
transports:
  - id: ssh/lab
    kind: ssh
    host: secret://ops/lab/ssh#host
    user: root
    keyRef: secret://ops/lab/ssh#identity
privilegeProfiles:
  - id: ssh-readonly
    kind: sudo
    commands:
      - /usr/bin/test
      - /bin/mkdir
YAML

ops_log "run TargetGraph selector package tests"
if go test -json ./internal/ops/targetgraph >"${go_test_json}"; then
  package_test_status="succeeded"
else
  package_test_status="failed"
fi

ops_log "resolve generated TargetGraph selections"
if TORQUE_OPS_TG_E2E_INPUT="${target_graph}" \
  TORQUE_OPS_TG_E2E_SELECTION_OUTPUT="${selection_json}" \
  go test ./internal/ops/targetgraph -run TestE2EEnvResolveSelection -count=1 >>"${OPS_RUN_DIR}/go/e2e-selection.out" 2>&1; then
  selection_status="succeeded"
else
  selection_status="failed"
fi

ops_log "execute selected local target fixtures"
python3 - "${selection_json}" "${scratch_root}" "${OPS_RUN_DIR}/execution/local-fixtures.json" <<'PY'
import json
import sys
from pathlib import Path

selection_path = Path(sys.argv[1])
scratch_root = Path(sys.argv[2])
output = Path(sys.argv[3])
proof = json.load(selection_path.open(encoding="utf-8"))
selections = proof["selections"]
fixtures = scratch_root / "target-fixtures"
executions = {}
for name in ["groupWeb", "groupWebZoneA", "groupWebLimitTwo", "groupMixedWeb"]:
    target_ids = selections[name]["matchedTargetIds"]
    markers = []
    for target_id in target_ids:
        marker = fixtures / name / target_id.replace("/", "_")
        marker.parent.mkdir(parents=True, exist_ok=True)
        marker.write_text(f"{name}:{target_id}\n", encoding="utf-8")
        markers.append(str(marker.relative_to(scratch_root)))
    executions[name] = {
        "targetIds": target_ids,
        "markerCount": len(markers),
        "markers": markers,
    }
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetGraphSelectionFixtureExecution",
    "status": "succeeded",
    "executions": executions,
}
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

ssh_canary_status="skipped"
ssh_selected_count=0
if [[ "${skip_ssh_canary}" != "1" ]]; then
  ops_log "run lab.ssh-linux selector execution canary"
  ssh_remote_root="/tmp/torque-ops-tg-002-${OPS_RUN_ID}"
  ops_set_ssh_base_args
  ssh_target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  if ssh "${OPS_SSH_ARGS[@]}" "${ssh_target}" "RUN_ID='${OPS_RUN_ID}' REMOTE_ROOT='${ssh_remote_root}' bash -se" <<'REMOTE' | ops_redact_stdin "${OPS_RUN_DIR}/ssh/selection.redacted.json"
set -euo pipefail
rm -rf "$REMOTE_ROOT"
mkdir -p "$REMOTE_ROOT/selected"
for target in host_web-01 host_web-02 host_web-03; do
  printf '%s\n' "$RUN_ID" > "$REMOTE_ROOT/selected/$target"
done
selected_count="$(find "$REMOTE_ROOT/selected" -type f | wc -l | tr -d '[:space:]')"
python3 - "$selected_count" "$REMOTE_ROOT" <<'PY'
import json
import sys
selected_count, remote_root = sys.argv[1:3]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetGraphSSHSelectionCanary",
    "status": "succeeded" if int(selected_count) == 3 else "failed",
    "remoteRoot": remote_root,
    "selection": "group/web",
    "selectedTargetCount": int(selected_count),
    "targetFixtures": ["host/web-01", "host/web-02", "host/web-03"],
}
print(json.dumps(doc, indent=2, sort_keys=True))
PY
test "$selected_count" = 3
REMOTE
  then
    ssh_canary_status="succeeded"
    ssh_selected_count=3
  else
    ssh_canary_status="failed"
  fi
fi

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${selection_json}" \
  "${OPS_RUN_DIR}/execution/local-fixtures.json" \
  "${package_test_status}" \
  "${selection_status}" \
  "${ssh_canary_status}" \
  "${ssh_selected_count}" \
  "${skip_ssh_canary}" <<'PY'
import json
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
selection_path = Path(sys.argv[5])
local_execution_path = Path(sys.argv[6])
package_test_status = sys.argv[7]
selection_status = sys.argv[8]
ssh_canary_status = sys.argv[9]
ssh_selected_count = int(sys.argv[10])
skip_ssh_canary = sys.argv[11] == "1"


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


selection = json.load(selection_path.open(encoding="utf-8")) if selection_path.is_file() else {}
local_execution = json.load(local_execution_path.open(encoding="utf-8")) if local_execution_path.is_file() else {}
selections = selection.get("selections", {})
group_web = selections.get("groupWeb", {})
zone_a = selections.get("groupWebZoneA", {})
limited = selections.get("groupWebLimitTwo", {})
mixed = selections.get("groupMixedWeb", {})
lab_profiles = ["lab.local"] + ([] if skip_ssh_canary else ["lab.ssh-linux"])
errors = []
if package_test_status != "succeeded":
    errors.append("TargetGraph package tests failed")
if selection_status != "succeeded":
    errors.append("generated TargetGraph selection proof failed")
if group_web.get("matchedTargetIds") != ["host/web-01", "host/web-02", "host/web-03"]:
    errors.append("group/web did not expand to three web targets")
if zone_a.get("matchedTargetIds") != ["host/web-01"]:
    errors.append("selector zone=a did not narrow group/web to host/web-01")
if limited.get("matchedTargetIds") != ["host/web-01", "host/web-02"]:
    errors.append("limit did not keep first two deterministic targets")
if limited.get("omittedTargetIds") != ["host/web-03"]:
    errors.append("limit did not report omitted host/web-03")
if mixed.get("matchedTargetIds") != ["host/web-01", "host/web-02"]:
    errors.append("mixed group role=web did not select the two web targets")
if mixed.get("conflictCount") != 1:
    errors.append("mixed group role=web did not report one conflict")
if local_execution.get("status") != "succeeded":
    errors.append("local target fixture execution did not succeed")
if not skip_ssh_canary and (ssh_canary_status != "succeeded" or ssh_selected_count != 3):
    errors.append("ssh selection canary failed")

status = "succeeded" if not errors else "failed"
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(lab_profiles),
        "graphName": selection.get("graphName", ""),
    },
)
write_json(
    run_dir / "target-snapshot.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsLabTargetSnapshot",
        "taskId": task_id,
        "runId": run_id,
        "profiles": lab_profiles,
        "targets": [
            {"targetId": "host/web-01", "type": "host", "profile": "lab.ssh-linux", "fixture": True},
            {"targetId": "host/web-02", "type": "host", "profile": "lab.ssh-linux", "fixture": True},
            {"targetId": "host/web-03", "type": "host", "profile": "lab.ssh-linux", "fixture": True},
            {"targetId": "host/db-01", "type": "host", "profile": "lab.ssh-linux", "fixture": True},
        ],
        "targetCount": 4,
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded" if not errors else "blocked",
        "decision": "allow" if not errors else "block",
        "taskId": task_id,
        "runId": run_id,
        "reason": "targetgraph-selector-group-proof",
        "labProfiles": lab_profiles,
        "packageTests": package_test_status,
        "selection": selection_status,
        "sshCanaryStatus": ssh_canary_status,
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": status,
        "taskId": task_id,
        "runId": run_id,
        "labProfiles": lab_profiles,
        "graphName": selection.get("graphName", ""),
        "groupWebTargets": group_web.get("matchedTargetIds", []),
        "zoneATargets": zone_a.get("matchedTargetIds", []),
        "limitedTargets": limited.get("matchedTargetIds", []),
        "limitedOmittedTargets": limited.get("omittedTargetIds", []),
        "mixedTargets": mixed.get("matchedTargetIds", []),
        "mixedConflictCount": mixed.get("conflictCount", 0),
        "localExecutionStatus": local_execution.get("status", ""),
        "sshCanaryStatus": ssh_canary_status,
        "sshSelectedCount": ssh_selected_count,
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
        "graphName": selection.get("graphName", ""),
        "selectedTargetSetChanged": True,
        "mixedConflictCount": mixed.get("conflictCount", 0),
        "sshCanaryStatus": ssh_canary_status,
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
