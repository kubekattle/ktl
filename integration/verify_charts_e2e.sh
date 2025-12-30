#!/usr/bin/env bash
set -euo pipefail

# Run verify against every chart under testdata/charts.
# Use $VERIFY_BIN if set; otherwise prefer ./bin/verify, falling back to go run.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHART_ROOT="${ROOT}/testdata/charts"
VERIFY_BIN="${VERIFY_BIN:-}"

if [[ -z "${VERIFY_BIN}" ]]; then
  if [[ -x "${ROOT}/bin/verify" ]]; then
    VERIFY_BIN="${ROOT}/bin/verify"
  else
    VERIFY_BIN="go run ./cmd/verify"
  fi
fi

failures=()

mapfile -t charts < <(find "${CHART_ROOT}" -maxdepth 1 -mindepth 1 -type d | sort)

for chart in "${charts[@]}"; do
  name="$(basename "${chart}")"
  tmpdir="$(mktemp -d)"
  cfg="${tmpdir}/verify.yaml"
  echo "==> ${name}"
  # Generate config
  if ! eval "${VERIFY_BIN}" init chart --chart "\"${chart}\"" --release "\"${name}\"" -n default --write "\"${cfg}\"" >/dev/null; then
    echo "init failed for ${name}"
    failures+=("${name}")
    rm -rf "${tmpdir}"
    continue
  fi
  # Run verify
  if ! eval "${VERIFY_BIN}" "\"${cfg}\""; then
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
