#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./lib.sh
source "${script_dir}/lib.sh"

usage() {
  cat <<'EOF'
Usage: scripts/e2e/ops/OPS-CLI-006.sh [options]

Options:
  --evidence-root DIR    Evidence root. Defaults to a temp directory.
  --cleanup              Delete the local stack fixture after proof. Default.
  --no-cleanup           Leave the local stack fixture for debugging.
  -h, --help             Show this help.

OPS-CLI-006 proves `torque stack export` produces a portable, redacted,
hash-checked stack run bundle. It exports an explicit run ID and the default
latest run, audits the exported bundle read-only, verifies the archive contains
only the selected run, proves secret-like command material is redacted from the
portable SQLite state, and proves bundle hash tampering is rejected.
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

ops_require_cmd go
ops_require_cmd make
ops_require_cmd python3
ops_require_cmd tar

repo_root="$(ops_repo_root)"
ops_init_run "OPS-CLI-006"
started_at="$(ops_utc_now)"
scratch_root="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-cli-006.XXXXXX")"
stack_root="${scratch_root}/stack"
marker_root="${scratch_root}/marker"
explicit_export="${OPS_RUN_DIR}/export/explicit-run.tgz"
latest_export="${OPS_RUN_DIR}/export/latest-run.tgz"
tampered_export="${OPS_RUN_DIR}/export/tampered-run.tgz"
first_run_id=""
second_run_id=""
export_status=""
tamper_status=""
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
    "${scratch_root}" \
    "${stack_root}" \
    "${explicit_export}" \
    "${latest_export}" \
    "${tampered_export}" \
    "${first_run_id}" \
    "${second_run_id}" \
    "${export_status}" \
    "${tamper_status}" \
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
    scratch_root,
    stack_root,
    explicit_export,
    latest_export,
    tampered_export,
    first_run_id,
    second_run_id,
    export_status,
    tamper_status,
    cleanup_status,
    exit_code,
) = sys.argv[1:16]
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

proof_path = run / "verification" / "ops-cli-006-proof.json"
proof = load("verification/ops-cli-006-proof.json")
errors = list(proof.get("errors") or [])
if not proof_path.is_file():
    errors.append("ops-cli-006 proof missing")
if not first_run_id:
    errors.append("first run id missing")
if not second_run_id:
    errors.append("second run id missing")
if not explicit_export:
    errors.append("explicit export path missing")
if not latest_export:
    errors.append("latest export path missing")
if export_status != "passed":
    errors.append(f"export status is {export_status or 'missing'}")
if tamper_status != "failed":
    errors.append(f"tamper status is {tamper_status or 'missing'}")
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
    "labProfiles": ["lab.local"],
})
write("target-snapshot.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabTargetSnapshot",
    "taskId": task_id,
    "runId": run_id,
    "targets": [
        {"id": "local/controller", "type": "local-host", "transport": "local"},
        {"id": "stack-run/explicit", "type": "stack-run-bundle", "runId": first_run_id, "path": explicit_export},
        {"id": "stack-run/latest", "type": "stack-run-bundle", "runId": second_run_id, "path": latest_export},
    ],
})
write("decision.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabDecision",
    "taskId": task_id,
    "runId": run_id,
    "decision": "export-redacted-portable-stack-run-bundle",
    "status": "succeeded" if status == "succeeded" else "blocked",
    "stackRoot": stack_root,
    "scratchRoot": scratch_root,
})
write("verification/receipt.json", {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabVerificationReceipt",
    "taskId": task_id,
    "runId": run_id,
    "status": status,
    "exportStatus": export_status,
    "tamperStatus": tamper_status,
    "explicitRunId": first_run_id,
    "latestRunId": second_run_id,
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
    "explicitRunId": first_run_id,
    "latestRunId": second_run_id,
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

mkdir -p "${OPS_RUN_DIR}/build" "${OPS_RUN_DIR}/stack" "${OPS_RUN_DIR}/runs" "${OPS_RUN_DIR}/export" "${OPS_RUN_DIR}/audit" "${OPS_RUN_DIR}/tamper" "${OPS_RUN_DIR}/verification" "${OPS_RUN_DIR}/cleanup"

ops_log "build torque binary"
if ! make -C "${repo_root}" -s build >"${OPS_RUN_DIR}/build/make-build.out" 2>&1; then
  ops_fail "make build failed; see ${OPS_RUN_DIR}/build/make-build.out"
fi

ops_log "create local export fixture"
python3 - "${stack_root}" "${marker_root}" "${OPS_RUN_ID}" "${OPS_SECRET_CANARY}" <<'PY'
import shlex
import sys
from pathlib import Path

stack_root = Path(sys.argv[1])
marker_root = Path(sys.argv[2])
run_id = sys.argv[3]
secret = sys.argv[4]
marker = marker_root / "apply-marker.txt"

def block(value: str, spaces: int) -> str:
    pad = " " * spaces
    return "\n".join(pad + line if line else pad for line in value.rstrip("\n").splitlines())

command = f"""set -eu
mkdir -p {shlex.quote(str(marker_root))}
printf 'password={secret}\\n'
printf 'target=local://ops-cli-006\\n'
printf '{run_id}\\n' >> {shlex.quote(str(marker))}
"""
delete = f"rm -rf {shlex.quote(str(marker_root))}"

stack_root.mkdir(parents=True, exist_ok=True)
(stack_root / "stack.yaml").write_text(
    f"""apiVersion: torque.dev/v1
kind: Stack
name: ops-cli-006-export
cli:
  inferDeps: false
nodes:
  - name: export-marker
    kind: host.command.run
    host:
      transport: local
      target: local://ops-cli-006
      timeout: 20s
      command: |
{block(command, 8)}
      deleteCommand: {delete!r}
""",
    encoding="utf-8",
)
PY
printf '%s\n' "${stack_root}" >"${OPS_RUN_DIR}/stack/stack-root.txt"
printf '%s\n' "${scratch_root}" >"${OPS_RUN_DIR}/stack/scratch-root.txt"

ops_log "plan local stack"
(
  cd "${repo_root}"
  ./bin/torque stack plan --config "${stack_root}" --output json
) 2>"${OPS_RUN_DIR}/stack/plan.stderr" | ops_redact_stdin "${OPS_RUN_DIR}/stack/plan.redacted.json"

latest_run_id() {
  (
    cd "${repo_root}"
    ./bin/torque stack runs --config "${stack_root}" --output json --limit 1 |
      python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
  )
}

ops_log "apply local stack first run"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/runs/apply-first.jsonl" 2>"${OPS_RUN_DIR}/runs/apply-first.stderr"
first_run_id="$(latest_run_id)"
[[ -n "${first_run_id}" ]] || ops_fail "failed to discover first run ID"
printf '%s\n' "${first_run_id}" >"${OPS_RUN_DIR}/runs/first-run-id.txt"

ops_log "apply local stack second run"
(
  cd "${repo_root}"
  ./bin/torque stack apply --config "${stack_root}" --yes --concurrency 1 --output json
) >"${OPS_RUN_DIR}/runs/apply-second.jsonl" 2>"${OPS_RUN_DIR}/runs/apply-second.stderr"
second_run_id="$(latest_run_id)"
[[ -n "${second_run_id}" ]] || ops_fail "failed to discover second run ID"
printf '%s\n' "${second_run_id}" >"${OPS_RUN_DIR}/runs/second-run-id.txt"
if [[ "${first_run_id}" == "${second_run_id}" ]]; then
  ops_fail "first and second run IDs unexpectedly match"
fi

ops_log "export explicit first run"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --run-id "${first_run_id}" --out "${explicit_export}"
) >"${OPS_RUN_DIR}/export/explicit-run.out" 2>"${OPS_RUN_DIR}/export/explicit-run.stderr"

ops_log "export default latest run"
(
  cd "${repo_root}"
  ./bin/torque stack export --config "${stack_root}" --out "${latest_export}"
) >"${OPS_RUN_DIR}/export/latest-run.out" 2>"${OPS_RUN_DIR}/export/latest-run.stderr"

ops_log "audit exported bundle read-only"
(
  cd "${repo_root}"
  ./bin/torque stack audit --from-bundle "${explicit_export}" --output json --include-plan --include-events --include-artifacts
) >"${OPS_RUN_DIR}/audit/explicit-from-bundle.json" 2>"${OPS_RUN_DIR}/audit/explicit-from-bundle.stderr"

ops_log "create tampered export bundle"
python3 - "${explicit_export}" "${scratch_root}/tampered" "${tampered_export}" <<'PY'
import shutil
import sys
import tarfile
from pathlib import Path

source = Path(sys.argv[1])
work = Path(sys.argv[2])
out = Path(sys.argv[3])
if work.exists():
    shutil.rmtree(work)
work.mkdir(parents=True)
with tarfile.open(source, "r:gz") as tar:
    tar.extractall(work)
(work / "state.sqlite").write_bytes((work / "state.sqlite").read_bytes() + b"\n# tampered\n")
out.parent.mkdir(parents=True, exist_ok=True)
with tarfile.open(out, "w:gz") as tar:
    tar.add(work / "state.sqlite", arcname="state.sqlite")
    tar.add(work / "manifest.json", arcname="manifest.json")
PY

ops_log "verify tampered export is rejected"
set +e
(
  cd "${repo_root}"
  ./bin/torque stack audit --from-bundle "${tampered_export}" --output json --include-artifacts
) >"${OPS_RUN_DIR}/tamper/tampered-audit.json" 2>"${OPS_RUN_DIR}/tamper/tampered-audit.stderr"
tampered_code=$?
set -e
printf '%s\n' "${tampered_code}" >"${OPS_RUN_DIR}/tamper/tampered-audit.exit"
if [[ "${tampered_code}" -eq 0 ]]; then
  ops_fail "tampered export audit unexpectedly succeeded"
fi
tamper_status="failed"

ops_log "verify export archive contract"
python3 - \
  "${explicit_export}" \
  "${latest_export}" \
  "${OPS_RUN_DIR}/audit/explicit-from-bundle.json" \
  "${OPS_RUN_DIR}/tamper/tampered-audit.stderr" \
  "${first_run_id}" \
  "${second_run_id}" \
  "${OPS_SECRET_CANARY}" \
  "${OPS_RUN_DIR}/verification/ops-cli-006-proof.json" <<'PY'
import hashlib
import json
import shutil
import sqlite3
import sys
import tarfile
import tempfile
from pathlib import Path

explicit_bundle, latest_bundle, audit_path, tampered_stderr, first_run_id, second_run_id, secret, out_path = map(Path, sys.argv[1:9])
first_run_id = str(first_run_id)
second_run_id = str(second_run_id)
secret = str(secret)
errors = []

def run_digest(plan_json: str, summary_json: str, last_event_digest: str) -> str:
    h = hashlib.sha256()
    for part in ("torque.stack-run.v1", plan_json, summary_json, last_event_digest.strip()):
        h.update(part.encode("utf-8"))
        h.update(b"\x00")
    return "sha256:" + h.hexdigest()

def extract(bundle: Path) -> Path:
    tmp = Path(tempfile.mkdtemp(prefix="ops-cli-006-export-"))
    with tarfile.open(bundle, "r:gz") as tar:
        names = sorted(member.name for member in tar.getmembers() if member.isfile())
        if names != ["manifest.json", "state.sqlite"]:
            errors.append(f"{bundle.name} entries are {names}, want manifest.json and state.sqlite")
        tar.extractall(tmp)
    return tmp

def validate_bundle(bundle: Path, want_run_id: str, label: str) -> dict:
    root = extract(bundle)
    try:
        manifest_path = root / "manifest.json"
        state_path = root / "state.sqlite"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        if manifest.get("apiVersion") != "torque.dev/stack-bundle/v1":
            errors.append(f"{label} manifest apiVersion mismatch")
        if manifest.get("kind") != "StackRunBundle":
            errors.append(f"{label} manifest kind mismatch")
        if manifest.get("runId") != want_run_id:
            errors.append(f"{label} manifest runId={manifest.get('runId')} want {want_run_id}")
        if manifest.get("redacted") is not True:
            errors.append(f"{label} manifest redacted flag missing")
        state_sha = hashlib.sha256(state_path.read_bytes()).hexdigest()
        if manifest.get("stateSha256") != state_sha:
            errors.append(f"{label} stateSha256 mismatch")

        conn = sqlite3.connect(state_path)
        try:
            runs = conn.execute("SELECT run_id, status, plan_json, summary_json, last_event_digest, run_digest FROM torque_stack_runs").fetchall()
            if len(runs) != 1:
                errors.append(f"{label} exported run count={len(runs)}, want 1")
                return manifest
            run_id, status, plan_json, summary_json, last_event_digest, stored_digest = runs[0]
            if run_id != want_run_id:
                errors.append(f"{label} sqlite runId={run_id} want {want_run_id}")
            if status != "succeeded":
                errors.append(f"{label} run status={status}, want succeeded")
            expected_digest = run_digest(plan_json, summary_json, last_event_digest)
            if stored_digest != expected_digest:
                errors.append(f"{label} run_digest={stored_digest} want {expected_digest}")
            if manifest.get("runDigest") != stored_digest:
                errors.append(f"{label} manifest runDigest does not match sqlite run_digest")

            for table in ("torque_stack_nodes", "torque_stack_node_steps", "torque_stack_run_artifacts", "torque_stack_events"):
                distinct = conn.execute(f"SELECT COUNT(DISTINCT run_id) FROM {table}").fetchone()[0]
                if distinct != 1:
                    errors.append(f"{label} {table} distinct run count={distinct}, want 1")
                wrong = conn.execute(f"SELECT COUNT(*) FROM {table} WHERE run_id != ?", (want_run_id,)).fetchone()[0]
                if wrong:
                    errors.append(f"{label} {table} has {wrong} rows for another run")

            text_parts = [plan_json, summary_json]
            text_parts.extend(row[0] or "" for row in conn.execute("SELECT body_text FROM torque_stack_run_artifacts"))
            text_parts.extend((row[0] or "") + "\n" + (row[1] or "") + "\n" + (row[2] or "") for row in conn.execute("SELECT message, fields_json, error_message FROM torque_stack_events"))
            exported_text = "\n".join(text_parts)
            if secret in exported_text:
                errors.append(f"{label} exported SQLite leaked OPS secret canary")
            if "password=[REDACTED]" not in exported_text:
                errors.append(f"{label} exported SQLite missing redacted password evidence")
            execute_body = conn.execute("SELECT body_text FROM torque_stack_run_artifacts WHERE artifact_name = 'host-command-execute.json'").fetchone()
            verify_body = conn.execute("SELECT body_text FROM torque_stack_run_artifacts WHERE artifact_name = 'host-command-verify.json'").fetchone()
            if not execute_body:
                errors.append(f"{label} missing host-command-execute.json")
            elif secret in execute_body[0] or "password=[REDACTED]" not in execute_body[0]:
                errors.append(f"{label} execute receipt was not redacted")
            if not verify_body:
                errors.append(f"{label} missing host-command-verify.json")
            elif '"stdoutRedacted": true' not in verify_body[0] or '"noSensitiveKeyValues": true' not in verify_body[0]:
                errors.append(f"{label} verify receipt missing redaction proof")
        finally:
            conn.close()
        return manifest
    finally:
        shutil.rmtree(root)

explicit_manifest = validate_bundle(explicit_bundle, first_run_id, "explicit")
latest_manifest = validate_bundle(latest_bundle, second_run_id, "latest")
if explicit_manifest.get("runId") == latest_manifest.get("runId"):
    errors.append("explicit and latest exports used the same run ID")

try:
    audit = json.loads(audit_path.read_text(encoding="utf-8"))
except Exception as exc:
    audit = {}
    errors.append(f"audit JSON invalid: {exc}")
if audit.get("runId") != first_run_id:
    errors.append(f"bundle audit runId={audit.get('runId')} want {first_run_id}")
integrity = audit.get("integrity") or {}
if integrity.get("eventsOk") is not True:
    errors.append("bundle audit event integrity failed")
if integrity.get("runDigestOk") is not True:
    errors.append("bundle audit run digest integrity failed")
if audit.get("status") != "succeeded":
    errors.append(f"bundle audit status={audit.get('status')}, want succeeded")

tamper_text = tampered_stderr.read_text(encoding="utf-8", errors="replace")
if "state.sqlite sha256 mismatch" not in tamper_text:
    errors.append("tampered export did not fail with state.sqlite sha256 mismatch")

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsCLI006Proof",
    "status": "succeeded" if not errors else "failed",
    "errors": errors,
    "explicitRunId": first_run_id,
    "latestRunId": second_run_id,
    "explicitManifest": explicit_manifest,
    "latestManifest": latest_manifest,
    "auditIntegrity": integrity,
    "tamperRejected": "state.sqlite sha256 mismatch" in tamper_text,
}
out_path.parent.mkdir(parents=True, exist_ok=True)
out_path.write_text(json.dumps(doc, indent=2, sort_keys=True) + "\n", encoding="utf-8")
if errors:
    raise SystemExit("; ".join(errors))
PY
export_status="passed"

if [[ "${cleanup_enabled}" == "1" ]]; then
  ops_log "delete local stack fixture"
  (
    cd "${repo_root}"
    ./bin/torque stack delete --config "${stack_root}" --yes --concurrency 1 --output json
  ) >"${OPS_RUN_DIR}/cleanup/delete.jsonl" 2>"${OPS_RUN_DIR}/cleanup/delete.stderr"
  if [[ -e "${marker_root}" ]]; then
    ops_fail "cleanup marker root still exists: ${marker_root}"
  fi
  cleanup_status="succeeded"
else
  cleanup_status="skipped"
fi
