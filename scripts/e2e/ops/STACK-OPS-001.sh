#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/STACK-OPS-001.sh [options]

Options:
  --evidence-root DIR  Evidence root. Defaults to a temp directory.
  --cleanup            Clean lab resources. Default.
  --no-cleanup         Leave lab resources for debugging.
  -h, --help           Show this help.

STACK-OPS-001 proves generic stack nodes with a backward-compatible releases:
alias. It plans and applies one host.command.run node over SSH, one release.helm
node that depends on it, and one legacy releases: Helm node against a real
Kubernetes namespace, then audits and exports the stack run evidence.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_SSH=ssh://root@141.105.65.227   optional; defaults to this host
  KUBECONFIG_PATH=/path/to/kubeconfig        optional
  TORQUE_LAB_KUBECONFIG=/path/to/kubeconfig  optional
  KUBECONFIG=/path/to/kubeconfig             optional
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing live stack/host E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"

ops_require_cmd go
ops_require_cmd kubectl
ops_require_cmd make
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
kubeconfig_path="${KUBECONFIG_PATH:-${TORQUE_LAB_KUBECONFIG:-}}"
if [[ -z "${kubeconfig_path}" && -n "${KUBECONFIG:-}" && "${KUBECONFIG}" != *:* ]]; then
  kubeconfig_path="${KUBECONFIG}"
fi
if [[ -z "${kubeconfig_path}" ]]; then
  kubeconfig_path="${HOME}/.kube/config"
fi
[[ -f "${kubeconfig_path}" ]] || ops_fail "missing kubeconfig: ${kubeconfig_path}"

ops_init_run "STACK-OPS-001"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-stack-ops-001.XXXXXX")"
started_at="$(ops_utc_now)"
safe_suffix="$(printf '%s' "${OPS_RUN_ID}" | tr '[:upper:]_' '[:lower:]-' | tr -cd 'a-z0-9-' | cut -c1-24)"
namespace="torque-stack-ops-${safe_suffix}"
ssh_remote_root="/tmp/torque-stack-ops-001-${OPS_RUN_ID}"
stack_root="${scratch_root}/stack"
apply_run_id=""

cleanup_lab_resources() {
  local status="succeeded"
  local ssh_status="not-requested"
  local k8s_status="not-requested"
  local scratch_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    ops_set_ssh_base_args
    if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "rm -rf '${ssh_remote_root}' && test ! -e '${ssh_remote_root}'"; then
      ssh_status="deleted"
    else
      ssh_status="failed"
      status="failed"
    fi
    if kubectl --kubeconfig "${kubeconfig_path}" delete namespace "${namespace}" --ignore-not-found=true --wait=false >/dev/null 2>&1; then
      k8s_status="delete-requested"
    else
      k8s_status="failed"
      status="failed"
    fi
  fi
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles="lab.ssh-linux,lab.k8s" \
    namespace="${namespace}" \
    remoteRoot="${ssh_remote_root}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    ssh="${ssh_status}" \
    k8s="${k8s_status}" \
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/k8s" "${OPS_RUN_DIR}/ssh" "${OPS_RUN_DIR}/verification"

ops_log "build torque binary"
if make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  build_status="succeeded"
else
  build_status="failed"
fi
[[ "${build_status}" == "succeeded" ]] || ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"

ops_log "create mixed stack fixture"
python3 - "${stack_root}" "${namespace}" "${kubeconfig_path}" "${ssh_remote_root}" "${OPS_RUN_ID}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
namespace = sys.argv[2]
kubeconfig = sys.argv[3]
remote_root = sys.argv[4]
run_id = sys.argv[5]
chart = root / "charts" / "cm"
(chart / "templates").mkdir(parents=True, exist_ok=True)
(chart / "Chart.yaml").write_text("apiVersion: v2\nname: stack-ops-001-cm\nversion: 0.1.0\n", encoding="utf-8")
(chart / "values.yaml").write_text("fixture: stack-ops-001\n", encoding="utf-8")
(chart / "templates" / "configmap.yaml").write_text(
    'apiVersion: v1\n'
    'kind: ConfigMap\n'
    'metadata:\n'
    '  name: {{ .Release.Name | quote }}\n'
    'data:\n'
    '  fixture: {{ .Values.fixture | quote }}\n'
    '  runId: {{ .Values.runId | quote }}\n',
    encoding="utf-8",
)
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: stack-ops-001
cli:
  inferDeps: false
defaults:
  namespace: {namespace}
nodes:
  - name: stack-ops-001-host-prep
    kind: host.command.run
    input:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      command: "mkdir -p '{remote_root}' && printf '{run_id}' > '{remote_root}/host-prep.txt'"
      deleteCommand: "rm -rf '{remote_root}'"
  - name: stack-ops-001-app
    kind: release.helm
    chart: ./charts/cm
    cluster: {{ name: stack-ops-001, kubeconfig: {kubeconfig!r} }}
    apply: {{ createNamespace: true }}
    needs: [stack-ops-001-host-prep]
    set: {{ fixture: stack-ops-001-app, runId: {run_id} }}
releases:
  - name: stack-ops-001-legacy
    chart: ./charts/cm
    cluster: {{ name: stack-ops-001, kubeconfig: {kubeconfig!r} }}
    apply: {{ createNamespace: true }}
    set: {{ fixture: stack-ops-001-legacy, runId: {run_id} }}
""",
    encoding="utf-8",
)
PY

ops_log "plan mixed stack"
(
  cd "${repo_root}"
  ./bin/torque --kubeconfig "${kubeconfig_path}" stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "verify plan shape"
python3 - "${OPS_RUN_DIR}/stack/plan.json" "${OPS_RUN_DIR}/stack/plan-check.json" <<'PY'
import json
import sys
from pathlib import Path

plan_path = Path(sys.argv[1])
out = Path(sys.argv[2])
plan = json.loads(plan_path.read_text(encoding="utf-8"))
nodes = {node.get("id"): node for node in plan.get("nodes", [])}
errors = []
host_id = "host.command.run/stack-ops-001-host-prep"
app_id = "stack-ops-001/torque-stack-ops-"  # prefix check below
legacy_name = "stack-ops-001-legacy"
host = nodes.get(host_id)
if not host:
    errors.append("missing host.command.run node id")
if host and host.get("cluster", {}).get("name"):
    errors.append("host node unexpectedly required cluster.name")
app = next((n for n in nodes.values() if n.get("name") == "stack-ops-001-app"), None)
if not app:
    errors.append("missing release.helm app node")
elif "stack-ops-001-host-prep" not in app.get("needs", []):
    errors.append("app node missing dependency on host node")
legacy = next((n for n in nodes.values() if n.get("name") == legacy_name), None)
if not legacy:
    errors.append("missing legacy releases: alias node")
elif (legacy.get("kind") or "release.helm") != "release.helm":
    errors.append("legacy releases: alias did not normalize to release.helm")
if app and app_id not in app.get("id", ""):
    errors.append(f"app helm id did not preserve cluster/namespace/name shape: {app.get('id')}")
doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "StackOps001PlanCheck",
    "status": "succeeded" if not errors else "failed",
    "nodeCount": len(nodes),
    "errors": errors,
}
out.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY

ops_log "apply mixed stack against SSH host and Kubernetes"
(
  cd "${repo_root}"
  ./bin/torque --kubeconfig "${kubeconfig_path}" stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover stack apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ops_set_ssh_base_args
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "cat '${ssh_remote_root}/host-prep.txt'" >"${OPS_RUN_DIR}/ssh/host-prep-marker.txt"
kubectl --kubeconfig "${kubeconfig_path}" -n "${namespace}" get configmap stack-ops-001-app -o json >"${OPS_RUN_DIR}/k8s/app-configmap.json"
kubectl --kubeconfig "${kubeconfig_path}" -n "${namespace}" get configmap stack-ops-001-legacy -o json >"${OPS_RUN_DIR}/k8s/legacy-configmap.json"

ops_log "audit and export stack run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit.json" 2>"${OPS_RUN_DIR}/stack/audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${apply_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete mixed stack"
(
  cd "${repo_root}"
  ./bin/torque --kubeconfig "${kubeconfig_path}" stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_SSH}")" "test ! -e '${ssh_remote_root}'" >"${OPS_RUN_DIR}/ssh/delete-check.out" 2>"${OPS_RUN_DIR}/ssh/delete-check.stderr"
if kubectl --kubeconfig "${kubeconfig_path}" -n "${namespace}" get configmap stack-ops-001-app >/dev/null 2>&1; then
  ops_fail "stack delete left app ConfigMap behind"
fi

ops_log "verify stack evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${namespace}" \
  "${OPS_RUN_DIR}/stack/plan-check.json" \
  "${OPS_RUN_DIR}/stack/audit.json" \
  "${OPS_RUN_DIR}/ssh/host-prep-marker.txt" \
  "${OPS_RUN_DIR}/k8s/app-configmap.json" \
  "${OPS_RUN_DIR}/k8s/legacy-configmap.json" <<'PY'
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
namespace = sys.argv[5]
plan_check_path = Path(sys.argv[6])
audit_path = Path(sys.argv[7])
host_marker_path = Path(sys.argv[8])
app_cm_path = Path(sys.argv[9])
legacy_cm_path = Path(sys.argv[10])

def read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path: Path, doc: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

errors = []
plan_check = read_json(plan_check_path)
audit = read_json(audit_path)
app_cm = read_json(app_cm_path)
legacy_cm = read_json(legacy_cm_path)
if plan_check.get("status") != "succeeded":
    errors.append("plan shape check failed")
if host_marker_path.read_text(encoding="utf-8").strip() != run_id:
    errors.append("ssh host marker did not match run id")
if app_cm.get("data", {}).get("runId") != run_id:
    errors.append("app ConfigMap runId mismatch")
if legacy_cm.get("data", {}).get("runId") != run_id:
    errors.append("legacy ConfigMap runId mismatch")
if audit.get("status") != "succeeded":
    errors.append(f"stack audit status is {audit.get('status')}")
integrity = audit.get("integrity", {})
if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
    errors.append("stack audit integrity failed")
artifacts = audit.get("artifacts", [])
host_artifact = None
for artifact in artifacts:
    if artifact.get("nodeId") == "host.command.run/stack-ops-001-host-prep" and artifact.get("name") == "host-command.json":
        host_artifact = artifact
        break
if not host_artifact:
    errors.append("host command artifact missing from stack audit")
else:
    body = json.loads(host_artifact.get("body") or "{}")
    if body.get("status") != "succeeded":
        errors.append("host command artifact did not succeed")
    receipt = body.get("receipt", {})
    if receipt.get("status") != "succeeded" or not receipt.get("targetDigest"):
        errors.append("host command receipt missing success target digest")
stack_export = run_dir / "stack" / "stack-export.tgz"
if not stack_export.is_file() or stack_export.stat().st_size <= 0:
    errors.append("stack export bundle missing")

status = "succeeded" if not errors else "failed"
profiles = ["lab.ssh-linux", "lab.k8s"]
write_json(
    run_dir / "metadata.json",
    {
        "taskId": task_id,
        "runId": run_id,
        "startedAt": started_at,
        "profiles": ",".join(profiles),
        "namespace": namespace,
    },
)
write_json(
    run_dir / "target-snapshot.json",
    {
        "apiVersion": "torque.dev/e2e/v1",
        "kind": "OpsLabTargetSnapshot",
        "taskId": task_id,
        "runId": run_id,
        "profiles": profiles,
        "targets": [
            {
                "targetId": "host/lab-ssh",
                "type": "host",
                "profile": "lab.ssh-linux",
                "transport": "ssh",
            },
            {
                "targetId": f"k8s/{namespace}",
                "type": "kubernetes-namespace",
                "profile": "lab.k8s",
                "namespace": namespace,
            },
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
        "reason": "generic-stack-nodes-release-alias-host-and-k8s-proof",
        "labProfiles": profiles,
        "nodeKinds": ["host.command.run", "release.helm"],
        "backwardCompatibleAlias": "releases",
        "decidedAt": started_at,
    },
)
write_json(
    run_dir / "verification" / "receipt.json",
    {
        "status": status,
        "taskId": task_id,
        "runId": run_id,
        "labProfiles": profiles,
        "namespace": namespace,
        "planCheck": plan_check.get("status"),
        "stackAuditStatus": audit.get("status"),
        "stackAuditEventsOK": integrity.get("eventsOk"),
        "stackAuditRunDigestOK": integrity.get("runDigestOk"),
        "hostMarkerMatched": host_marker_path.read_text(encoding="utf-8").strip() == run_id,
        "appConfigMapObserved": app_cm.get("metadata", {}).get("name") == "stack-ops-001-app",
        "legacyConfigMapObserved": legacy_cm.get("metadata", {}).get("name") == "stack-ops-001-legacy",
        "hostCommandArtifactObserved": host_artifact is not None,
        "stackExportBundleBytes": stack_export.stat().st_size if stack_export.is_file() else 0,
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
        "labProfiles": profiles,
        "nodeKinds": ["host.command.run", "release.helm"],
        "backwardCompatibleAlias": "releases",
        "stackAuditRunId": audit.get("runId"),
    },
)
if errors:
    raise SystemExit("; ".join(errors))
PY
