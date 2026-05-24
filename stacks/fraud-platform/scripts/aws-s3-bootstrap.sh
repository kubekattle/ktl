#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: aws-s3-bootstrap.
# Keep runtime differences in environment/profile values, not by editing evidence output.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export TORQUE_STACK_ROOT="${TORQUE_STACK_ROOT:-${STACK_DIR}}"
export TORQUE_FRAUD_PROFILE="${TORQUE_FRAUD_PROFILE:-${TORQUE_STACK_PROFILE:-lab}}"
mode="${1:-apply}"
      case "${mode}" in
        apply)
set -euo pipefail
KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-/tmp/torque-fraud-platform.kubeconfig}"
if [[ "${TORQUE_FRAUD_PROFILE}" == "prod" || "${TORQUE_FRAUD_AWS_SECRET_MODE:-}" == "existing" ]]; then
  for ns in apps data stream ml argo observability; do
    kubectl --kubeconfig "${KUBECONFIG_PATH}" create namespace "${ns}" --dry-run=client -o yaml |
      kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f -
  done
  for ns in apps argo ml data; do
    kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${ns}" get secret aws-s3 >/dev/null
  done
  echo "using existing aws-s3 secrets for profile=${TORQUE_FRAUD_PROFILE}"
  exit 0
fi
region="${AWS_REGION:-$(aws configure get region)}"
region="${region:-us-east-1}"
account="$(aws sts get-caller-identity --query Account --output text)"
bucket="${TORQUE_FRAUD_S3_BUCKET:-torque-fraud-demo-${account}-${region}}"
if ! aws s3api head-bucket --bucket "${bucket}" >/dev/null 2>&1; then
  if [[ "${region}" == "us-east-1" ]]; then
    aws s3api create-bucket --bucket "${bucket}" --region "${region}" >/dev/null
  else
    aws s3api create-bucket --bucket "${bucket}" --region "${region}" --create-bucket-configuration LocationConstraint="${region}" >/dev/null
  fi
fi
aws s3api put-bucket-versioning --bucket "${bucket}" --versioning-configuration Status=Enabled >/dev/null || true
tmp="$(mktemp)"
trap 'rm -f "${tmp}"' EXIT
aws configure export-credentials --format env-no-export >"${tmp}"
set -a
# shellcheck disable=SC1090
. "${tmp}"
set +a
for ns in apps data stream ml argo observability; do
  kubectl --kubeconfig "${KUBECONFIG_PATH}" create namespace "${ns}" --dry-run=client -o yaml |
    kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f -
done
create_aws_secret() {
  ns="$1"
  args=(
    --from-literal="S3_BUCKET=${bucket}"
    --from-literal="AWS_DEFAULT_REGION=${region}"
    --from-literal="AWS_REGION=${region}"
    --from-literal="AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}"
    --from-literal="AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}"
  )
  if [[ -n "${AWS_SESSION_TOKEN:-}" ]]; then
    args+=(--from-literal="AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN}")
  fi
  kubectl --kubeconfig "${KUBECONFIG_PATH}" -n "${ns}" create secret generic aws-s3 "${args[@]}" --dry-run=client -o yaml |
    kubectl --kubeconfig "${KUBECONFIG_PATH}" apply -f -
}
for ns in apps argo ml data; do
  create_aws_secret "${ns}"
done
printf 'S3_BUCKET=%s\nAWS_REGION=%s\n' "${bucket}" "${region}" > /tmp/torque-fraud-s3.env
echo "s3_bucket=${bucket}"
          ;;
        delete)
true          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
