#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-K8S-003.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test namespace and local scratch. Default.
  --no-cleanup           Leave scratch/namespace state for debugging.
  -h, --help             Show this help.

OPS-K8S-003 proves `k8s.resource.wait` on lab.k3s. It waits for one Deployment
rollout to become Available, then runs a bad-image Deployment wait that times
out and proves event evidence is still captured.

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
ops_init_run "OPS-K8S-003"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-k8s-003.XXXXXX")"
success_stack_root="${scratch_root}/success"
failure_stack_root="${scratch_root}/failure"
namespace="tqops003-$(date -u +%s)-$$"
success_run_id=""
failure_run_id=""
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
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; ${TORQUE_LAB_K3S_KUBECTL} version --request-timeout=10s >/dev/null; ${TORQUE_LAB_K3S_KUBECTL} delete namespace '${namespace}' --ignore-not-found=true >/dev/null 2>&1 || true; ${TORQUE_LAB_K3S_KUBECTL} create namespace '${namespace}' >/dev/null" >"${OPS_RUN_DIR}/ssh/k3s-preflight.out" 2>"${OPS_RUN_DIR}/ssh/k3s-preflight.stderr"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create k8s.resource.wait stack fixtures"
python3 - "${success_stack_root}" "${failure_stack_root}" "${namespace}" "${TORQUE_LAB_K3S_KUBECTL}" <<'PY'
from pathlib import Path
import sys

success_root = Path(sys.argv[1])
failure_root = Path(sys.argv[2])
namespace = sys.argv[3]
kubectl = sys.argv[4]
success_root.mkdir(parents=True, exist_ok=True)
failure_root.mkdir(parents=True, exist_ok=True)

def stack(name, node_name, deploy_name, timeout):
    return f"""apiVersion: torque.dev/v1
kind: Stack
name: {name}
cli:
  inferDeps: false
nodes:
  - name: {node_name}
    kind: k8s.resource.wait
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: {kubectl!r}
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      resource:
        namespace: {namespace!r}
        kind: deployment
        name: {deploy_name}
        for: condition=Available
        timeout: {timeout}
        eventLimit: 50
"""

success_root.joinpath("stack.yaml").write_text(stack("ops-k8s-003-success", "wait-ready", "tqops003-ready", "120s"), encoding="utf-8")
failure_root.joinpath("stack.yaml").write_text(stack("ops-k8s-003-failure", "wait-failure", "tqops003-failure", "5s"), encoding="utf-8")
seed = f"""apiVersion: apps/v1
kind: Deployment
metadata:
  name: tqops003-ready
  namespace: {namespace}
  labels:
    app: tqops003-ready
    torque.dev/e2e: OPS-K8S-003
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tqops003-ready
  template:
    metadata:
      labels:
        app: tqops003-ready
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tqops003-failure
  namespace: {namespace}
  labels:
    app: tqops003-failure
    torque.dev/e2e: OPS-K8S-003
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tqops003-failure
  template:
    metadata:
      labels:
        app: tqops003-failure
    spec:
      containers:
        - name: broken
          image: registry.invalid/torque/ops-k8s-003-missing:never
"""
success_root.joinpath("seed.yaml").write_text(seed, encoding="utf-8")
PY

ops_log "seed rollout and timeout deployments"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; tmp=\$(mktemp /tmp/torque-ops-k8s-003-seed.XXXXXX.yaml); cat >\"\${tmp}\"; ${TORQUE_LAB_K3S_KUBECTL} apply -f \"\${tmp}\" >/dev/null; rm -f \"\${tmp}\"" <"${success_stack_root}/seed.yaml" >"${OPS_RUN_DIR}/ssh/seed.out" 2>"${OPS_RUN_DIR}/ssh/seed.stderr"

ops_log "plan successful k8s.resource.wait stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${success_stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/success-plan.json" 2>"${OPS_RUN_DIR}/stack/success-plan.stderr"

ops_log "apply successful k8s.resource.wait stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${success_stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/success-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/success-apply.stderr"

success_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${success_stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${success_run_id}" ]] || ops_fail "failed to discover successful k8s.resource.wait run ID"
printf '%s\n' "${success_run_id}" >"${OPS_RUN_DIR}/stack/success-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${success_stack_root}" --run-id "${success_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/success-audit.json" 2>"${OPS_RUN_DIR}/stack/success-audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${success_stack_root}" --run-id "${success_run_id}" --out "${OPS_RUN_DIR}/stack/success-stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/success-export.out" 2>"${OPS_RUN_DIR}/stack/success-export.stderr"

ops_log "run failing k8s.resource.wait timeout stack"
set +e
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${failure_stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/failure-apply.jsonl" 2>"${OPS_RUN_DIR}/stack/failure-apply.stderr"
failure_code=$?
set -e
printf '%s\n' "${failure_code}" >"${OPS_RUN_DIR}/stack/failure-apply.exit"
if [[ "${failure_code}" -eq 0 ]]; then
  ops_fail "failing k8s.resource.wait stack unexpectedly succeeded"
fi

failure_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${failure_stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${failure_run_id}" ]] || ops_fail "failed to discover failing k8s.resource.wait run ID"
printf '%s\n' "${failure_run_id}" >"${OPS_RUN_DIR}/stack/failure-run-id.txt"

(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${failure_stack_root}" --run-id "${failure_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/failure-audit.json" 2>"${OPS_RUN_DIR}/stack/failure-audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${failure_stack_root}" --run-id "${failure_run_id}" --out "${OPS_RUN_DIR}/stack/failure-stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/failure-export.out" 2>"${OPS_RUN_DIR}/stack/failure-export.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get deployment tqops003-ready tqops003-failure -o json" >"${OPS_RUN_DIR}/ssh/deployments.after.json" 2>"${OPS_RUN_DIR}/ssh/deployments.after.stderr"

ops_log "verify k8s.resource.wait evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${namespace}" \
  "${success_run_id}" \
  "${failure_run_id}" \
  "${failure_code}" <<'PY'
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
success_run_id = sys.argv[6]
failure_run_id = sys.argv[7]
failure_code = int(sys.argv[8])
success_node = "k8s.resource.wait/wait-ready"
failure_node = "k8s.resource.wait/wait-failure"

def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))

def write_json(path, doc):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")

def artifact(audit, node_id, name):
    for item in audit.get("artifacts", []):
        if item.get("nodeId") == node_id and item.get("name") == name:
            return json.loads(item.get("body") or "{}")
    return {}

errors = []
success_plan = read_json(run_dir / "stack" / "success-plan.json")
success_audit = read_json(run_dir / "stack" / "success-audit.json")
failure_audit = read_json(run_dir / "stack" / "failure-audit.json")
deployments = read_json(run_dir / "ssh" / "deployments.after.json")

if success_node not in {node.get("id") for node in success_plan.get("nodes", [])}:
    errors.append(f"success plan missing {success_node}")
if success_audit.get("status") != "succeeded":
    errors.append(f"success audit status is {success_audit.get('status')}")
if failure_audit.get("status") != "failed":
    errors.append(f"failure audit status is {failure_audit.get('status')}, want failed")
for label, audit in (("success", success_audit), ("failure", failure_audit)):
    integrity = audit.get("integrity", {})
    if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
        errors.append(f"{label} audit integrity failed")

success_apply = artifact(success_audit, success_node, "k8s-resource-wait-apply.json")
success_events = artifact(success_audit, success_node, "k8s-resource-wait-events.json")
success_verify = artifact(success_audit, success_node, "k8s-resource-wait-verify.json")
failure_apply = artifact(failure_audit, failure_node, "k8s-resource-wait-apply.json")
failure_events = artifact(failure_audit, failure_node, "k8s-resource-wait-events.json")
failure_verify = artifact(failure_audit, failure_node, "k8s-resource-wait-verify.json")

if success_apply.get("status") != "succeeded" or not ((success_apply.get("after") or {}).get("ready")):
    errors.append("success wait did not prove ready state")
if success_apply.get("changed") is not False:
    errors.append("resource wait should be non-mutating")
if success_verify.get("status") != "succeeded":
    errors.append("success verify did not succeed")
if (success_events.get("receipt") or {}).get("status") != "succeeded":
    errors.append("success events receipt did not succeed")
if not success_events.get("events"):
    errors.append("success events list is empty")
if failure_code == 0:
    errors.append("failure stack exit code was zero")
if failure_apply.get("status") != "failed" or (failure_apply.get("after") or {}).get("ready") is not False:
    errors.append("failure wait did not prove timeout/not-ready state")
reason = (failure_apply.get("reason") or "") + " " + (failure_verify.get("reason") or "")
if "timed out" not in reason.lower():
    errors.append("failure wait reason did not include timeout")
if failure_verify.get("status") != "failed":
    errors.append("failure verify did not fail")
if (failure_events.get("receipt") or {}).get("status") != "succeeded":
    errors.append("failure events receipt did not succeed")
if not failure_events.get("events"):
    errors.append("failure events list is empty")
for node_id, audit in ((success_node, success_audit), (failure_node, failure_audit)):
    for name in ("k8s-resource-wait-observe.json", "k8s-resource-wait-plan.json", "k8s-resource-wait-apply.json", "k8s-resource-wait-events.json", "k8s-resource-wait-verify.json", "k8s-resource-wait.json"):
        if not artifact(audit, node_id, name):
            errors.append(f"{node_id} audit missing {name}")
for name in ("success-stack-export.tgz", "failure-stack-export.tgz"):
    path = run_dir / "stack" / name
    if not path.is_file() or path.stat().st_size <= 0:
        errors.append(f"{name} missing")

items = {item.get("metadata", {}).get("name"): item for item in deployments.get("items", [])}
ready = items.get("tqops003-ready", {})
failure = items.get("tqops003-failure", {})
if (ready.get("status") or {}).get("availableReplicas", 0) < 1:
    errors.append("remote ready deployment is not available")
if (failure.get("status") or {}).get("availableReplicas", 0) != 0:
    errors.append("remote failure deployment unexpectedly available")

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
        {"id": "k8s/deployment/tqops003-ready", "type": "deployment", "namespace": namespace},
        {"id": "k8s/deployment/tqops003-failure", "type": "deployment", "namespace": namespace},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-k8s-resource-wait-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "k8s.resource.wait",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "namespace": namespace,
    "successRunId": success_run_id,
    "failureRunId": failure_run_id,
    "successReady": bool((success_apply.get("after") or {}).get("ready")),
    "failureTimedOut": "timed out" in reason.lower(),
    "successEventCount": len(success_events.get("events") or []),
    "failureEventCount": len(failure_events.get("events") or []),
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
    "nodeKind": "k8s.resource.wait",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
