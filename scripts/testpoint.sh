#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/testpoint.sh [flags]

Default (no flags):
  - fmt (fixes files)
  - lint
  - unit tests
  - smoke package/verify

Flags:
  --ci                  CI mode: gofmt check (no edits), extra verification, stable logs.
  --no-fmt              Skip formatting step.
  --no-lint             Skip lint step.
  --no-unit             Skip unit tests.
  --no-smoke            Skip smoke package/verify.

  --integration         Run integration tests (tagged) across the repo.
  --charts-e2e          Run verify against allowlisted charts (integration/verify_charts_e2e.sh).
  --e2e-real            Run real-cluster stack verify e2e test (requires env; will fail if missing).

  --race-selected        Run a small race suite (cmd/... + internal/verify/...).
  --matrix-safe          Run a minimal test pass for Go version matrix (unit tests only).

  --json-out <path>     Write go test -json output to <path> (unit tests stage).
  --update-goldens      Set KTL_UPDATE_RULE_GOLDENS=1 (and KTL_UPDATE_GOLDENS=1) for this run.

Environment:
  KTL_UPDATE_RULE_GOLDENS=1 / KTL_UPDATE_GOLDENS=1
    Update snapshots/goldens where supported.

  Real-cluster e2e requires:
    KTL_STACK_VERIFY_E2E_NAMESPACE=...
    KUBECONFIG=...
USAGE
}

die() {
  echo "error: $*" >&2
  exit 2
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

CI_MODE=0
RUN_FMT=1
RUN_LINT=1
RUN_UNIT=1
RUN_SMOKE=1
RUN_INTEGRATION=0
RUN_CHARTS_E2E=0
RUN_E2E_REAL=0
RUN_RACE_SELECTED=0
RUN_MATRIX_SAFE=0
UPDATE_GOLDENS=0
JSON_OUT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --ci)
      CI_MODE=1
      shift
      ;;
    --no-fmt)
      RUN_FMT=0
      shift
      ;;
    --no-lint)
      RUN_LINT=0
      shift
      ;;
    --no-unit)
      RUN_UNIT=0
      shift
      ;;
    --no-smoke)
      RUN_SMOKE=0
      shift
      ;;
    --integration)
      RUN_INTEGRATION=1
      shift
      ;;
    --charts-e2e)
      RUN_CHARTS_E2E=1
      shift
      ;;
    --e2e-real)
      RUN_E2E_REAL=1
      shift
      ;;
    --race-selected)
      RUN_RACE_SELECTED=1
      shift
      ;;
    --matrix-safe)
      RUN_MATRIX_SAFE=1
      shift
      ;;
    --json-out)
      [[ $# -ge 2 ]] || die "--json-out requires a path"
      JSON_OUT="$2"
      shift 2
      ;;
    --update-goldens)
      UPDATE_GOLDENS=1
      shift
      ;;
    *)
      die "unknown flag: $1 (use --help)"
      ;;
  esac
done

if [[ "${UPDATE_GOLDENS}" == "1" ]]; then
  export KTL_UPDATE_RULE_GOLDENS=1
  export KTL_UPDATE_GOLDENS=1
fi

if [[ "${RUN_MATRIX_SAFE}" == "1" ]]; then
  RUN_FMT=0
  RUN_LINT=0
  RUN_SMOKE=0
  RUN_INTEGRATION=0
  RUN_CHARTS_E2E=0
  RUN_E2E_REAL=0
  RUN_RACE_SELECTED=0
fi

echo "ktl testpoint"
echo "  ci:            ${CI_MODE}"
echo "  fmt:           ${RUN_FMT}"
echo "  lint:          ${RUN_LINT}"
echo "  unit:          ${RUN_UNIT}"
echo "  smoke:         ${RUN_SMOKE}"
echo "  integration:   ${RUN_INTEGRATION}"
echo "  charts-e2e:    ${RUN_CHARTS_E2E}"
echo "  e2e-real:      ${RUN_E2E_REAL}"
echo "  race-selected: ${RUN_RACE_SELECTED}"
echo "  json-out:      ${JSON_OUT:-<none>}"
echo

need_cmd go

if [[ "${RUN_FMT}" == "1" ]]; then
  if [[ "${CI_MODE}" == "1" ]]; then
    need_cmd gofmt
    echo ">> gofmt (check)"
    files="$(gofmt -l . || true)"
    if [[ -n "${files}" ]]; then
      echo "gofmt needed on:"
      echo "${files}"
      exit 1
    fi
  else
    echo ">> make fmt"
    make -s fmt
  fi
fi

if [[ "${RUN_LINT}" == "1" ]]; then
  echo ">> make lint"
  make -s lint
fi

if [[ "${CI_MODE}" == "1" ]]; then
  echo ">> go mod verify"
  go mod verify
fi

if [[ "${RUN_UNIT}" == "1" ]]; then
  echo ">> go test ./..."
  if [[ -n "${JSON_OUT}" ]]; then
    mkdir -p "$(dirname "${JSON_OUT}")"
    go test -json ./... >"${JSON_OUT}"
  else
    go test ./...
  fi
fi

if [[ "${RUN_SMOKE}" == "1" ]]; then
  echo ">> smoke package/verify"
  make -s smoke-package-verify
fi

if [[ "${RUN_RACE_SELECTED}" == "1" ]]; then
  echo ">> go test -race (selected)"
  go test -race ./cmd/... ./internal/verify/...
fi

if [[ "${RUN_INTEGRATION}" == "1" ]]; then
  echo ">> go test -tags=integration ./..."
  # Many integration tests are opt-in via env and/or skip when tool deps are missing.
  go test -tags=integration ./...
fi

if [[ "${RUN_CHARTS_E2E}" == "1" ]]; then
  echo ">> charts verify e2e (allowlist)"
  ./integration/verify_charts_e2e.sh
fi

if [[ "${RUN_E2E_REAL}" == "1" ]]; then
  if [[ -z "${KTL_STACK_VERIFY_E2E_NAMESPACE:-}" ]]; then
    die "KTL_STACK_VERIFY_E2E_NAMESPACE is required for --e2e-real"
  fi
  if [[ -z "${KUBECONFIG:-}" ]]; then
    die "KUBECONFIG is required for --e2e-real"
  fi
  echo ">> real-cluster e2e: stack verify"
  go test -tags=integration ./cmd/ktl -run TestStackVerify_E2E_RealCluster -count=1
fi

echo
echo "ok"

