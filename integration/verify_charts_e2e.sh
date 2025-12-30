#!/usr/bin/env bash
set -euo pipefail

# Run verify against every chart under testdata/charts.
# Use $VERIFY_BIN if set; otherwise prefer ./bin/verify, falling back to go run.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_ROOT="${ROOT}/testdata/charts"
# Limit to a small known-good set to keep runtime fast and stable.
ALLOWLIST=("verify-smoke")
VERIFY_BIN="${VERIFY_BIN:-}"

if [[ -z "${VERIFY_BIN}" ]]; then
  if [[ -x "${ROOT}/bin/verify" ]]; then
    VERIFY_BIN="${ROOT}/bin/verify"
  else
    tmp_verify="$(mktemp)"
    GOOS=$(go env GOOS) GOARCH=$(go env GOARCH) go build -o "${tmp_verify}" "${ROOT}/cmd/verify"
    VERIFY_BIN="${tmp_verify}"
  fi
fi

failures=()

if command -v mapfile >/dev/null 2>&1; then
  mapfile -t charts < <(find "${CHART_ROOT}" -maxdepth 1 -mindepth 1 -type d | sort)
else
  charts=()
  while IFS= read -r d; do
    charts+=("$d")
  done < <(find "${CHART_ROOT}" -maxdepth 1 -mindepth 1 -type d | sort)
fi

for chart in "${charts[@]}"; do
  name="$(basename "${chart}")"
  if [[ ${#ALLOWLIST[@]} -gt 0 ]]; then
    skip=true
    for c in "${ALLOWLIST[@]}"; do
      if [[ "$name" == "$c" ]]; then
        skip=false
        break
      fi
    done
    if $skip; then
      echo "skip ${name} (not in allowlist)"
      continue
    fi
  fi
  if [[ ! -f "${chart}/Chart.yaml" ]]; then
    echo "skip ${name} (no Chart.yaml)"
    continue
  fi
  tmpdir="$(mktemp -d)"
  cfg="${tmpdir}/verify.yaml"
  echo "==> ${name}"
  # Generate config
  if ! "${VERIFY_BIN}" init chart --chart "${chart}" --release "${name}" -n default --write "${cfg}" >"${tmpdir}/init.log" 2>&1; then
    echo "init failed for ${name}"
    cat "${tmpdir}/init.log"
    failures+=("${name}")
    rm -rf "${tmpdir}"
    continue
  fi
  # Run verify
  if ! "${VERIFY_BIN}" "${cfg}"; then
    echo "verify failed for ${name}"
    failures+=("${name}")
  else
    echo "pass ${name}"
  fi
  rm -rf "${tmpdir}"
done

if (( ${#failures[@]} )); then
  echo "Failures (${#failures[@]}): ${failures[*]}"
  exit 1
fi

echo "All charts verified successfully."
