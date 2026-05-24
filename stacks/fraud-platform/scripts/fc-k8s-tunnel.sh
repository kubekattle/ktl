#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: fc-k8s-tunnel.
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
LAB_TARGET="${TORQUE_LAB_SSH:-ssh://root@${TORQUE_LAB_PUBLIC_IP:?set TORQUE_LAB_PUBLIC_IP or TORQUE_LAB_SSH}}"
LAB_TARGET="${LAB_TARGET#ssh://}"
RUN_ROOT="/var/lib/torque-firecracker-k8s/fraud-platform"
SERVER_IP="172.31.250.10"
API_PORT="${TORQUE_FRAUD_API_PORT:-16450}"
KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-/tmp/torque-fraud-platform.kubeconfig}"
CONTROL_PATH="${TORQUE_FRAUD_SSH_CONTROL:-/tmp/torque-fraud-platform.ctl}"
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "${LAB_TARGET}" "cat '${RUN_ROOT}/kubeconfig.yaml'" |
  sed "s#https://${SERVER_IP}:6443#https://127.0.0.1:${API_PORT}#g" >"${KUBECONFIG_PATH}"
ssh -S "${CONTROL_PATH}" -O exit "${LAB_TARGET}" >/dev/null 2>&1 || true
rm -f "${CONTROL_PATH}"
ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ExitOnForwardFailure=yes \
  -M -S "${CONTROL_PATH}" -fN -L "127.0.0.1:${API_PORT}:${SERVER_IP}:6443" "${LAB_TARGET}"
for i in $(seq 1 60); do
  if kubectl --kubeconfig "${KUBECONFIG_PATH}" get nodes >/dev/null 2>&1; then
    kubectl --kubeconfig "${KUBECONFIG_PATH}" get nodes -o wide
    exit 0
  fi
  sleep 2
done
echo "tunnel did not expose Kubernetes API" >&2
exit 1
          ;;
        delete)
set +e
LAB_TARGET="${TORQUE_LAB_SSH:-ssh://root@${TORQUE_LAB_PUBLIC_IP:?set TORQUE_LAB_PUBLIC_IP or TORQUE_LAB_SSH}}"
LAB_TARGET="${LAB_TARGET#ssh://}"
KUBECONFIG_PATH="${TORQUE_FRAUD_KUBECONFIG:-/tmp/torque-fraud-platform.kubeconfig}"
CONTROL_PATH="${TORQUE_FRAUD_SSH_CONTROL:-/tmp/torque-fraud-platform.ctl}"
ssh -S "${CONTROL_PATH}" -O exit "${LAB_TARGET}" >/dev/null 2>&1
rm -f "${CONTROL_PATH}" "${KUBECONFIG_PATH}"
          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
