#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-K8S-001.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test namespace and local scratch. Default.
  --no-cleanup           Leave scratch/namespace state for debugging.
  -h, --help             Show this help.

OPS-K8S-001 proves `k8s.manifest.apply` on lab.k3s. It applies a
namespace-scoped ConfigMap and Deployment with server-side diff, verifies
field-manager ownership, repeats apply as a no-op, exports/audits evidence,
then deletes through the stack and proves cleanup.

Environment:
  TORQUE_OPS_E2E_CONFIRM=1
  TORQUE_LAB_K3S_SSH=ssh://root@141.105.65.227  optional
  TORQUE_LAB_K3S_KUBECTL='k3s kubectl'          optional
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

[[ "${TORQUE_OPS_E2E_CONFIRM:-}" == "1" ]] || ops_fail "refusing lab.k3s E2E without TORQUE_OPS_E2E_CONFIRM=1"
export TORQUE_LAB_K3S_SSH="${TORQUE_LAB_K3S_SSH:-${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}}"
export TORQUE_LAB_K3S_KUBECTL="${TORQUE_LAB_K3S_KUBECTL:-k3s kubectl}"

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd ssh

repo_root="$(ops_repo_root)"
ops_init_run "OPS-K8S-001"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-k8s-001.XXXXXX")"
stack_root="${scratch_root}/stack"
namespace="tqops001-$(date -u +%s)-$$"
apply_run_id=""
repeat_run_id=""
delete_run_id=""
cleanup_status="pending"

cleanup_lab_resources() {
  local status="succeeded"
  local scratch_status="not-requested"
  local namespace_status="not-requested"
  if [[ "${cleanup_enabled}" == "1" ]]; then
    rm -rf "${scratch_root}"
    if [[ -e "${scratch_root}" ]]; then
      scratch_status="failed"
      status="failed"
    else
      scratch_status="deleted"
    fi
    ops_set_ssh_base_args
    if ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "${TORQUE_LAB_K3S_KUBECTL} delete namespace '${namespace}' --ignore-not-found=true >/dev/null 2>&1 || true"; then
      namespace_status="delete-requested"
    else
      namespace_status="failed"
      status="failed"
    fi
  else
    scratch_status="skipped"
    namespace_status="skipped"
  fi
  cleanup_status="${status}"
  mkdir -p "${OPS_RUN_DIR}/cleanup"
  ops_write_json_object "${OPS_RUN_DIR}/cleanup/receipt.json" \
    status="${status}" \
    taskId="${OPS_TASK_ID}" \
    runId="${OPS_RUN_ID}" \
    labProfiles=lab.k3s \
    namespace="${namespace}" \
    scratchRoot="${scratch_root}" \
    scratch="${scratch_status}" \
    namespaceCleanup="${namespace_status}" \
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/ssh" "${OPS_RUN_DIR}/verification"

ops_set_ssh_base_args
ops_log "verify lab.k3s access"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; command -v python3 >/dev/null; ${TORQUE_LAB_K3S_KUBECTL} version --request-timeout=10s >/dev/null; ${TORQUE_LAB_K3S_KUBECTL} delete namespace '${namespace}' --ignore-not-found=true >/dev/null 2>&1 || true; ${TORQUE_LAB_K3S_KUBECTL} create namespace '${namespace}' >/dev/null" >"${OPS_RUN_DIR}/ssh/k3s-preflight.out" 2>"${OPS_RUN_DIR}/ssh/k3s-preflight.stderr"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create k8s.manifest.apply stack fixture"
python3 - "${stack_root}" "${namespace}" "${TORQUE_LAB_K3S_KUBECTL}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
namespace = sys.argv[2]
kubectl = sys.argv[3]
root.mkdir(parents=True, exist_ok=True)
manifest = f"""apiVersion: v1
kind: ConfigMap
metadata:
  name: tqops001-config
  namespace: {namespace}
  labels:
    torque.dev/e2e: OPS-K8S-001
data:
  marker: OPS-K8S-001
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tqops001-deploy
  namespace: {namespace}
  labels:
    torque.dev/e2e: OPS-K8S-001
spec:
  replicas: 0
  selector:
    matchLabels:
      app: tqops001
  template:
    metadata:
      labels:
        app: tqops001
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
"""
root.joinpath("stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-k8s-001
cli:
  inferDeps: false
nodes:
  - name: apply-manifest
    kind: k8s.manifest.apply
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: {kubectl!r}
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      manifest:
        namespace: {namespace!r}
        fieldManager: torque-ops-k8s-001
        forceConflicts: true
        removeOnDelete: true
        content: |
{''.join('          ' + line + chr(10) for line in manifest.splitlines())}
""",
    encoding="utf-8",
)
PY

ops_log "plan k8s.manifest.apply stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply k8s.manifest.apply stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover k8s.manifest.apply apply run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/apply-run-id.txt"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get configmap tqops001-config -o json" >"${OPS_RUN_DIR}/ssh/configmap.apply.json" 2>"${OPS_RUN_DIR}/ssh/configmap.apply.stderr"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get deployment tqops001-deploy -o json" >"${OPS_RUN_DIR}/ssh/deployment.apply.json" 2>"${OPS_RUN_DIR}/ssh/deployment.apply.stderr"

ops_log "audit first k8s.manifest.apply run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-apply.json" 2>"${OPS_RUN_DIR}/stack/audit-apply.stderr"

ops_log "repeat apply to prove Kubernetes no-op"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/repeat-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/repeat-apply.stderr"

repeat_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${repeat_run_id}" ]] || ops_fail "failed to discover repeat k8s.manifest.apply run ID"
printf '%s\n' "${repeat_run_id}" >"${OPS_RUN_DIR}/stack/repeat-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${repeat_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-repeat.json" 2>"${OPS_RUN_DIR}/stack/audit-repeat.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${repeat_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ops_log "delete Kubernetes manifest through stack"
(
  cd "${repo_root}"
  ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/delete.jsonl" 2>"${OPS_RUN_DIR}/stack/delete.stderr"

delete_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${delete_run_id}" ]] || ops_fail "failed to discover k8s.manifest.apply delete run ID"
printf '%s\n' "${delete_run_id}" >"${OPS_RUN_DIR}/stack/delete-run-id.txt"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${delete_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit-delete.json" 2>"${OPS_RUN_DIR}/stack/audit-delete.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "! ${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get configmap tqops001-config >/dev/null 2>&1 && ! ${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get deployment tqops001-deploy >/dev/null 2>&1" >"${OPS_RUN_DIR}/ssh/manifest-delete-check.out" 2>"${OPS_RUN_DIR}/ssh/manifest-delete-check.stderr"

ops_log "verify k8s.manifest.apply evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${namespace}" \
  "${apply_run_id}" \
  "${repeat_run_id}" \
  "${delete_run_id}" <<'PY'
import hashlib
import json
import sys
import time
from pathlib import Path

run_dir = Path(sys.argv[1])
task_id = sys.argv[2]
run_id = sys.argv[3]
started_at = sys.argv[4]
namespace = sys.argv[5]
apply_run_id = sys.argv[6]
repeat_run_id = sys.argv[7]
delete_run_id = sys.argv[8]
node_id = "k8s.manifest.apply/apply-manifest"

def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path, doc):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, name):
    for item in audit.get("artifacts", []):
        if item.get("nodeId") == node_id and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
plan = read_json(run_dir / "stack" / "plan.json")
apply_audit = read_json(run_dir / "stack" / "audit-apply.json")
repeat_audit = read_json(run_dir / "stack" / "audit-repeat.json")
delete_audit = read_json(run_dir / "stack" / "audit-delete.json")
configmap = read_json(run_dir / "ssh" / "configmap.apply.json")
deployment = read_json(run_dir / "ssh" / "deployment.apply.json")

nodes = {node.get("id"): node for node in plan.get("nodes", [])}
if node_id not in nodes:
    errors.append(f"plan missing {node_id}")
for label, audit in (("apply", apply_audit), ("repeat", repeat_audit), ("delete", delete_audit)):
    if audit.get("status") != "succeeded":
        errors.append(f"{label} audit status is {audit.get('status')}")
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")

apply_receipt = artifact(apply_audit, "k8s-manifest-apply.json")
apply_diff = artifact(apply_audit, "k8s-manifest-diff.json")
apply_verify = artifact(apply_audit, "k8s-manifest-verify.json")
repeat_receipt = artifact(repeat_audit, "k8s-manifest-apply.json")
delete_receipt = artifact(delete_audit, "k8s-manifest-apply.json")
delete_verify = artifact(delete_audit, "k8s-manifest-verify.json")

resources = (apply_receipt.get("after") or {}).get("resources") or []
delete_resources = (delete_receipt.get("after") or {}).get("resources") or []
if apply_receipt.get("status") != "succeeded" or apply_receipt.get("changed") is not True:
    errors.append("first apply receipt did not record a changed manifest apply")
if len(resources) != 2 or not all(item.get("exists") and item.get("owned") for item in resources):
    errors.append(f"first apply did not prove owned resources: {resources}")
if apply_diff.get("diffQuality") != "server-side" or apply_diff.get("changed") is not True:
    errors.append("server-side diff receipt missing changed proof")
if apply_verify.get("status") != "succeeded" or not all(item.get("owned") for item in apply_verify.get("resources") or []):
    errors.append("verify receipt did not prove field-manager ownership")
if repeat_receipt.get("status") != "succeeded" or repeat_receipt.get("changed") is not False:
    errors.append("repeat apply was not a no-op")
if delete_receipt.get("status") != "succeeded" or delete_receipt.get("desiredState") != "absent":
    errors.append("delete receipt did not target absent state")
if not delete_resources or not all(item.get("exists") is False for item in delete_resources):
    errors.append(f"delete receipt did not prove cleanup: {delete_resources}")
if delete_verify.get("status") != "succeeded":
    errors.append("delete verify receipt did not succeed")
for name in ("k8s-manifest-observe.json", "k8s-manifest-plan.json", "k8s-manifest-diff.json", "k8s-manifest-apply.json", "k8s-manifest-verify.json", "k8s-manifest.json"):
    if not artifact(apply_audit, name):
        errors.append(f"apply audit missing {name}")
stack_export = run_dir / "stack" / "stack-export.tgz"
if not stack_export.is_file() or stack_export.stat().st_size <= 0:
    errors.append("stack export bundle missing")
if configmap.get("metadata", {}).get("namespace") != namespace or configmap.get("data", {}).get("marker") != "OPS-K8S-001":
    errors.append("remote ConfigMap state mismatch")
if deployment.get("metadata", {}).get("namespace") != namespace or deployment.get("spec", {}).get("replicas") != 0:
    errors.append("remote Deployment state mismatch")

status = "succeeded" if not errors else "failed"
finished_at = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
write_json(run_dir / "metadata.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabRunMetadata",
    "taskId": task_id,
    "runId": run_id,
    "startedAt": started_at,
    "finishedAt": finished_at,
    "labProfiles": ["lab.k3s"],
})
write_json(run_dir / "target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "k8s/namespace/" + namespace, "type": "kubernetes-namespace", "namespaceDigest": "sha256:" + hashlib.sha256(namespace.encode("utf-8")).hexdigest()},
        {"id": "k8s/configmap/tqops001-config", "type": "configmap", "namespace": namespace},
        {"id": "k8s/deployment/tqops001-deploy", "type": "deployment", "namespace": namespace},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-k8s-manifest-apply-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "k8s.manifest.apply",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "namespace": namespace,
    "applyRunId": apply_run_id,
    "repeatRunId": repeat_run_id,
    "deleteRunId": delete_run_id,
    "applyChanged": apply_receipt.get("changed"),
    "repeatChanged": repeat_receipt.get("changed"),
    "deleteResourcesAbsent": bool(delete_resources) and all(item.get("exists") is False for item in delete_resources),
    "errors": errors,
    "verifiedAt": finished_at,
})
write_json(run_dir / "result.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabResult",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "finishedAt": finished_at,
    "nodeKind": "k8s.manifest.apply",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
