#!/usr/bin/env bash

ops_fail() {
  echo "error: $*" >&2
  exit 1
}

ops_log() {
  printf '>> %s\n' "$*"
}

ops_require_cmd() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || ops_fail "missing required command: ${cmd}"
}

ops_require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    ops_fail "missing required environment variable: ${name}"
  fi
}

ops_repo_root() {
  local script_dir
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  cd "${script_dir}/../../.." && pwd
}

ops_utc_now() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

ops_new_run_id() {
  date -u +"%Y%m%dT%H%M%SZ"
}

ops_init_run() {
  local task_id="$1"
  ops_require_cmd python3
  ops_require_cmd tar

  OPS_TASK_ID="${task_id}"
  OPS_RUN_ID="${OPS_RUN_ID:-$(ops_new_run_id)-$$}"
  if [[ -z "${OPS_EVIDENCE_ROOT:-}" ]]; then
    OPS_EVIDENCE_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/torque-ops-e2e.XXXXXX")"
  fi
  OPS_RUN_DIR="${OPS_EVIDENCE_ROOT}/${OPS_TASK_ID}-${OPS_RUN_ID}"
  OPS_BUNDLE_PATH="${OPS_EVIDENCE_ROOT}/${OPS_TASK_ID}-${OPS_RUN_ID}.tgz"
  OPS_SECRET_CANARY="${OPS_SECRET_CANARY:-torque-redaction-canary-${OPS_RUN_ID}}"
  mkdir -p "${OPS_RUN_DIR}"
}

ops_write_json_object() {
  local path="$1"
  shift
  mkdir -p "$(dirname "${path}")"
  python3 - "$path" "$@" <<'PY'
import json
import sys

path = sys.argv[1]
doc = {}
for item in sys.argv[2:]:
    key, value = item.split("=", 1)
    if value == "true":
        doc[key] = True
    elif value == "false":
        doc[key] = False
    elif value.isdigit():
        doc[key] = int(value)
    else:
        doc[key] = value

with open(path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
}

ops_redact_stdin() {
  local output="$1"
  mkdir -p "$(dirname "${output}")"
  python3 -c '
import re
import sys

output = sys.argv[1]
canary = sys.argv[2]
text = sys.stdin.read()
text = text.replace(canary, "[REDACTED]")
text = re.sub(
    r"(?i)\b(password|passwd|token|secret)=([^\s\",;]+)",
    lambda m: f"{m.group(1)}=[REDACTED]",
    text,
)
text = re.sub(
    r"(?i)(authorization:\s*bearer\s+)([^\s]+)",
    lambda m: f"{m.group(1)}[REDACTED]",
    text,
)
text = re.sub(r"secret://[^\s\",;]+", "[REDACTED:secret-ref]", text)
with open(output, "w", encoding="utf-8") as f:
    f.write(text)
' "$output" "${OPS_SECRET_CANARY}"
}

ops_scan_for_secret_material() {
  local root="$1"
  local report="$2"
  mkdir -p "$(dirname "${report}")"
  python3 - "$root" "$report" "${OPS_SECRET_CANARY}" <<'PY'
import json
import os
import sys

root = sys.argv[1]
report = sys.argv[2]
canary = sys.argv[3]
needles = [
    canary,
    "secret://",
    "BEGIN PRIVATE KEY",
    "BEGIN OPENSSH PRIVATE KEY",
    "BEGIN RSA PRIVATE KEY",
]
findings = []
for base, _, files in os.walk(root):
    for name in files:
        path = os.path.join(base, name)
        rel = os.path.relpath(path, root)
        try:
            with open(path, "rb") as f:
                data = f.read()
        except OSError:
            continue
        for needle in needles:
            if needle.encode("utf-8") in data:
                findings.append({"path": rel, "needle": needle})

doc = {
    "status": "passed" if not findings else "failed",
    "secretMaterialFound": bool(findings),
    "findings": findings,
}
with open(report, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
if findings:
    sys.exit(1)
PY
}

ops_write_manifest() {
  local root="$1"
  local manifest="$2"
  mkdir -p "$(dirname "${manifest}")"
  python3 - "$root" "$manifest" "${OPS_TASK_ID}" "${OPS_RUN_ID}" <<'PY'
import hashlib
import json
import os
import sys

root, manifest, task_id, run_id = sys.argv[1:5]
artifacts = []
for base, _, files in os.walk(root):
    for name in files:
        path = os.path.join(base, name)
        rel = os.path.relpath(path, root)
        if rel == os.path.relpath(manifest, root):
            continue
        h = hashlib.sha256()
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(1024 * 1024), b""):
                h.update(chunk)
        artifacts.append(
            {
                "path": rel,
                "sha256": h.hexdigest(),
                "size": os.path.getsize(path),
            }
        )

doc = {
    "apiVersion": "torque.dev/e2e/v1",
    "kind": "OpsLabEvidenceManifest",
    "taskId": task_id,
    "runId": run_id,
    "artifactCount": len(artifacts),
    "artifacts": sorted(artifacts, key=lambda item: item["path"]),
}
with open(manifest, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY
}

ops_export_bundle() {
  local root="$1"
  local bundle="$2"
  local parent
  parent="$(dirname "${root}")"
  tar -C "${parent}" -czf "${bundle}" "$(basename "${root}")"
  test -s "${bundle}" || ops_fail "empty evidence bundle: ${bundle}"
}

ops_validate_evidence_contract() {
  local run_dir="$1"
  local bundle="$2"
  shift 2
  python3 "$(dirname "${BASH_SOURCE[0]}")/validate-evidence.py" \
    --run-dir "${run_dir}" \
    --bundle "${bundle}" \
    "$@"
}

ops_ssh_target() {
  local target="$1"
  printf '%s' "${target#ssh://}"
}

ops_set_ssh_base_args() {
  OPS_SSH_ARGS=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new)
  if [[ -n "${TORQUE_LAB_SSH_IDENTITY:-}" ]]; then
    OPS_SSH_ARGS+=(-i "${TORQUE_LAB_SSH_IDENTITY}")
  fi
  if [[ -n "${TORQUE_LAB_SSH_OPTS:-}" ]]; then
    # shellcheck disable=SC2206
    local extra=(${TORQUE_LAB_SSH_OPTS})
    OPS_SSH_ARGS+=("${extra[@]}")
  fi
}
