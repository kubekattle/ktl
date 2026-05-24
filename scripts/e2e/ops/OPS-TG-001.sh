#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-TG-001.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --skip-ssh-canary    Skip real lab.ssh-linux reachability. Debug only.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

OPS-TG-001 proves the TargetGraph schema and loader. It creates a lab
TargetGraph with targets, groups, transports, variables, facts, and privilege
profiles; runs the Go loader and package tests; and optionally proves a
lab.ssh-linux host target is reachable without storing the SSH target address
in evidence.

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

ops_init_run "OPS-TG-001"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-tg-001.XXXXXX")"
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

mkdir -p "${OPS_RUN_DIR}/targetgraph" "${OPS_RUN_DIR}/go" "${OPS_RUN_DIR}/ssh"
target_graph="${scratch_root}/targetgraph.yaml"
summary_json="${OPS_RUN_DIR}/targetgraph/load-summary.json"
go_test_json="${OPS_RUN_DIR}/go/targetgraph-test.jsonl"

cat >"${target_graph}" <<'YAML'
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ops-tg-001-lab
  labels:
    env: lab
variables:
  - id: environment
    values:
      region: local
      changeWindow: e2e
targets:
  - id: local/controller
    type: local
    labels:
      role: controller
      profile: lab.local
    privilegeProfile: local-readonly
    facts:
      ttl: 5m
    variables:
      - id: local
        values:
          workspace: scratch
  - id: host/lab-ssh
    type: host
    transportRef: ssh/lab
    labels:
      role: web
      profile: lab.ssh-linux
    groups:
      - group/web
    privilegeProfile: ssh-readonly
    facts:
      ttl: 15m
    variables:
      - id: host
        values:
          package: curl
groups:
  - id: group/web
    selector:
      role: web
transports:
  - id: local/controller
    kind: local
  - id: ssh/lab
    kind: ssh
    host: secret://ops/lab/ssh#host
    user: root
    keyRef: secret://ops/lab/ssh#identity
privilegeProfiles:
  - id: local-readonly
    kind: none
  - id: ssh-readonly
    kind: sudo
    commands:
      - /usr/bin/uname
      - /bin/mkdir
YAML

ops_log "run TargetGraph package tests"
if go test -json ./internal/ops/targetgraph >"${go_test_json}"; then
  package_test_status="succeeded"
else
  package_test_status="failed"
fi

ops_log "load generated TargetGraph fixture"
if TORQUE_OPS_TG_E2E_INPUT="${target_graph}" \
  TORQUE_OPS_TG_E2E_OUTPUT="${summary_json}" \
  go test ./internal/ops/targetgraph -run TestE2EEnvLoadTargetGraph -count=1 >>"${OPS_RUN_DIR}/go/e2e-loader.out" 2>&1; then
  loader_status="succeeded"
else
  loader_status="failed"
fi

ssh_canary_status="skipped"
ssh_reachable="false"
if [[ "${skip_ssh_canary}" != "1" ]]; then
  ops_log "run lab.ssh-linux TargetGraph reachability canary"
  ssh_remote_root="/tmp/torque-ops-tg-001-${OPS_RUN_ID}"
  ops_set_ssh_base_args
  ssh_target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  if ssh "${OPS_SSH_ARGS[@]}" "${ssh_target}" "RUN_ID='${OPS_RUN_ID}' REMOTE_ROOT='${ssh_remote_root}' bash -se" <<'REMOTE' | ops_redact_stdin "${OPS_RUN_DIR}/ssh/reachability.redacted.json"
set -euo pipefail
rm -rf "$REMOTE_ROOT"
mkdir -p "$REMOTE_ROOT"
uname_s="$(uname -s)"
printf '%s\n' "$RUN_ID" > "$REMOTE_ROOT/reachability.txt"
test -s "$REMOTE_ROOT/reachability.txt"
python3 - "$uname_s" "$REMOTE_ROOT" <<'PY'
import json
import sys
uname_s, remote_root = sys.argv[1:3]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetGraphSSHReachabilityCanary",
    "status": "succeeded",
    "targetId": "host/lab-ssh",
    "transportRef": "ssh/lab",
    "remoteRoot": remote_root,
    "uname": uname_s,
    "reachable": True,
}
print(json.dumps(doc, indent=2, sort_keys=True))
PY
REMOTE
  then
    ssh_canary_status="succeeded"
    ssh_reachable="true"
  else
    ssh_canary_status="failed"
  fi
fi

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${summary_json}" \
  "${package_test_status}" \
  "${loader_status}" \
  "${ssh_canary_status}" \
  "${ssh_reachable}" \
  "${skip_ssh_canary}" <<'PY'
import json
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
summary_path = Path(sys.argv[5])
package_test_status = sys.argv[6]
loader_status = sys.argv[7]
ssh_canary_status = sys.argv[8]
ssh_reachable = sys.argv[9] == "true"
skip_ssh_canary = sys.argv[10] == "1"


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


summary = {}
if summary_path.is_file():
    summary = json.load(summary_path.open(encoding="utf-8"))

lab_profiles = ["lab.local"] + ([] if skip_ssh_canary else ["lab.ssh-linux"])
errors = []
if package_test_status != "succeeded":
    errors.append("TargetGraph package tests failed")
if loader_status != "succeeded":
    errors.append("generated TargetGraph fixture failed to load")
if summary.get("targetCount") != 2:
    errors.append("expected two parsed targets")
if summary.get("groupCount") != 1:
    errors.append("expected one parsed group")
if summary.get("transportCount") != 2:
    errors.append("expected two parsed transports")
if summary.get("privilegeProfileCount") != 2:
    errors.append("expected two parsed privilege profiles")
if summary.get("secretReferenceCount") != 2:
    errors.append("expected two secret references")
if "host/lab-ssh" not in summary.get("hostReachabilityRefs", []):
    errors.append("host reachability ref missing from summary")
if not skip_ssh_canary and (ssh_canary_status != "succeeded" or not ssh_reachable):
    errors.append("ssh reachability canary failed")

status = "succeeded" if not errors else "failed"
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(lab_profiles),
        "graphName": summary.get("name", ""),
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
            {
                "profile": "lab.local",
                "type": "local",
                "targetId": "local/controller",
                "transport": "local/controller",
                "configured": True,
            },
            {
                "profile": "lab.ssh-linux",
                "type": "host",
                "targetId": "host/lab-ssh",
                "transport": "ssh/lab",
                "configured": not skip_ssh_canary,
            },
        ],
        "targetCount": summary.get("targetCount", 0),
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded" if not errors else "blocked",
        "decision": "allow" if not errors else "block",
        "taskId": task_id,
        "runId": run_id,
        "reason": "targetgraph-schema-loader-proof",
        "labProfiles": lab_profiles,
        "packageTests": package_test_status,
        "loader": loader_status,
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
        "graphName": summary.get("name", ""),
        "targetCount": summary.get("targetCount", 0),
        "groupCount": summary.get("groupCount", 0),
        "transportCount": summary.get("transportCount", 0),
        "privilegeProfileCount": summary.get("privilegeProfileCount", 0),
        "variableLayerCount": summary.get("variableLayerCount", 0),
        "secretReferenceCount": summary.get("secretReferenceCount", 0),
        "hostReachabilityRefs": summary.get("hostReachabilityRefs", []),
        "packageTests": package_test_status,
        "loader": loader_status,
        "sshCanaryStatus": ssh_canary_status,
        "sshReachable": ssh_reachable,
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
        "graphName": summary.get("name", ""),
        "targetCount": summary.get("targetCount", 0),
        "sshCanaryStatus": ssh_canary_status,
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
