#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-testdata/stack/smoke/basic}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-$HOME/.kube/archimedes.yaml}"
NAMESPACE="${KTL_STACK_SMOKE_NAMESPACE:-ktl-stack-smoke}"

echo ">> build"
make -s build

echo ">> ensure namespace ${NAMESPACE}"
kubectl --kubeconfig "${KUBECONFIG_PATH}" get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl --kubeconfig "${KUBECONFIG_PATH}" create ns "${NAMESPACE}"

echo ">> plan"
./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack plan --root "${ROOT}" --output table

echo ">> graph"
./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack graph --root "${ROOT}" --format dot >/dev/null

echo ">> apply"
./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack apply --root "${ROOT}" --concurrency 2 --yes --retry 2

echo ">> resume (noop)"
./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack apply --root "${ROOT}" --resume --yes --retry 2

echo ">> delete"
./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack delete --root "${ROOT}" --concurrency 2 --yes --retry 2
