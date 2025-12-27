#!/usr/bin/env bash
set -euo pipefail

# Real-cluster end-to-end verification for `ktl stack`.
#
# This script is intentionally "paranoid":
# - It copies fixtures to a temp dir (never writes into repo testdata/)
# - It requires explicit confirmation
# - It uses only safe resources (the fixtures install ConfigMaps only)
#
# Required:
#   KUBECONFIG_PATH=/path/to/kubeconfig
#   KTL_STACK_E2E_CONFIRM=1
#
# Optional:
#   KUBE_CONTEXT=...
#   KTL_STACK_E2E_NAMESPACE=ktl-stack-e2e
#   ITERATIONS=1

ROOT_BASE="${ROOT_BASE:-testdata/stack/e2e}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
NAMESPACE="${KTL_STACK_E2E_NAMESPACE:-ktl-stack-e2e}"
ITERATIONS="${ITERATIONS:-1}"

if [[ "${KTL_STACK_E2E_CONFIRM:-}" != "1" ]]; then
  echo "Refusing to run without KTL_STACK_E2E_CONFIRM=1" >&2
  echo "This script talks to a real cluster and will install/uninstall Helm releases (ConfigMaps only)." >&2
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

echo "ktl stack real-cluster e2e"
echo "  iterations:  ${ITERATIONS}"
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

tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/ktl-stack-e2e-real.XXXXXX")"
cleanup() {
  rm -rf "${tmp_root}"
}
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
        # Normalize the fixtures to the kubeconfig/namespace passed to this script.
        # The fixtures use a compact inline YAML mapping in defaults, so do a simple
        # line-level replacement that's stable for these testdata files.
        line = line.replace("kubeconfig: ~/.kube/archimedes.yaml", f"kubeconfig: {kubeconfig}")
        line = line.replace("namespace: ktl-stack-e2e", f"namespace: {namespace}")
        out.append(line)

    with open(path, "w", encoding="utf-8") as f:
        f.write("".join(out))

for base, dirs, files in os.walk(root):
    for name in files:
        if name in ("stack.yaml", "release.yaml"):
            rewrite(os.path.join(base, name))
PY
}

json_first_node() {
  python3 - <<'PY'
import json
import sys

doc = json.load(sys.stdin)
nodes = doc.get("Nodes") or doc.get("nodes") or []
if not nodes:
    sys.exit(1)
n = nodes[0]
print(n.get("id",""), n.get("name",""))
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

most_recent_run_id() {
  local root="$1"
  ./bin/ktl "${ktl_args[@]}" stack runs --root "${root}" --output json --limit 1 | python3 - <<'PY'
import json
import sys
doc = json.load(sys.stdin)
if not doc:
    sys.exit(1)
print(doc[0].get("runId") or doc[0].get("runID") or doc[0].get("RunID") or "")
PY
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

expected_fail_plan=(
  "x1-cycle-detect"
  "x2-missing-dep"
)

run_ok_fixture() {
  local root="$1"
  shift || true
  local -a extra_args=()
  if [[ $# -gt 0 ]]; then
    extra_args=("$@")
  fi

  echo ">> plan table (${root})"
  ./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --output table "${extra_args[@]}" >/dev/null
  echo ">> plan json (${root})"
  plan_json="$(./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --output json "${extra_args[@]}")"
  echo "${plan_json}" >/dev/null

  echo ">> graph dot (${root})"
  ./bin/ktl "${ktl_args[@]}" stack graph --root "${root}" --format dot "${extra_args[@]}" >/dev/null
  echo ">> graph mermaid (${root})"
  ./bin/ktl "${ktl_args[@]}" stack graph --root "${root}" --format mermaid "${extra_args[@]}" >/dev/null

  echo ">> explain (${root})"
  first_id_and_name="$(printf '%s' "${plan_json}" | json_first_node)"
  first_id="$(printf '%s' "${first_id_and_name}" | awk '{print $1}')"
  first_name="$(printf '%s' "${first_id_and_name}" | awk '{print $2}')"
  ./bin/ktl "${ktl_args[@]}" stack explain --root "${root}" "${first_id}" "${extra_args[@]}" >/dev/null
  ./bin/ktl "${ktl_args[@]}" stack explain --root "${root}" "${first_name}" --why "${extra_args[@]}" >/dev/null

  echo ">> apply dry-run (${root})"
  ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --concurrency 2 --yes --dry-run "${extra_args[@]}" >/dev/null

  echo ">> apply diff (${root})"
  ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2 --diff "${extra_args[@]}" >/dev/null

  echo ">> status raw (${root})"
  ./bin/ktl "${ktl_args[@]}" stack status --root "${root}" --format raw --tail 5 >/dev/null
  echo ">> status table (${root})"
  ./bin/ktl "${ktl_args[@]}" stack status --root "${root}" --format table >/dev/null
  echo ">> status json (${root})"
  ./bin/ktl "${ktl_args[@]}" stack status --root "${root}" --format json >/dev/null

  echo ">> runs list (${root})"
  ./bin/ktl "${ktl_args[@]}" stack runs --root "${root}" --limit 5 >/dev/null

  echo ">> resume rerun-failed (${root})"
  ./bin/ktl "${ktl_args[@]}" stack rerun-failed --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2 "${extra_args[@]}" >/dev/null

  echo ">> seal + apply sealed-dir (${root})"
  sealed_dir="${root}/.sealed"
  rm -rf "${sealed_dir}"
  ./bin/ktl "${ktl_args[@]}" stack seal --root "${root}" --out "${sealed_dir}" --bundle >/dev/null
  ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --sealed-dir "${sealed_dir}" --yes --diff >/dev/null

  echo ">> delete (${root})"
  ./bin/ktl "${ktl_args[@]}" stack delete --root "${root}" --concurrency 4 --progressive-concurrency --yes --retry 2 "${extra_args[@]}" >/dev/null

  echo ">> follow status during apply (${root})"
  (
    ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --concurrency 1 --yes --retry 1 >/dev/null
  ) &
  apply_pid="$!"

  # Wait until the run appears in sqlite and then follow it.
  run_id=""
  for _ in {1..40}; do
    if run_id="$(most_recent_run_id "${root}")" && [[ -n "${run_id}" ]]; then
      break
    fi
    sleep 0.25
  done
  if [[ -z "${run_id}" ]]; then
    echo "failed to discover run id for follow test (${root})" >&2
    kill "${apply_pid}" >/dev/null 2>&1 || true
    wait "${apply_pid}" || true
    exit 1
  fi

  follow_out="${root}/.follow.jsonl"
  rm -f "${follow_out}"
  (
    ./bin/ktl "${ktl_args[@]}" stack status --root "${root}" --run-id "${run_id}" --format raw --follow --tail 5
  ) >"${follow_out}" &
  follow_pid="$!"

  wait "${apply_pid}"

  # Follow does not auto-stop; ensure we observed completion and then stop it.
  for _ in {1..40}; do
    if rg -n "\"type\"\\s*:\\s*\"RUN_COMPLETED\"" "${follow_out}" >/dev/null 2>&1; then
      break
    fi
    sleep 0.25
  done
  kill "${follow_pid}" >/dev/null 2>&1 || true
  wait "${follow_pid}" >/dev/null 2>&1 || true

  echo ">> drift safety on resume (${root})"
  last_run_id="$(most_recent_run_id "${root}")"
  if [[ -n "${last_run_id}" ]]; then
    # Mutate a file that is likely referenced by at least one release values.yaml.
    # This should make resume fail unless --allow-drift is set.
    vals="$(find "${root}" -type f -name values.yaml | head -n 1 || true)"
    if [[ -n "${vals}" ]]; then
      echo "# drift $(date -u +%s)" >> "${vals}"
      must_fail "resume without allow-drift" ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --resume --run-id "${last_run_id}" --yes
      ./bin/ktl "${ktl_args[@]}" stack apply --root "${root}" --resume --run-id "${last_run_id}" --allow-drift --yes --concurrency 1 >/dev/null
    fi
  fi
}

run_expected_fail_plan_fixture() {
  local root="$1"
  local name="$2"
  echo ">> plan expect-fail (${name})"
  must_fail "plan should fail (${name})" ./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --output table
}

echo ">> staging fixtures into temp dir"
work="${tmp_root}/fixtures"
copy_fixture_tree "${work}"
rewrite_fixture_yaml "${work}"

for ((iter=1; iter<=ITERATIONS; iter++)); do
  echo "===== suite iteration ${iter}/${ITERATIONS} ====="

  for f in "${fixtures_ok[@]}"; do
    root="${work}/${f}"
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

  for f in "${expected_fail_plan[@]}"; do
    run_expected_fail_plan_fixture "${work}/${f}" "${f}"
  done

  echo ">> allow-missing-deps behavior"
  root="${work}/01-basic-chain"
  must_fail "missing selected deps should fail" ./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --release "e2e01-dependent"
  ./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --release "e2e01-dependent" --allow-missing-deps >/dev/null

  echo ">> include-deps/include-dependents behavior"
  ./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --release "e2e01-dependent" --include-deps >/dev/null
  ./bin/ktl "${ktl_args[@]}" stack plan --root "${root}" --release "e2e01-base" --include-dependents >/dev/null

  echo
done

echo "All stack real-cluster e2e checks passed (${ITERATIONS} iterations)"

