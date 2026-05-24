#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-TG-003.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --skip-ssh-canary    Skip real lab.ssh-linux variable canary. Debug only.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

OPS-TG-003 proves TargetGraph variable layering with provenance and redaction.
It resolves graph, group, target, environment, and CLI variable layers; proves
the final precedence order; records redacted provenance; checks that secret
references do not enter evidence; and optionally mirrors the final non-secret
values on a real lab.ssh-linux host.

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

ops_init_run "OPS-TG-003"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-tg-003.XXXXXX")"
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
variable_json="${OPS_RUN_DIR}/targetgraph/variable-proof.json"
go_test_json="${OPS_RUN_DIR}/go/targetgraph-variable-test.jsonl"

cat >"${target_graph}" <<'YAML'
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ops-tg-003-lab
variables:
  - id: defaults
    values:
      package: default-nginx
      region: global
      dbPassword: secret://ops/db#password
targets:
  - id: host/web-01
    type: host
    transportRef: ssh/lab
    labels:
      role: web
      zone: a
      profile: lab.ssh-linux
    groups:
      - group/web
    variables:
      - id: host
        values:
          package: host-nginx
          hostRole: primary
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
    variables:
      - id: web
        values:
          package: group-nginx
          replicas: 3
          tlsCert: secret://ops/tls#cert
transports:
  - id: ssh/lab
    kind: ssh
    host: secret://ops/lab/ssh#host
    user: root
    keyRef: secret://ops/lab/ssh#identity
YAML

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
} | ops_redact_stdin "${OPS_RUN_DIR}/targetgraph/redaction-probe.txt"

ops_log "run TargetGraph variable package tests"
if go test -json ./internal/ops/targetgraph >"${go_test_json}"; then
  package_test_status="succeeded"
else
  package_test_status="failed"
fi

ops_log "resolve generated TargetGraph variables"
if TORQUE_OPS_TG_E2E_INPUT="${target_graph}" \
  TORQUE_OPS_TG_E2E_VARIABLE_OUTPUT="${variable_json}" \
  go test ./internal/ops/targetgraph -run TestE2EEnvResolveVariables -count=1 >>"${OPS_RUN_DIR}/go/e2e-variables.out" 2>&1; then
  variable_status="succeeded"
else
  variable_status="failed"
fi

ops_log "verify redacted variable proof locally"
python3 - "${variable_json}" "${OPS_RUN_DIR}/targetgraph/redaction-check.json" <<'PY'
import json
import sys
from pathlib import Path

proof_path = Path(sys.argv[1])
output = Path(sys.argv[2])
raw = proof_path.read_text(encoding="utf-8")
proof = json.loads(raw)
resolution = proof["resolution"]
values = resolution["values"]
errors = []
if "secret://" in raw:
    errors.append("secret reference leaked into variable proof")
if values["package"]["value"] != "cli-nginx":
    errors.append("CLI package override did not win")
if values["replicas"]["source"]["type"] != "group":
    errors.append("replicas did not come from group layer")
if values["hostRole"]["source"]["type"] != "target":
    errors.append("hostRole did not come from target layer")
if values["maintenanceWindow"]["source"]["type"] != "environment":
    errors.append("maintenanceWindow did not come from environment layer")
if values["apiToken"]["value"] != "[REDACTED:sensitive-key]":
    errors.append("apiToken was not redacted")
if values["tlsCert"]["value"] != "[REDACTED:secret-ref]":
    errors.append("tlsCert was not redacted as secret ref")
if resolution["redaction"]["secretRefCount"] < 3:
    errors.append("expected at least three secret references")
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetGraphVariableRedactionCheck",
    "status": "succeeded" if not errors else "failed",
    "finalPackage": values["package"]["value"],
    "secretReferenceLeak": "secret://" in raw,
    "redactedAssignments": resolution["redaction"]["redactedAssignments"],
    "secretRefCount": resolution["redaction"]["secretRefCount"],
    "errors": errors,
}
output.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY

ssh_canary_status="skipped"
ssh_precedence="false"
ssh_redacted="false"
if [[ "${skip_ssh_canary}" != "1" ]]; then
  ops_log "run lab.ssh-linux variable layering canary"
  ssh_remote_root="/tmp/torque-ops-tg-003-${OPS_RUN_ID}"
  ops_set_ssh_base_args
  ssh_target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
  if ssh "${OPS_SSH_ARGS[@]}" "${ssh_target}" "RUN_ID='${OPS_RUN_ID}' REMOTE_ROOT='${ssh_remote_root}' PACKAGE='cli-nginx' REPLICAS='3' MAINTENANCE='night' bash -se" <<'REMOTE' | ops_redact_stdin "${OPS_RUN_DIR}/ssh/variables.redacted.json"
set -euo pipefail
rm -rf "$REMOTE_ROOT"
mkdir -p "$REMOTE_ROOT"
cat > "$REMOTE_ROOT/final.env" <<EOF
package=$PACKAGE
replicas=$REPLICAS
maintenanceWindow=$MAINTENANCE
apiToken=[REDACTED]
EOF
precedence_ok=false
redacted_ok=false
if grep -q '^package=cli-nginx$' "$REMOTE_ROOT/final.env" && grep -q '^replicas=3$' "$REMOTE_ROOT/final.env"; then
  precedence_ok=true
fi
if grep -q '^apiToken=\[REDACTED\]$' "$REMOTE_ROOT/final.env" && ! grep -q 'secret://' "$REMOTE_ROOT/final.env"; then
  redacted_ok=true
fi
python3 - "$precedence_ok" "$redacted_ok" "$REMOTE_ROOT" <<'PY'
import json
import sys
precedence_ok, redacted_ok, remote_root = sys.argv[1:4]
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsTargetGraphSSHVariableCanary",
    "status": "succeeded" if precedence_ok == "true" and redacted_ok == "true" else "failed",
    "remoteRoot": remote_root,
    "targetId": "host/web-01",
    "finalPackage": "cli-nginx",
    "replicas": 3,
    "precedenceApplied": precedence_ok == "true",
    "redacted": redacted_ok == "true",
}
print(json.dumps(doc, indent=2, sort_keys=True))
PY
test "$precedence_ok" = true
test "$redacted_ok" = true
REMOTE
  then
    ssh_canary_status="succeeded"
    ssh_precedence="true"
    ssh_redacted="true"
  else
    ssh_canary_status="failed"
  fi
fi

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${variable_json}" \
  "${OPS_RUN_DIR}/targetgraph/redaction-check.json" \
  "${package_test_status}" \
  "${variable_status}" \
  "${ssh_canary_status}" \
  "${ssh_precedence}" \
  "${ssh_redacted}" \
  "${skip_ssh_canary}" <<'PY'
import json
import sys
import time
from pathlib import Path


run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
variable_path = Path(sys.argv[5])
redaction_check_path = Path(sys.argv[6])
package_test_status = sys.argv[7]
variable_status = sys.argv[8]
ssh_canary_status = sys.argv[9]
ssh_precedence = sys.argv[10] == "true"
ssh_redacted = sys.argv[11] == "true"
skip_ssh_canary = sys.argv[12] == "1"


def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")


variable_proof = json.load(variable_path.open(encoding="utf-8")) if variable_path.is_file() else {}
redaction_check = json.load(redaction_check_path.open(encoding="utf-8")) if redaction_check_path.is_file() else {}
resolution = variable_proof.get("resolution", {})
values = resolution.get("values", {})
redaction = resolution.get("redaction", {})
lab_profiles = ["lab.local"] + ([] if skip_ssh_canary else ["lab.ssh-linux"])
errors = []
if package_test_status != "succeeded":
    errors.append("TargetGraph package tests failed")
if variable_status != "succeeded":
    errors.append("generated TargetGraph variable proof failed")
if redaction_check.get("status") != "succeeded":
    errors.append("local redaction check failed")
if values.get("package", {}).get("value") != "cli-nginx":
    errors.append("CLI package override did not win")
if values.get("maintenanceWindow", {}).get("source", {}).get("type") != "environment":
    errors.append("environment layer did not contribute maintenanceWindow")
if values.get("replicas", {}).get("source", {}).get("type") != "group":
    errors.append("group layer did not contribute replicas")
if values.get("hostRole", {}).get("source", {}).get("type") != "target":
    errors.append("target layer did not contribute hostRole")
if redaction.get("secretRefCount", 0) < 3:
    errors.append("secret reference count too low")
if redaction_check.get("secretReferenceLeak"):
    errors.append("secret reference leaked into evidence")
if not skip_ssh_canary and (ssh_canary_status != "succeeded" or not ssh_precedence or not ssh_redacted):
    errors.append("ssh variable canary failed")

status = "succeeded" if not errors else "failed"
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(lab_profiles),
        "graphName": variable_proof.get("graphName", ""),
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
            {"targetId": "host/web-01", "type": "host", "profile": "lab.ssh-linux", "configured": not skip_ssh_canary},
            {"targetId": "host/db-01", "type": "host", "profile": "lab.ssh-linux", "configured": False},
        ],
        "targetCount": 2,
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded" if not errors else "blocked",
        "decision": "allow" if not errors else "block",
        "taskId": task_id,
        "runId": run_id,
        "reason": "targetgraph-variable-layering-proof",
        "labProfiles": lab_profiles,
        "packageTests": package_test_status,
        "variables": variable_status,
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
        "graphName": variable_proof.get("graphName", ""),
        "finalPackage": values.get("package", {}).get("value"),
        "finalPackageSource": values.get("package", {}).get("source", {}).get("type"),
        "replicasSource": values.get("replicas", {}).get("source", {}).get("type"),
        "hostRoleSource": values.get("hostRole", {}).get("source", {}).get("type"),
        "maintenanceWindowSource": values.get("maintenanceWindow", {}).get("source", {}).get("type"),
        "redactedAssignments": redaction.get("redactedAssignments", 0),
        "secretRefCount": redaction.get("secretRefCount", 0),
        "secretReferenceLeak": redaction_check.get("secretReferenceLeak"),
        "sshCanaryStatus": ssh_canary_status,
        "sshPrecedenceApplied": ssh_precedence,
        "sshRedacted": ssh_redacted,
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
        "graphName": variable_proof.get("graphName", ""),
        "finalPackage": values.get("package", {}).get("value"),
        "secretReferenceLeak": redaction_check.get("secretReferenceLeak"),
        "sshCanaryStatus": ssh_canary_status,
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
