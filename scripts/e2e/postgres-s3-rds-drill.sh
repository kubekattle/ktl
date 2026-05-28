#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/e2e/postgres-s3-rds-drill.sh

Runs the Torque-owned PostgreSQL S3/RDS restore drill stack. The stack creates
a disposable PostgreSQL source container, S3 bucket, and public RDS PostgreSQL
instance, proves backup/restore, and then deletes those resources unless told
to keep them.

Required:
  TORQUE_AWS_RDS_E2E_CONFIRM=1
  AWS credentials accepted by awscli

Optional:
  AWS_REGION=ap-south-1
  WORKDIR=docs/showcase/postgres-s3-rds-drill/runtime
  TORQUE_BIN=/path/to/torque
  TORQUE_AWS_RDS_E2E_KEEP_RESOURCES=1   leave stack-owned resources running
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

[[ "${TORQUE_AWS_RDS_E2E_CONFIRM:-}" == "1" ]] || {
  echo "refusing AWS RDS E2E without TORQUE_AWS_RDS_E2E_CONFIRM=1" >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${repo_root}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require_cmd aws
require_cmd docker
require_cmd make
require_cmd pg_restore
require_cmd psql
require_cmd python3

region="${AWS_REGION:-${AWS_DEFAULT_REGION:-}}"
if [[ -z "${region}" ]]; then
  region="$(aws configure get region 2>/dev/null || true)"
fi
if [[ -z "${region}" ]]; then
  region="us-east-1"
fi
export AWS_REGION="${region}"

make build >/dev/null
torque_bin="${TORQUE_BIN:-${repo_root}/bin/torque}"
workdir="${WORKDIR:-${repo_root}/docs/showcase/postgres-s3-rds-drill/runtime}"
stack_dir="${repo_root}/docs/showcase/postgres-s3-rds-drill"
env_file="${workdir}/manual.env"
keep_resources="${TORQUE_AWS_RDS_E2E_KEEP_RESOURCES:-0}"
mkdir -p "${workdir}/backups" "${workdir}/reports"

cleanup_done=0
cleanup() {
  local code=$?
  trap - EXIT
  if [[ "${keep_resources}" != "1" && "${cleanup_done}" != "1" ]]; then
    "${torque_bin}" stack delete --config "${stack_dir}" --yes >>"${workdir}/stack-delete.log" 2>&1 || true
  fi
  exit "${code}"
}
trap cleanup EXIT

rm -f "${stack_dir}/runtime/backups/keycloak.dump" \
  "${stack_dir}/runtime/backups/keycloak.dump".download.tmp.* \
  "${stack_dir}/runtime/backups/keycloak.s3-upload-session.json"

echo "running Torque-owned stack apply for Postgres backup -> S3 -> RDS restore drill"
"${torque_bin}" stack apply --config "${stack_dir}" --yes >"${workdir}/stack-apply.log" 2>&1
"${torque_bin}" stack audit --config "${stack_dir}" --output json --include-artifacts >"${workdir}/stack-audit.json"
"${torque_bin}" stack audit --config "${stack_dir}" --output html --events -1 --include-artifacts >"${workdir}/reports/stack-audit.html"

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

object_key="${TORQUE_DEMO_S3_PREFIX}/base/keycloak/rds-drill/keycloak.dump"
aws s3api head-object \
  --bucket "${TORQUE_DEMO_S3_BUCKET}" \
  --key "${object_key}" \
  --region "${region}" >"${workdir}/aws-s3-backup-head.json"

restored_count="$(PGPASSWORD="${TORQUE_DEMO_RDS_PASSWORD}" PGSSLMODE=require psql \
  -h "${TORQUE_DEMO_RDS_ENDPOINT}" \
  -p 5432 \
  -U torque_demo \
  -d keycloak_restore_drill \
  -At \
  -c "select count(*) from realm where name = 'torque'")"
if [[ "${restored_count}" != "1" ]]; then
  echo "RDS restore verification failed: expected 1 torque realm, got ${restored_count}" >&2
  exit 1
fi

python3 - "${workdir}/stack-audit.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
names = [a.get("name") for a in doc.get("artifacts", [])]
required = {"postgres-execute.json", "postgres-verify.json", "postgres-resource.json"}
missing = sorted(required - set(names))
if missing:
    raise SystemExit("missing stack audit artifacts: " + ", ".join(missing))
body = "\n".join(a.get("body") or "" for a in doc.get("artifacts", []))
for needle in ["postgresBackupStoreURI", "keycloak_restore_drill", "restore drill verified"]:
    if needle not in body:
        raise SystemExit(f"missing audit proof marker: {needle}")
PY

cat >"${workdir}/summary.txt" <<EOF
postgres_s3_rds_drill_ok
region=${region}
bucket=${TORQUE_DEMO_S3_BUCKET}
s3_key=${object_key}
rds_instance=${TORQUE_DEMO_RDS_INSTANCE}
rds_endpoint=${TORQUE_DEMO_RDS_ENDPOINT}
stack=${stack_dir}/stack.yaml
audit=${workdir}/stack-audit.json
html=${workdir}/reports/stack-audit.html
EOF

echo "postgres_s3_rds_drill_ok region=${region} bucket=${TORQUE_DEMO_S3_BUCKET} rds=${TORQUE_DEMO_RDS_INSTANCE} audit=${workdir}/stack-audit.json"
if [[ "${keep_resources}" == "1" ]]; then
  echo "resources are owned by stack and were kept for manual reruns"
  echo "cleanup command: ${torque_bin} stack delete --config ${stack_dir} --yes"
else
  echo "running Torque-owned stack delete cleanup"
  "${torque_bin}" stack delete --config "${stack_dir}" --yes >"${workdir}/stack-delete.log" 2>&1
  cleanup_done=1
fi
