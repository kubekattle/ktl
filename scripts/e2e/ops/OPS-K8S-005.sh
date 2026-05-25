#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-K8S-005.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test namespace and local scratch. Default.
  --no-cleanup           Leave scratch/namespace state for debugging.
  -h, --help             Show this help.

OPS-K8S-005 proves `k8s.events.capture` on lab.k3s. It creates normal and
warning namespace events, captures all namespace events and a warning-only
filtered view, audits/exports the run, and proves cleanup.

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
ops_init_run "OPS-K8S-005"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-k8s-005.XXXXXX")"
stack_root="${scratch_root}/stack"
namespace="tqops005-$(date -u +%s)-$$"
apply_run_id=""
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

ops_log "create k8s.events.capture stack fixture"
python3 - "${stack_root}" "${namespace}" "${TORQUE_LAB_K3S_KUBECTL}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
namespace = sys.argv[2]
kubectl = sys.argv[3]
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-k8s-005
cli:
  inferDeps: false
nodes:
  - name: capture-events
    kind: k8s.events.capture
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: {kubectl!r}
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      events:
        namespace: {namespace!r}
        eventLimit: 100
  - name: capture-warning-events
    kind: k8s.events.capture
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: {kubectl!r}
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      events:
        namespace: {namespace!r}
        types: [Warning]
        eventLimit: 100
""", encoding="utf-8")

root.joinpath("seed.yaml").write_text(f"""apiVersion: v1
kind: Pod
metadata:
  name: tqops005-normal
  namespace: {namespace}
  labels:
    app: tqops005-normal
    torque.dev/e2e: OPS-K8S-005
spec:
  restartPolicy: Never
  containers:
    - name: app
      image: registry.k8s.io/e2e-test-images/busybox:1.29-4
      command: ["/bin/sh", "-c", "echo tqops005-normal; sleep 3600"]
---
apiVersion: v1
kind: Pod
metadata:
  name: tqops005-warning
  namespace: {namespace}
  labels:
    app: tqops005-warning
    torque.dev/e2e: OPS-K8S-005
spec:
  restartPolicy: Never
  containers:
    - name: broken
      image: registry.invalid/torque/ops-k8s-005-missing:never
      command: ["/bin/sh", "-c", "true"]
""", encoding="utf-8")
PY

ops_log "seed normal and warning event pods"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; tmp=\$(mktemp /tmp/torque-ops-k8s-005-seed.XXXXXX.yaml); cat >\"\${tmp}\"; ${TORQUE_LAB_K3S_KUBECTL} apply -f \"\${tmp}\" >/dev/null; rm -f \"\${tmp}\"; ${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' wait --for=condition=Ready pod/tqops005-normal --timeout=120s >/dev/null" <"${stack_root}/seed.yaml" >"${OPS_RUN_DIR}/ssh/seed.out" 2>"${OPS_RUN_DIR}/ssh/seed.stderr"

ops_log "wait for normal and warning events"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; for i in \$(seq 1 60); do if ${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get events -o json | python3 -c 'import json,sys; doc=json.load(sys.stdin); types={item.get(\"type\") for item in doc.get(\"items\", [])}; sys.exit(0 if \"Normal\" in types and \"Warning\" in types else 1)'; then echo events-ready; exit 0; fi; sleep 2; done; echo events-not-ready >&2; exit 1" >"${OPS_RUN_DIR}/ssh/event-readiness.out" 2>"${OPS_RUN_DIR}/ssh/event-readiness.stderr"

ops_log "plan k8s.events.capture stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply k8s.events.capture stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover k8s.events.capture run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/run-id.txt"

ops_log "audit and export k8s.events.capture run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit.json" 2>"${OPS_RUN_DIR}/stack/audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${apply_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get pods tqops005-normal tqops005-warning -o json" >"${OPS_RUN_DIR}/ssh/pods.after.json" 2>"${OPS_RUN_DIR}/ssh/pods.after.stderr"

ops_log "verify k8s.events.capture evidence"
python3 - \
  "${OPS_RUN_DIR}" \
  "${OPS_TASK_ID}" \
  "${OPS_RUN_ID}" \
  "${started_at}" \
  "${namespace}" \
  "${apply_run_id}" <<'PY'
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
all_node = "k8s.events.capture/capture-events"
warning_node = "k8s.events.capture/capture-warning-events"

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
plan = read_json(run_dir / "stack" / "plan.json")
audit = read_json(run_dir / "stack" / "audit.json")
pods = read_json(run_dir / "ssh" / "pods.after.json")

planned = {node.get("id") for node in plan.get("nodes", [])}
for node_id in (all_node, warning_node):
    if node_id not in planned:
        errors.append(f"plan missing {node_id}")
if audit.get("status") != "succeeded":
    errors.append(f"audit status is {audit.get('status')}")
integrity = audit.get("integrity", {})
if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
    errors.append("audit integrity failed")

all_events = artifact(audit, all_node, "k8s-events-capture-events.json")
all_verify = artifact(audit, all_node, "k8s-events-capture-verify.json")
warning_events = artifact(audit, warning_node, "k8s-events-capture-events.json")
warning_verify = artifact(audit, warning_node, "k8s-events-capture-verify.json")
for node_id in (all_node, warning_node):
    for name in ("k8s-events-capture-observe.json", "k8s-events-capture-plan.json", "k8s-events-capture-events.json", "k8s-events-capture-verify.json", "k8s-events-capture.json"):
        if not artifact(audit, node_id, name):
            errors.append(f"{node_id} audit missing {name}")

all_evidence = all_events.get("evidence") or {}
warning_evidence = warning_events.get("evidence") or {}
all_type_counts = all_evidence.get("typeCounts") or {}
warning_type_counts = warning_evidence.get("typeCounts") or {}
if all_events.get("status") != "succeeded" or all_verify.get("status") != "succeeded":
    errors.append("all-events capture did not succeed")
if warning_events.get("status") != "succeeded" or warning_verify.get("status") != "succeeded":
    errors.append("warning-events capture did not succeed")
if all_events.get("changed") is not False or warning_events.get("changed") is not False:
    errors.append("events capture should be non-mutating")
if all_type_counts.get("Normal", 0) < 1:
    errors.append("all-events capture missing Normal event")
if all_type_counts.get("Warning", 0) < 1:
    errors.append("all-events capture missing Warning event")
if warning_type_counts.get("Warning", 0) < 1:
    errors.append("warning-only capture missing Warning event")
if warning_type_counts.get("Normal", 0) != 0:
    errors.append("warning-only capture included Normal event")
if warning_evidence.get("filteredOutCount", 0) < 1:
    errors.append("warning-only capture did not prove filtering")
for label, evidence in (("all", all_evidence), ("warning", warning_evidence)):
    if evidence.get("capturedCount", 0) < 1:
        errors.append(f"{label} capture count is empty")
    redaction = evidence.get("redaction") or {}
    if not redaction.get("noSensitiveKeyValues") or not redaction.get("noSecretRefs") or not redaction.get("noAuthorizationBearer"):
        errors.append(f"{label} redaction proof failed")
    for event in evidence.get("events") or []:
        if not event.get("messageDigest"):
            errors.append(f"{label} event missing message digest")
event_text = json.dumps({"all": all_events, "warning": warning_events}, sort_keys=True)
if "registry.invalid/torque/ops-k8s-005-missing" in event_text:
    errors.append("event artifact leaked raw warning message")
export_path = run_dir / "stack" / "stack-export.tgz"
if not export_path.is_file() or export_path.stat().st_size <= 0:
    errors.append("stack export missing")
items = {item.get("metadata", {}).get("name"): item for item in pods.get("items", [])}
if "tqops005-normal" not in items or "tqops005-warning" not in items:
    errors.append("remote pod snapshot missing seeded pods")

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
        {"id": "k8s/pod/tqops005-normal", "type": "pod", "namespace": namespace},
        {"id": "k8s/pod/tqops005-warning", "type": "pod", "namespace": namespace},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-k8s-events-capture-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "k8s.events.capture",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "namespace": namespace,
    "applyRunId": apply_run_id,
    "allTypeCounts": all_type_counts,
    "warningTypeCounts": warning_type_counts,
    "warningFilteredOutCount": warning_evidence.get("filteredOutCount", 0),
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
    "nodeKind": "k8s.events.capture",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
