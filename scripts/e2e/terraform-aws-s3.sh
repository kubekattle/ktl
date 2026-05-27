#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/e2e/terraform-aws-s3.sh

Creates one unique AWS S3 bucket through the Torque Terraform/OpenTofu adapter,
verifies stack audit evidence, then destroys the bucket through stack delete.

Required:
  TORQUE_AWS_E2E_CONFIRM=1
  AWS credentials accepted by the hashicorp/aws provider

Optional:
  AWS_REGION=us-east-1
  TORQUE_TERRAFORM_BIN=tofu|terraform|/path/to/bin
  TORQUE_AWS_PROVIDER_VERSION=">= 5.0"
  WORKDIR=/tmp/torque-terraform-aws-s3
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

[[ "${TORQUE_AWS_E2E_CONFIRM:-}" == "1" ]] || {
  echo "refusing AWS E2E without TORQUE_AWS_E2E_CONFIRM=1" >&2
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

require_cmd make
require_cmd python3

if [[ -n "${TORQUE_TERRAFORM_BIN:-}" ]]; then
  tf_bin="${TORQUE_TERRAFORM_BIN}"
elif command -v tofu >/dev/null 2>&1; then
  tf_bin="$(command -v tofu)"
elif command -v terraform >/dev/null 2>&1; then
  tf_bin="$(command -v terraform)"
else
  echo "missing OpenTofu/Terraform binary; set TORQUE_TERRAFORM_BIN" >&2
  exit 1
fi

make build >/dev/null
torque_bin="${TORQUE_BIN:-${repo_root}/bin/torque}"
region="${AWS_REGION:-${AWS_DEFAULT_REGION:-us-east-1}}"
provider_version="${TORQUE_AWS_PROVIDER_VERSION:->= 5.0}"
workdir="${WORKDIR:-$(mktemp -d "${TMPDIR:-/tmp}/torque-terraform-aws-s3.XXXXXX")}"
mkdir -p "${workdir}"

suffix="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(6))
PY
)"
bucket="torque-tf-e2e-${suffix}"
stack_dir="${workdir}/stack"
mkdir -p "${stack_dir}"
applied=0
deleted=0

cleanup() {
  local code=$?
  trap - EXIT
  if [[ "${deleted}" != "1" && -f "${stack_dir}/stack.yaml" ]]; then
    TORQUE_TERRAFORM_BIN="${tf_bin}" "${torque_bin}" stack delete --config "${stack_dir}" --yes >"${workdir}/cleanup-delete.log" 2>&1 || true
  fi
  local tf_work="${stack_dir}/.torque/terraform/local-default-aws-s3-bucket"
  if [[ "${applied}" == "1" && -d "${tf_work}" ]]; then
    "${tf_bin}" -chdir="${tf_work}" destroy -auto-approve -input=false -no-color >"${workdir}/cleanup-terraform-destroy.log" 2>&1 || true
  fi
  exit "${code}"
}
trap cleanup EXIT

cat > "${stack_dir}/stack.yaml" <<YAML
apiVersion: torque.dev/v1
kind: Stack
name: terraform-aws-s3-e2e
defaults:
  cluster:
    name: local
nodes:
  - name: aws-s3-bucket
    kind: aws.s3.bucket.ensure
    module:
      source: oci://ghcr.io/torque-modules/terraform-aws
      version: 0.1.0
      command: ["${torque_bin}", "terraform-adapter", "--terraform-bin", "${tf_bin}"]
      timeout: 30m
      input:
        provider:
          source: hashicorp/aws
          version: "${provider_version}"
          localName: aws
          config:
            region: "${region}"
        resource:
          type: aws_s3_bucket
          name: this
          values:
            bucket: "${bucket}"
            force_destroy: true
            tags:
              managed-by: torque
              torque-e2e: terraform-aws-s3
YAML

TORQUE_TERRAFORM_BIN="${tf_bin}" "${torque_bin}" stack plan --config "${stack_dir}" --output json >"${workdir}/stack-plan.json"
applied=1
TORQUE_TERRAFORM_BIN="${tf_bin}" "${torque_bin}" stack apply --config "${stack_dir}" --yes >"${workdir}/stack-apply.log" 2>&1
TORQUE_TERRAFORM_BIN="${tf_bin}" "${torque_bin}" stack audit --config "${stack_dir}" --output json --include-artifacts >"${workdir}/stack-apply-audit.json"

python3 - "${workdir}/stack-apply-audit.json" <<'PY'
import json, sys
raw = open(sys.argv[1], encoding="utf-8").read()
doc = json.loads(raw)
names = {artifact.get("name") for artifact in doc.get("artifacts", [])}
required = [
    "module-plan.json",
    "module-apply.json",
    "module-verify.json",
    "terraform-plan-summary.json",
    "terraform-plan-metadata.json",
    "terraform-state-summary.json",
]
missing = [item for item in required if item not in names]
if missing:
    raise SystemExit(f"missing apply audit artifact(s): {', '.join(missing)}")
PY

TORQUE_TERRAFORM_BIN="${tf_bin}" "${torque_bin}" stack delete --config "${stack_dir}" --yes >"${workdir}/stack-delete.log" 2>&1
deleted=1
TORQUE_TERRAFORM_BIN="${tf_bin}" "${torque_bin}" stack audit --config "${stack_dir}" --output json --include-artifacts >"${workdir}/stack-delete-audit.json"

python3 - "${workdir}/stack-delete-audit.json" <<'PY'
import json, sys
raw = open(sys.argv[1], encoding="utf-8").read()
doc = json.loads(raw)
names = {artifact.get("name") for artifact in doc.get("artifacts", [])}
required = [
    "module-plan.json",
    "module-delete.json",
    "module-verify.json",
    "terraform-plan-summary.json",
    "terraform-state-summary.json",
]
missing = [item for item in required if item not in names]
if missing:
    raise SystemExit(f"missing delete audit artifact(s): {', '.join(missing)}")
state_zero = False
for artifact in doc.get("artifacts", []):
    if artifact.get("name") != "terraform-state-summary.json":
        continue
    body = json.loads(artifact.get("body") or "{}")
    value = body.get("value") or {}
    if value.get("resources") == 0:
        state_zero = True
if not state_zero:
    raise SystemExit("delete audit did not prove empty Terraform state")
PY

if command -v aws >/dev/null 2>&1; then
  if aws s3api head-bucket --bucket "${bucket}" >/dev/null 2>&1; then
    echo "bucket still exists after stack delete: ${bucket}" >&2
    exit 1
  fi
fi

trap - EXIT
echo "terraform_aws_s3_e2e_ok bucket=${bucket} region=${region} workdir=${workdir}"
