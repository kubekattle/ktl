#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-INV-002.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

OPS-INV-002 proves `torque ops inventory graph`. It exports JSON and HTML graph
views from a TargetGraph fixture, proves selector-highlighted targets and graph
edges, checks secret redaction, and records a real lab.ssh-linux reachability
canary for the GitLab-style lab inventory.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@lab-host
  TORQUE_LAB_SSH_IDENTITY=/path/to/key       optional
  TORQUE_LAB_SSH_OPTS="..."                 optional
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing inventory graph E2E without TORQUE_OPS_E2E_CONFIRM=1"
ops_require_env TORQUE_LAB_SSH
ops_require_cmd go
ops_require_cmd ssh

ops_init_run "OPS-INV-002"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-inv-002.XXXXXX")"
started_at="$(ops_utc_now)"

cleanup_lab_resources() {
  local status="succeeded"
  local local_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      status="failed"
      local_status="failed"
    else
      local_status="deleted"
    fi
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    labProfiles="lab.local,lab.ssh-linux" \
    scratchRoot="${scratch_root}" \
    local="${local_status}" \
    remote="not-required" \
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

mkdir -p "${OPS_RUN_DIR}/inventory" "${OPS_RUN_DIR}/go" "${OPS_RUN_DIR}/ssh" "${OPS_RUN_DIR}/verification"
targetgraph_yaml="${scratch_root}/targetgraph.yaml"
targetgraph_evidence_yaml="${OPS_RUN_DIR}/inventory/targetgraph.redacted.yaml"
graph_json="${OPS_RUN_DIR}/inventory/graph-db.json"
graph_html="${OPS_RUN_DIR}/inventory/graph-gitlab.html"
check_json="${OPS_RUN_DIR}/inventory/graph-check.json"
ssh_json="${OPS_RUN_DIR}/ssh/reachability.json"

{
  printf 'authorization: bearer %s\n' "${OPS_SECRET_CANARY}"
  printf 'token=%s\n' "${OPS_SECRET_CANARY}"
  printf 'secret://ops/inv-002#token\n'
} | ops_redact_stdin "${OPS_RUN_DIR}/inventory/redaction-probe.txt"

cat >"${targetgraph_yaml}" <<EOF
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: gitlab-hybrid-lab
targets:
  - id: host/gitlab-app-01
    type: host
    transportRef: ssh/gitlab-app-01
    labels:
      app: gitlab
      env: lab
      role: app
    facts:
      ttl: 15m
  - id: host/gitlab-db-01
    type: host
    transportRef: ssh/gitlab-db-01
    labels:
      app: gitlab
      env: lab
      role: db
    facts:
      ttl: 15m
  - id: host/gitlab-db-02
    type: host
    transportRef: ssh/gitlab-db-02
    labels:
      app: gitlab
      env: lab
      role: db
    facts:
      ttl: 15m
groups:
  - id: gitlab
    selector:
      app: gitlab
  - id: db
    selector:
      role: db
transports:
  - id: ssh/gitlab-app-01
    kind: ssh
    host: 141.105.65.227
    user: root
    keyRef: secret://ops/inv-002#ssh-key
  - id: ssh/gitlab-db-01
    kind: ssh
    host: 172.31.245.13
    user: root
  - id: ssh/gitlab-db-02
    kind: ssh
    host: 172.31.245.14
    user: root
variables:
  - id: global
    values:
      credential: secret://ops/inv-002#token
EOF
ops_redact_stdin "${targetgraph_evidence_yaml}" <"${targetgraph_yaml}"

ops_log "run inventory graph package and CLI tests"
if go test ./internal/ops/inventory ./cmd/torque -run 'Test(Show|Graph|ParseSelector|OpsInventory)' >"${OPS_RUN_DIR}/go/inventory-graph-tests.out" 2>&1; then
  package_test_status="succeeded"
else
  package_test_status="failed"
fi

ops_log "export JSON and HTML inventory graphs"
if go run ./cmd/torque ops inventory graph --targets "${targetgraph_yaml}" --selector role=db --format json >"${graph_json}" 2>"${OPS_RUN_DIR}/inventory/graph-json.err"; then
  json_status="succeeded"
else
  json_status="failed"
fi
if go run ./cmd/torque ops inventory graph --targets "${targetgraph_yaml}" --group gitlab --limit 1 --output "${graph_html}" >"${OPS_RUN_DIR}/inventory/graph-html.out" 2>"${OPS_RUN_DIR}/inventory/graph-html.err"; then
  html_status="succeeded"
else
  html_status="failed"
fi

ops_log "run lab.ssh-linux reachability canary"
ops_set_ssh_base_args
ssh_target="$(ops_ssh_target "${TORQUE_LAB_SSH}")"
if ssh "${OPS_SSH_ARGS[@]}" "${ssh_target}" "printf torque-inventory-graph-ok" >"${OPS_RUN_DIR}/ssh/reachability.out" 2>"${OPS_RUN_DIR}/ssh/reachability.err"; then
  ssh_status="succeeded"
else
  ssh_status="failed"
fi

python3 - "${ssh_target}" "${ssh_status}" "${ssh_json}" <<'PY'
import hashlib
import json
import sys
import time
target, status, output = sys.argv[1:4]
digest = "sha256:" + hashlib.sha256(target.strip().encode("utf-8")).hexdigest()
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsInventoryGraphSSHReachability",
    "status": status,
    "targetId": "host/gitlab-app-01",
    "targetDigest": digest,
    "checkedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
with open(output, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
if status != "succeeded":
    raise SystemExit("ssh reachability failed")
PY

ops_log "verify graph outputs"
python3 - "${graph_json}" "${graph_html}" "${ssh_json}" "${check_json}" "${OPS_SECRET_CANARY}" "${package_test_status}" "${json_status}" "${html_status}" "${ssh_status}" <<'PY'
import json
import sys
from pathlib import Path

json_path = Path(sys.argv[1])
html_path = Path(sys.argv[2])
ssh_path = Path(sys.argv[3])
output = Path(sys.argv[4])
canary = sys.argv[5]
package_status, json_status, html_status, ssh_status = sys.argv[6:10]
errors = []
raw_json = json_path.read_text(encoding="utf-8") if json_path.is_file() else ""
raw_html = html_path.read_text(encoding="utf-8") if html_path.is_file() else ""
doc = json.loads(raw_json) if raw_json else {}
ssh = json.loads(ssh_path.read_text(encoding="utf-8")) if ssh_path.is_file() else {}
nodes = doc.get("nodes", [])
edges = doc.get("edges", [])
selected = doc.get("selectedTargetIds", [])
node_ids = sorted(node.get("id") for node in nodes if isinstance(node, dict))
edge_kinds = sorted(set(edge.get("kind") for edge in edges if isinstance(edge, dict)))
if package_status != "succeeded":
    errors.append("inventory graph package or CLI tests failed")
if json_status != "succeeded":
    errors.append("JSON graph command failed")
if html_status != "succeeded":
    errors.append("HTML graph command failed")
if ssh_status != "succeeded" or ssh.get("status") != "succeeded":
    errors.append("lab.ssh-linux reachability failed")
if doc.get("apiVersion") != "torque.dev/ops/inventory/v1alpha1":
    errors.append("graph apiVersion mismatch")
if doc.get("kind") != "InventoryGraph":
    errors.append("graph kind mismatch")
if selected != ["host/gitlab-db-01", "host/gitlab-db-02"]:
    errors.append(f"selected target IDs mismatch: {selected}")
for expected in ["target/host/gitlab-db-01", "target/host/gitlab-app-01", "transport/ssh/gitlab-db-01", "group/gitlab"]:
    if expected not in node_ids:
        errors.append(f"missing graph node {expected}")
for expected in ["contains", "selects", "uses"]:
    if expected not in edge_kinds:
        errors.append(f"missing graph edge kind {expected}")
if "<!doctype html>" not in raw_html or "target/host/gitlab-app-01" not in raw_html:
    errors.append("HTML graph missing expected content")
combined = raw_json + raw_html
if canary in combined:
    errors.append("secret canary leaked into graph output")
if "secret://" in combined:
    errors.append("secret reference leaked into graph output")

check = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsInventoryGraphCheck",
    "status": "succeeded" if not errors else "failed",
    "selectedTargetIds": selected,
    "nodeCount": len(nodes),
    "edgeCount": len(edges),
    "edgeKinds": edge_kinds,
    "sshTargetDigest": ssh.get("targetDigest", ""),
    "secretCanaryLeak": canary in combined,
    "secretReferenceLeak": "secret://" in combined,
    "errors": errors,
}
output.write_text(json.dumps(check, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY

python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${check_json}" \
  "${ssh_json}" \
  "${package_test_status}" \
  "${json_status}" \
  "${html_status}" \
  "${ssh_status}" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
check_path = Path(sys.argv[5])
ssh_path = Path(sys.argv[6])
package_status, json_status, html_status, ssh_status = sys.argv[7:11]

def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        json.dump(doc, f, indent=2, sort_keys=True)
        f.write("\n")

check = json.loads(check_path.read_text(encoding="utf-8")) if check_path.is_file() else {}
ssh = json.loads(ssh_path.read_text(encoding="utf-8")) if ssh_path.is_file() else {}
errors = []
if package_status != "succeeded":
    errors.append("inventory graph tests failed")
if json_status != "succeeded" or html_status != "succeeded":
    errors.append("inventory graph command failed")
if ssh_status != "succeeded":
    errors.append("ssh reachability failed")
if check.get("status") != "succeeded":
    errors.append("inventory graph output check failed")
if check.get("secretCanaryLeak") or check.get("secretReferenceLeak"):
    errors.append("redaction leak found")

status = "succeeded" if not errors else "failed"
lab_profiles = ["lab.local", "lab.ssh-linux"]
selected_target_ids = check.get("selectedTargetIds", [])
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(lab_profiles),
        "inventoryCommand": "torque ops inventory graph",
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
                "targetId": "host/gitlab-app-01",
                "type": "host",
                "profile": "lab.ssh-linux",
                "transport": "ssh",
                "targetDigest": ssh.get("targetDigest", ""),
            }
        ] + [
            {
                "targetId": target_id,
                "type": "host",
                "profile": "lab.local",
                "transport": "fixture",
            }
            for target_id in selected_target_ids
        ],
        "targetCount": 1 + len(selected_target_ids),
    },
)
write_json(
    run_dir / "decision.json",
    {
        "status": "succeeded" if not errors else "blocked",
        "decision": "allow" if not errors else "block",
        "taskId": task_id,
        "runId": run_id,
        "reason": "inventory-graph-export-proof",
        "labProfiles": lab_profiles,
        "packageTests": package_status,
        "jsonCommand": json_status,
        "htmlCommand": html_status,
        "sshReachability": ssh_status,
        "nodeCount": check.get("nodeCount", 0),
        "edgeCount": check.get("edgeCount", 0),
        "selectedTargetIds": selected_target_ids,
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
        "selectedTargetIds": selected_target_ids,
        "nodeCount": check.get("nodeCount", 0),
        "edgeCount": check.get("edgeCount", 0),
        "edgeKinds": check.get("edgeKinds", []),
        "sshTargetDigest": ssh.get("targetDigest", ""),
        "secretCanaryLeak": check.get("secretCanaryLeak"),
        "secretReferenceLeak": check.get("secretReferenceLeak"),
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
        "inventoryCommand": "torque ops inventory graph",
        "selectedTargetIds": selected_target_ids,
        "nodeCount": check.get("nodeCount", 0),
        "edgeCount": check.get("edgeCount", 0),
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
