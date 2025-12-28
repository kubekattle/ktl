#!/usr/bin/env bash
set -euo pipefail

# Real-cluster e2e verification for `ktl stack` verify (Kubernetes-only health gates).
#
# This suite is separate from scripts/stack-e2e-real.sh because it intentionally
# creates real workloads (Deployments/Pods), not just ConfigMaps.
#
# Required:
#   KUBECONFIG_PATH=/path/to/kubeconfig
#   KTL_STACK_VERIFY_E2E_CONFIRM=1
#
# Optional:
#   KUBE_CONTEXT=...
#   KTL_STACK_VERIFY_E2E_NAMESPACE=ktl-stack-verify-e2e
#

ROOT_BASE="${ROOT_BASE:-testdata/stack/verify-e2e-real}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
NAMESPACE="${KTL_STACK_VERIFY_E2E_NAMESPACE:-ktl-stack-verify-e2e}"

if [[ "${KTL_STACK_VERIFY_E2E_CONFIRM:-}" != "1" ]]; then
  echo "Refusing to run without KTL_STACK_VERIFY_E2E_CONFIRM=1" >&2
  echo "This script talks to a real cluster and will create Deployments/Pods." >&2
  exit 2
fi

if [[ -z "${KUBECONFIG_PATH}" ]]; then
  echo "missing KUBECONFIG_PATH" >&2
  exit 2
fi
if [[ ! -f "${KUBECONFIG_PATH}" ]]; then
  echo "missing kubeconfig: ${KUBECONFIG_PATH}" >&2
  exit 2
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "missing kubectl in PATH" >&2
  exit 2
fi

echo "ktl stack verify real-cluster e2e"
echo "  fixtures:    ${ROOT_BASE}"
echo "  kubeconfig:  ${KUBECONFIG_PATH}"
if [[ -n "${KUBE_CONTEXT}" ]]; then
  echo "  context:     ${KUBE_CONTEXT}"
fi
echo "  namespace:   ${NAMESPACE}"
echo

make -s build

kubectl_args=(--kubeconfig "${KUBECONFIG_PATH}")
ktl_args=(--kubeconfig "${KUBECONFIG_PATH}")
if [[ -n "${KUBE_CONTEXT}" ]]; then
  kubectl_args+=(--context "${KUBE_CONTEXT}")
  ktl_args+=(--context "${KUBE_CONTEXT}")
fi

echo ">> ensure namespace ${NAMESPACE}"
kubectl "${kubectl_args[@]}" get ns "${NAMESPACE}" >/dev/null 2>&1 || kubectl "${kubectl_args[@]}" create ns "${NAMESPACE}"

tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/ktl-stack-verify-e2e-real.XXXXXX")"
cleanup() { rm -rf "${tmp_root}"; }
trap cleanup EXIT

copy_fixture_tree() {
  local dst="$1"
  if command -v rsync >/dev/null 2>&1; then
    rsync -a --delete --exclude ".ktl" "${ROOT_BASE}/" "${dst}/"
  else
    mkdir -p "${dst}"
    cp -R "${ROOT_BASE}/." "${dst}/"
    rm -rf "${dst}/"*/.ktl || true
  fi
}

rewrite_fixture_yaml() {
  local root="$1"
  python3 - "${root}" "${KUBECONFIG_PATH}" "${NAMESPACE}" <<'PY'
import os
import sys

root = sys.argv[1]
kubeconfig = sys.argv[2]
namespace = sys.argv[3]

def rewrite(path: str) -> None:
    with open(path, "r", encoding="utf-8") as f:
        lines = f.read().splitlines(True)

    out = []
    for line in lines:
        line = line.replace("kubeconfig: ~/.kube/archimedes.yaml", f"kubeconfig: {kubeconfig}")
        line = line.replace("namespace: ktl-stack-verify-e2e", f"namespace: {namespace}")
        out.append(line)

    with open(path, "w", encoding="utf-8") as f:
        f.write("".join(out))

for base, dirs, files in os.walk(root):
    for name in files:
        if name in ("stack.yaml", "release.yaml"):
            rewrite(os.path.join(base, name))
PY
}

must_fail() {
  local desc="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    echo "expected failure but succeeded: ${desc}" >&2
    return 1
  fi
  return 0
}

echo ">> staging fixtures into temp dir"
work="${tmp_root}/fixtures"
copy_fixture_tree "${work}"
rewrite_fixture_yaml "${work}"

root="${work}/01-deploy-not-ready"

echo ">> plan (${root})"
./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --output table >/dev/null

echo ">> apply expect verify failure (${root})"
must_fail "verify should fail" ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --yes --retry 1

echo ">> status shows verify failed (${root})"
status_raw_out="${root}/.status.raw.jsonl"
./bin/ktl "${ktl_args[@]}" stack status --root "${root}" --format raw --tail 200 >"${status_raw_out}"
rg -n "\"phase\"\\s*:\\s*\"verify\"" "${status_raw_out}" >/dev/null
rg -n "\"status\"\\s*:\\s*\"failed\"" "${status_raw_out}" >/dev/null

echo ">> fix image and re-apply (${root})"
python3 - "${root}" <<'PY'
import os,sys
root=sys.argv[1]
path=os.path.join(root,"stack.yaml")
data=open(path,"r",encoding="utf-8").read()
data=data.replace('image: \"example.invalid/does-not-exist:0\"','image: \"busybox:1\"')
open(path,"w",encoding="utf-8").write(data)
PY

./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --yes --retry 1 >/dev/null

echo ">> delete cleanup (${root})"
./bin/ktl "${ktl_args[@]}" stack delete --root "${root}" --yes --retry 2 >/dev/null

echo "All stack verify e2e checks passed"
