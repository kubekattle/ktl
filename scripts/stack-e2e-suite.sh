#!/usr/bin/env bash
set -euo pipefail

KUBECONFIG_PATH="${KUBECONFIG_PATH:-$HOME/.kube/archimedes.yaml}"
NAMESPACE="${KTL_STACK_E2E_NAMESPACE:-ktl-stack-e2e}"
ROOT_BASE="${1:-testdata/stack/e2e}"
ITERATIONS="${ITERATIONS:-1}"

if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
  echo "missing kubeconfig: ${KUBECONFIG_PATH}" >&2
  exit 2
fi

echo "ktl stack e2e suite"
echo "  iterations: ${ITERATIONS}"
echo "  fixtures:    ${ROOT_BASE}"
echo "  kubeconfig:  ${KUBECONFIG_PATH}"
echo "  namespace:   ${NAMESPACE}"
echo

make -s build

echo ">> ensure namespace ${NAMESPACE}"
kubectl --kubeconfig "${KUBECONFIG_PATH}" get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl --kubeconfig "${KUBECONFIG_PATH}" create ns "${NAMESPACE}"

run_ok_fixture() {
  local root="$1"
  shift || true
  local -a extra_args=()
  if [[ $# -gt 0 ]]; then
    extra_args=("$@")
  fi
  echo ">> plan (${root})"
  if [[ ${#extra_args[@]} -gt 0 ]]; then
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack plan --root "${root}" --output table "${extra_args[@]}"
  else
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack plan --root "${root}" --output table
  fi
  echo ">> graph (${root})"
  if [[ ${#extra_args[@]} -gt 0 ]]; then
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack graph --root "${root}" --format dot "${extra_args[@]}" >/dev/null
  else
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack graph --root "${root}" --format dot >/dev/null
  fi
  echo ">> apply (${root})"
  if [[ ${#extra_args[@]} -gt 0 ]]; then
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack apply --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2 "${extra_args[@]}"
  else
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack apply --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2
  fi
  echo ">> status table (${root})"
  ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack status --root "${root}" --format table --tail 5 >/dev/null
  echo ">> resume rerun-failed (${root})"
  if [[ ${#extra_args[@]} -gt 0 ]]; then
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack rerun-failed --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2 "${extra_args[@]}"
  else
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack rerun-failed --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2
  fi
  echo ">> delete (${root})"
  if [[ ${#extra_args[@]} -gt 0 ]]; then
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack delete --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2 "${extra_args[@]}"
  else
    ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack delete --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2
  fi
}

run_fail_plan_fixture() {
  local root="$1"
  local name="$2"
  echo ">> plan expect-fail (${name})"
  if ./bin/ktl --kubeconfig "${KUBECONFIG_PATH}" stack plan --root "${root}" --output table >/dev/null 2>&1; then
    echo "expected plan failure but succeeded: ${name}" >&2
    exit 1
  fi
}

fixtures_ok=(
  "01-basic-chain"
  "02-fanout"
  "03-fanin"
  "04-three-wave"
  "05-inline-releases"
  "06-release-files-mix"
  "07-inheritance-overlays"
  "08-tags-selection"
  "09-from-path-selection"
  "10-large-graph"
)

for ((iter=1; iter<=ITERATIONS; iter++)); do
  echo "===== suite iteration ${iter}/${ITERATIONS} ====="
  for f in "${fixtures_ok[@]}"; do
    root="${ROOT_BASE}/${f}"
    echo "==== fixture ${f} ===="
    case "${f}" in
      08-tags-selection)
        run_ok_fixture "${root}" --tag "team-a"
        ;;
      09-from-path-selection)
        run_ok_fixture "${root}" --from-path "apps/"
        ;;
      *)
        run_ok_fixture "${root}"
        ;;
    esac
    echo
  done

  run_fail_plan_fixture "${ROOT_BASE}/x1-cycle-detect" "x1-cycle-detect"
  run_fail_plan_fixture "${ROOT_BASE}/x2-missing-dep" "x2-missing-dep"
done

echo "All fixtures passed (${ITERATIONS} iterations)"
