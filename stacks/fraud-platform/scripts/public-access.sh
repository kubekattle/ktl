#!/usr/bin/env bash
# Generated from the original fraud-platform Torque stack node: public-access.
# Keep runtime differences in environment/profile values, not by editing evidence output.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACK_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
export TORQUE_STACK_ROOT="${TORQUE_STACK_ROOT:-${STACK_DIR}}"
export TORQUE_FRAUD_PROFILE="${TORQUE_FRAUD_PROFILE:-${TORQUE_STACK_PROFILE:-lab}}"
mode="${1:-apply}"
      LAB_TARGET="${TORQUE_LAB_SSH:-ssh://root@${TORQUE_LAB_PUBLIC_IP:?set TORQUE_LAB_PUBLIC_IP or TORQUE_LAB_SSH}}"
      LAB_TARGET="${LAB_TARGET#ssh://}"
      case "${mode}" in
        apply)
          ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "${LAB_TARGET}" 'bash -s' <<'TORQUE_REMOTE_COMMAND'
set -euo pipefail
RUN_ROOT="/var/lib/torque-firecracker-k8s/fraud-platform"
RUN_ROOT="${RUN_ROOT}" "${RUN_ROOT}/fraud-k3s-lab.sh" public-apply
TORQUE_REMOTE_COMMAND
          ;;
        delete)
          ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new "${LAB_TARGET}" 'bash -s' <<'TORQUE_REMOTE_DELETE'
set +e
RUN_ROOT="/var/lib/torque-firecracker-k8s/fraud-platform"
if [ -x "${RUN_ROOT}/fraud-k3s-lab.sh" ]; then
  RUN_ROOT="${RUN_ROOT}" "${RUN_ROOT}/fraud-k3s-lab.sh" public-delete
fi
TORQUE_REMOTE_DELETE
          ;;
        *) echo "unknown mode: ${mode}" >&2; exit 2 ;;
      esac
