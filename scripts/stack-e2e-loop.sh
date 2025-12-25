#!/usr/bin/env bash
set -euo pipefail

ITERATIONS="${ITERATIONS:-10}"
ROOT="${1:-testdata/stack/smoke/basic}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$HOME/.kube/archimedes.yaml}"
NAMESPACE="${KTL_STACK_SMOKE_NAMESPACE:-ktl-stack-smoke}"

if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
  echo "missing kubeconfig: ${KUBECONFIG_PATH}" >&2
  exit 2
fi

echo "ktl stack e2e loop"
echo "  iterations: ${ITERATIONS}"
echo "  root:       ${ROOT}"
echo "  kubeconfig:  ${KUBECONFIG_PATH}"
echo "  namespace:   ${NAMESPACE}"
echo

failures=0
for ((i=1; i<=ITERATIONS; i++)); do
  echo "===== iteration ${i}/${ITERATIONS} ====="
  if KUBECONFIG_PATH="${KUBECONFIG_PATH}" KTL_STACK_SMOKE_NAMESPACE="${NAMESPACE}" ./scripts/stack-smoke.sh "${ROOT}"; then
    echo "OK iteration ${i}"
  else
    echo "FAIL iteration ${i}" >&2
    failures=$((failures+1))
    echo "Most recent run artifact (if any):" >&2
    find "${ROOT}/.ktl/stack/runs" -maxdepth 1 -type d -print 2>/dev/null | sort | tail -n 3 >&2 || true
    echo >&2
  fi
done

if [[ "${failures}" -gt 0 ]]; then
  echo "${failures}/${ITERATIONS} iterations failed" >&2
  exit 1
fi

echo "All ${ITERATIONS} iterations passed"
