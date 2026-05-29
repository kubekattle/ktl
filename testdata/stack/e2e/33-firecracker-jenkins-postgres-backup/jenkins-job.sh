#!/usr/bin/env bash
set -euo pipefail

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 2
  }
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
torque_bin="${repo_root}/bin/torque"
base_stack_root="${repo_root}/testdata/stack/e2e/18-firecracker-direct-data-services"
backup_stack_root="${repo_root}/testdata/stack/e2e/33-firecracker-jenkins-postgres-backup"
workspace_root="${WORKSPACE:-${repo_root}}"
evidence_root="${workspace_root}/evidence"
runtime_root="${workspace_root}/${TORQUE_JENKINS_BACKUP_RUNTIME:-runtime}"
base_select=(--release postgres-verify --include-deps)
default_env_file="${backup_stack_root}/jenkins-job.env.example"
env_file="${1:-${TORQUE_JENKINS_ENV_FILE:-}}"

for cmd in pg_isready pg_restore python3 ssh; do
  require_cmd "${cmd}"
done
[[ -x "${torque_bin}" ]] || {
  echo "missing torque binary: ${torque_bin}" >&2
  exit 2
}

if [[ -z "${env_file}" && -f "${default_env_file}" ]]; then
  env_file="${default_env_file}"
fi
if [[ -n "${env_file}" ]]; then
  [[ -f "${env_file}" ]] || {
    echo "missing env file: ${env_file}" >&2
    exit 2
  }
  set -a
  # shellcheck disable=SC1090
  source "${env_file}"
  set +a
fi

export TORQUE_LAB_SSH="${TORQUE_LAB_SSH:-ssh://root@141.105.65.227}"
: "${TORQUE_LAB_SSH_IDENTITY:?TORQUE_LAB_SSH_IDENTITY is required}"
export TORQUE_JENKINS_PG_LOCAL_PORT="${TORQUE_JENKINS_PG_LOCAL_PORT:-15432}"
export TORQUE_JENKINS_PG_CONTROL="${TORQUE_JENKINS_PG_CONTROL:-/tmp/torque-jenkins-pg.ctl}"
export TORQUE_JENKINS_BACKUP_RUNTIME="${TORQUE_JENKINS_BACKUP_RUNTIME:-runtime}"
export TORQUE_JENKINS_SKIP_BASE_STACK="${TORQUE_JENKINS_SKIP_BASE_STACK:-0}"

cleanup() {
  local target control_path
  target="${TORQUE_LAB_SSH#ssh://}"
  control_path="${TORQUE_JENKINS_PG_CONTROL:-/tmp/torque-jenkins-pg.ctl}"
  if [[ -n "${target}" && -e "${control_path}" ]]; then
    ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "${TORQUE_LAB_SSH_IDENTITY}" \
      -S "${control_path}" -O exit "${target}" >/dev/null 2>&1 || true
  fi
  rm -f "${control_path}"
}
trap cleanup EXIT

open_pg_tunnel() {
  local target control_path local_port remote_host
  target="${TORQUE_LAB_SSH#ssh://}"
  control_path="${TORQUE_JENKINS_PG_CONTROL:-/tmp/torque-jenkins-pg.ctl}"
  local_port="${TORQUE_JENKINS_PG_LOCAL_PORT:-15432}"
  remote_host="${TORQUE_FIRECRACKER_PG_PRIMARY_HOST:-172.31.240.10}"
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "${TORQUE_LAB_SSH_IDENTITY}" \
    -S "${control_path}" -O exit "${target}" >/dev/null 2>&1 || true
  rm -f "${control_path}"
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "${TORQUE_LAB_SSH_IDENTITY}" \
    -M -S "${control_path}" -fN -o ExitOnForwardFailure=yes \
    -L "127.0.0.1:${local_port}:${remote_host}:5432" "${target}"
  for _ in $(seq 1 60); do
    if pg_isready -h 127.0.0.1 -p "${local_port}" -U postgres >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "postgres SSH tunnel did not become ready on port ${local_port}" >&2
  exit 1
}

mkdir -p "${evidence_root}"
rm -rf "${runtime_root}"
mkdir -p "${runtime_root}/backups"
open_pg_tunnel

run_id_from_stack() {
  local stack_root="$1"
  shift
  "${torque_bin}" stack runs --config "${stack_root}" "$@" --output json --limit 1 |
    python3 -c 'import json,sys; doc=json.load(sys.stdin); print((doc[0].get("runId") if doc else "") or "")'
}

if [[ "${TORQUE_JENKINS_SKIP_BASE_STACK}" == "1" ]]; then
  echo "reuse existing Firecracker PostgreSQL VM stack"
  base_run_id="preexisting-direct-vms"
  printf '%s\n' "${base_run_id}" >"${evidence_root}/base-run-id.txt"
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "${TORQUE_LAB_SSH_IDENTITY}" \
    "${TORQUE_LAB_SSH#ssh://}" 'cat /var/lib/torque-firecracker-direct/data-services/receipt.json' \
    >"${evidence_root}/base-receipt.json"
  ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -i "${TORQUE_LAB_SSH_IDENTITY}" \
    "${TORQUE_LAB_SSH#ssh://}" 'cat /var/lib/torque-firecracker-direct/data-services/nodes.txt' \
    >"${evidence_root}/base-nodes.txt"
else
  echo "plan base Firecracker PostgreSQL VM stack"
  "${torque_bin}" stack plan --config "${base_stack_root}" "${base_select[@]}" --output json \
    >"${evidence_root}/base-plan.json"

  echo "apply base Firecracker PostgreSQL VM stack"
  "${torque_bin}" stack apply --config "${base_stack_root}" "${base_select[@]}" \
    --yes --concurrency 1 --output json >"${evidence_root}/base-apply.jsonl"
  base_run_id="$(run_id_from_stack "${base_stack_root}" "${base_select[@]}")"
  [[ -n "${base_run_id}" ]] || {
    echo "failed to discover base stack run ID" >&2
    exit 1
  }
  printf '%s\n' "${base_run_id}" >"${evidence_root}/base-run-id.txt"

  echo "audit base Firecracker PostgreSQL VM stack"
  "${torque_bin}" stack audit --config "${base_stack_root}" "${base_select[@]}" \
    --run-id "${base_run_id}" --output json --include-artifacts >"${evidence_root}/base-audit.json"
  "${torque_bin}" stack export --config "${base_stack_root}" "${base_select[@]}" \
    --run-id "${base_run_id}" --out "${evidence_root}/base-export.tgz" \
    >"${evidence_root}/base-export.out" 2>"${evidence_root}/base-export.err"
fi

echo "plan Jenkins backup stack"
"${torque_bin}" stack plan --config "${backup_stack_root}" --output json \
  >"${evidence_root}/backup-plan.json"

echo "apply Jenkins backup stack"
"${torque_bin}" stack apply --config "${backup_stack_root}" \
  --yes --concurrency 1 --output json >"${evidence_root}/backup-apply.jsonl"
backup_run_id="$(run_id_from_stack "${backup_stack_root}")"
[[ -n "${backup_run_id}" ]] || {
  echo "failed to discover backup stack run ID" >&2
  exit 1
}
printf '%s\n' "${backup_run_id}" >"${evidence_root}/backup-run-id.txt"

echo "audit Jenkins backup stack"
"${torque_bin}" stack audit --config "${backup_stack_root}" \
  --run-id "${backup_run_id}" --output json --include-artifacts >"${evidence_root}/backup-audit.json"
"${torque_bin}" stack export --config "${backup_stack_root}" \
  --run-id "${backup_run_id}" --out "${evidence_root}/backup-export.tgz" \
  >"${evidence_root}/backup-export.out" 2>"${evidence_root}/backup-export.err"

cp -R "${runtime_root}/backups" "${evidence_root}/backups"
sha256sum "${runtime_root}/backups/torque-firecracker-direct.dump" \
  >"${evidence_root}/backup-sha256.txt"
pg_restore --list "${runtime_root}/backups/torque-firecracker-direct.dump" \
  >"${evidence_root}/backup-contents.txt"
ls -lh "${runtime_root}/backups" >"${evidence_root}/backup-files.txt"
if [[ -n "${env_file}" ]]; then
  cp "${env_file}" "${evidence_root}/job.env"
fi

python3 - "${evidence_root}/summary.json" "${base_run_id}" "${backup_run_id}" "${runtime_root}/backups/torque-firecracker-direct.manifest.json" <<'PY'
import json
import sys

summary_path, base_run_id, backup_run_id, manifest_path = sys.argv[1:5]
with open(manifest_path, "r", encoding="utf-8") as f:
    manifest = json.load(f)
doc = {
    "jobName": "torque-firecracker-postgres-backup",
    "baseRunId": base_run_id,
    "backupRunId": backup_run_id,
    "backup": {
        "database": manifest.get("database"),
        "file": manifest.get("file"),
        "sha256": manifest.get("sha256"),
        "bytes": manifest.get("bytes"),
        "createdAt": manifest.get("createdAt"),
    },
}
with open(summary_path, "w", encoding="utf-8") as f:
    json.dump(doc, f, indent=2, sort_keys=True)
    f.write("\n")
PY

tar -C "${workspace_root}" -czf "${workspace_root}/torque-firecracker-postgres-backup-evidence.tgz" evidence
