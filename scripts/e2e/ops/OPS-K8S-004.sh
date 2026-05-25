#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-K8S-004.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Remove the test namespace and local scratch. Default.
  --no-cleanup           Leave scratch/namespace state for debugging.
  -h, --help             Show this help.

OPS-K8S-004 proves `k8s.logs.capture` on lab.k3s. It creates an ephemeral
Deployment that emits normal and secret-like log lines, captures bounded logs
through a stack node, and proves the evidence contains redacted log lines,
digests, byte/line limits, exported audit evidence, and cleanup proof.

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
ops_init_run "OPS-K8S-004"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-k8s-004.XXXXXX")"
stack_root="${scratch_root}/stack"
namespace="tqops004-$(date -u +%s)-$$"
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

ops_log "create k8s.logs.capture stack fixture"
python3 - "${stack_root}" "${namespace}" "${TORQUE_LAB_K3S_KUBECTL}" <<'PY'
from pathlib import Path
import sys

root = Path(sys.argv[1])
namespace = sys.argv[2]
kubectl = sys.argv[3]
root.mkdir(parents=True, exist_ok=True)
root.joinpath("stack.yaml").write_text(f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-k8s-004
cli:
  inferDeps: false
nodes:
  - name: capture-logs
    kind: k8s.logs.capture
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: {kubectl!r}
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      logs:
        namespace: {namespace!r}
        kind: deployment
        name: tqops004-logs
        container: app
        tailLines: 20
        limitBytes: 4096
        timestamps: true
        maxLogRequests: 3
""", encoding="utf-8")

root.joinpath("seed.yaml").write_text(f"""apiVersion: apps/v1
kind: Deployment
metadata:
  name: tqops004-logs
  namespace: {namespace}
  labels:
    app: tqops004-logs
    torque.dev/e2e: OPS-K8S-004
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tqops004-logs
  template:
    metadata:
      labels:
        app: tqops004-logs
    spec:
      containers:
        - name: app
          image: registry.k8s.io/e2e-test-images/busybox:1.29-4
          command:
            - /bin/sh
            - -c
            - |
              echo "ops-k8s-004-safe-line"
              echo "password=ops-k8s-004-secret"
              echo "token=ops-k8s-004-token"
              echo "ops-k8s-004-final-line"
              sleep 3600
""", encoding="utf-8")
PY

ops_log "seed log-emitting deployment"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; tmp=\$(mktemp /tmp/torque-ops-k8s-004-seed.XXXXXX.yaml); cat >\"\${tmp}\"; ${TORQUE_LAB_K3S_KUBECTL} apply -f \"\${tmp}\" >/dev/null; rm -f \"\${tmp}\"; ${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' rollout status deployment/tqops004-logs --timeout=120s >/dev/null" <"${stack_root}/seed.yaml" >"${OPS_RUN_DIR}/ssh/seed.out" 2>"${OPS_RUN_DIR}/ssh/seed.stderr"

ops_log "verify remote log markers without storing raw logs"
ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "set -e; logs=\$(${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' logs deployment/tqops004-logs -c app --tail=20); printf '%s\n' \"\${logs}\" | grep -q 'ops-k8s-004-safe-line'; printf '%s\n' \"\${logs}\" | grep -q 'password=ops-k8s-004-secret'; printf '%s\n' \"\${logs}\" | grep -q 'token=ops-k8s-004-token'; printf 'remote log markers present\n'" >"${OPS_RUN_DIR}/ssh/log-marker-check.out" 2>"${OPS_RUN_DIR}/ssh/log-marker-check.stderr"

ops_log "plan k8s.logs.capture stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) >"${OPS_RUN_DIR}/stack/plan.json" 2>"${OPS_RUN_DIR}/stack/plan.stderr"

ops_log "apply k8s.logs.capture stack"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/stack/apply.jsonl" 2>"${OPS_RUN_DIR}/stack/apply.stderr"

apply_run_id="$(
  cd "${repo_root}"
  ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
)"
[[ -n "${apply_run_id}" ]] || ops_fail "failed to discover k8s.logs.capture run ID"
printf '%s\n' "${apply_run_id}" >"${OPS_RUN_DIR}/stack/run-id.txt"

ops_log "audit and export k8s.logs.capture run"
(
  cd "${repo_root}"
  ./bin/torque stack audit --config "${stack_root}" --run-id "${apply_run_id}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/stack/audit.json" 2>"${OPS_RUN_DIR}/stack/audit.stderr"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${apply_run_id}" --out "${OPS_RUN_DIR}/stack/stack-export.tgz"
) >"${OPS_RUN_DIR}/stack/export.out" 2>"${OPS_RUN_DIR}/stack/export.stderr"

ssh "${OPS_SSH_ARGS[@]}" "$(ops_ssh_target "${TORQUE_LAB_K3S_SSH}")" "${TORQUE_LAB_K3S_KUBECTL} -n '${namespace}' get deployment tqops004-logs -o json" >"${OPS_RUN_DIR}/ssh/deployment.after.json" 2>"${OPS_RUN_DIR}/ssh/deployment.after.stderr"

ops_log "verify k8s.logs.capture evidence"
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
node_id = "k8s.logs.capture/capture-logs"

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
audit = read_json(run_dir / "stack" / "audit.json")
deployment = read_json(run_dir / "ssh" / "deployment.after.json")

if node_id not in {node.get("id") for node in plan.get("nodes", [])}:
    errors.append(f"plan missing {node_id}")
if audit.get("status") != "succeeded":
    errors.append(f"audit status is {audit.get('status')}")
integrity = audit.get("integrity", {})
if not integrity.get("eventsOk") or not integrity.get("runDigestOk"):
    errors.append("audit integrity failed")

observe = artifact(audit, "k8s-logs-capture-observe.json")
plan_artifact = artifact(audit, "k8s-logs-capture-plan.json")
logs = artifact(audit, "k8s-logs-capture-logs.json")
verify = artifact(audit, "k8s-logs-capture-verify.json")
summary = artifact(audit, "k8s-logs-capture.json")
for name, doc in (
    ("k8s-logs-capture-observe.json", observe),
    ("k8s-logs-capture-plan.json", plan_artifact),
    ("k8s-logs-capture-logs.json", logs),
    ("k8s-logs-capture-verify.json", verify),
    ("k8s-logs-capture.json", summary),
):
    if not doc:
        errors.append(f"audit missing {name}")

log_evidence = logs.get("evidence") or {}
redaction = log_evidence.get("redaction") or {}
log_text = json.dumps(logs, sort_keys=True)
if logs.get("status") != "succeeded":
    errors.append("logs receipt did not succeed")
if logs.get("changed") is not False:
    errors.append("logs capture should be non-mutating")
if verify.get("status") != "succeeded":
    errors.append("verify receipt did not succeed")
if not (observe.get("state") or {}).get("exists"):
    errors.append("observe receipt did not prove target exists")
if (plan_artifact.get("logs") or {}).get("limitBytes") != 4096:
    errors.append("plan receipt did not preserve limitBytes")
if log_evidence.get("capturedLineCount", 0) < 4:
    errors.append("captured log line count too low")
if log_evidence.get("capturedBytes", 0) > 4096:
    errors.append("captured log bytes exceeded configured limit")
if not log_evidence.get("logDigest"):
    errors.append("missing log digest")
if "ops-k8s-004-safe-line" not in log_text:
    errors.append("redacted log evidence missing safe marker")
if "password=[REDACTED]" not in log_text or "token=[REDACTED]" not in log_text:
    errors.append("redacted log evidence missing redacted secret markers")
for leaked in ("ops-k8s-004-secret", "ops-k8s-004-token"):
    if leaked in log_text:
        errors.append(f"log evidence leaked {leaked}")
if not redaction.get("noSensitiveKeyValues") or not redaction.get("noSecretRefs") or not redaction.get("noAuthorizationBearer"):
    errors.append("redaction proof failed")
if (logs.get("receipt") or {}).get("stdoutDigest") and (logs.get("receipt") or {}).get("stdoutBytes", 0) <= 0:
    errors.append("logs command stdout digest missing byte count")
export_path = run_dir / "stack" / "stack-export.tgz"
if not export_path.is_file() or export_path.stat().st_size <= 0:
    errors.append("stack export missing")
if (deployment.get("status") or {}).get("availableReplicas", 0) < 1:
    errors.append("remote deployment is not available")

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
        {"id": "k8s/deployment/tqops004-logs", "type": "deployment", "namespace": namespace},
    ],
})
write_json(run_dir / "decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "allow-k8s-logs-capture-proof",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "nodeKind": "k8s.logs.capture",
})
write_json(run_dir / "verification" / "receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "namespace": namespace,
    "applyRunId": apply_run_id,
    "capturedLineCount": log_evidence.get("capturedLineCount", 0),
    "capturedBytes": log_evidence.get("capturedBytes", 0),
    "redactionVerified": bool(redaction.get("noSensitiveKeyValues") and redaction.get("noSecretRefs") and redaction.get("noAuthorizationBearer")),
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
    "nodeKind": "k8s.logs.capture",
})
if errors:
    raise SystemExit("; ".join(errors))
PY
