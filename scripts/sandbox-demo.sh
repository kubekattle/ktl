#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

ktl_bin="${KTL_BIN:-./bin/ktl}"
policy="${KTL_SANDBOX_CONFIG:-$repo_root/testdata/sandbox/linux-ci.cfg}"

baseline_ctx="$repo_root/testdata/sandbox-demo/baseline"
marker_ctx="$repo_root/testdata/sandbox-demo/host-marker"
marker_path="${MARKER_PATH:-/tmp/ktl-sandbox-demo-host-marker.txt}"

red() { printf "\033[31m%s\033[0m\n" "$*"; }
green() { printf "\033[32m%s\033[0m\n" "$*"; }
yellow() { printf "\033[33m%s\033[0m\n" "$*"; }

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    red "missing required command: $1"
    exit 2
  fi
}

need go
need uname

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$os" != "linux" ]]; then
  red "this demo is intended to run on Linux hosts (got: $os)"
  exit 2
fi

if [[ ! -x "$ktl_bin" ]]; then
  yellow "ktl binary not found at $ktl_bin; building via: make build"
  make build >/dev/null
fi

if ! command -v nsjail >/dev/null 2>&1; then
  red "nsjail not found; sandbox demo requires nsjail on the host"
  exit 2
fi

if [[ ! -f "$policy" ]]; then
  red "sandbox policy not found: $policy"
  exit 2
fi

run_build() {
  local label="$1"
  shift
  printf "\n== %s ==\n" "$label" >&2
  "$ktl_bin" build "$@"
}

pass_count=0
fail_count=0
skip_count=0

note_pass() { green "PASS: $*"; pass_count=$((pass_count+1)); }
note_fail() { red "FAIL: $*"; fail_count=$((fail_count+1)); }
note_skip() { yellow "SKIP: $*"; skip_count=$((skip_count+1)); }

printf "Using ktl: %s\n" "$ktl_bin" >&2
printf "Using sandbox policy: %s\n" "$policy" >&2

printf "\n-- Test 0: sandbox baseline produces output\n" >&2
baseline_out="$("$ktl_bin" build "$baseline_ctx" 2>&1 || true)"
if echo "$baseline_out" | rg -q "Running ktl build inside the sandbox" ; then
  note_pass "sandbox banner printed"
else
  note_fail "missing sandbox banner"
fi
if echo "$baseline_out" | rg -q "baseline-ok" ; then
  note_pass "baseline build ran"
else
  note_fail "baseline build did not run (no 'baseline-ok' in output)"
fi

printf "\n-- Test 1: disable sandbox still builds\n" >&2
nosb_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$baseline_ctx" 2>&1 || true)"
if echo "$nosb_out" | rg -q "Running ktl build inside the sandbox" ; then
  note_fail "unexpected sandbox banner with KTL_SANDBOX_DISABLE=1"
else
  note_pass "no sandbox banner when disabled"
fi
if echo "$nosb_out" | rg -q "baseline-ok" ; then
  note_pass "baseline build ran without sandbox"
else
  note_fail "baseline build did not run without sandbox"
fi

printf "\n-- Test 2: host-marker visibility differs (builder-permissive only)\n" >&2
marker_value="ktl-demo-marker-$(date +%s)-$RANDOM"
printf "%s\n" "$marker_value" >"$marker_path"
chmod 0644 "$marker_path"

nosb_marker_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$marker_ctx" 2>&1 || true)"
sandbox_marker_out="$(KTL_SANDBOX_CONFIG="$policy" "$ktl_bin" build "$marker_ctx" 2>&1 || true)"

nosb_present=false
sandbox_present=false

if echo "$nosb_marker_out" | rg -q "HOST_MARKER:present" && echo "$nosb_marker_out" | rg -q "$marker_value" ; then
  nosb_present=true
fi
if echo "$sandbox_marker_out" | rg -q "HOST_MARKER:present" && echo "$sandbox_marker_out" | rg -q "$marker_value" ; then
  sandbox_present=true
fi

if [[ "$nosb_present" != true ]]; then
  note_skip "builder likely blocks host bind mounts; cannot demonstrate host marker read without sandbox"
elif [[ "$sandbox_present" == true ]]; then
  note_fail "sandbox still exposed host marker (policy likely binds /tmp or builder bypassed sandbox)"
else
  note_pass "host marker visible without sandbox and hidden with sandbox"
fi

printf "\n-- Test 3: sandbox runtime failures are visible via --sandbox-logs\n" >&2
bad_policy="$repo_root/testdata/sandbox/does-not-exist.cfg"
logs_out="$(KTL_SANDBOX_CONFIG="$bad_policy" "$ktl_bin" build "$baseline_ctx" --sandbox-logs 2>&1 || true)"
if echo "$logs_out" | rg -q "sandbox config:" ; then
  note_pass "invalid policy reported"
else
  note_fail "invalid policy not reported (expected sandbox config error)"
fi

printf "\nSummary: %d passed, %d failed, %d skipped\n" "$pass_count" "$fail_count" "$skip_count" >&2
if [[ "$fail_count" -gt 0 ]]; then
  exit 1
fi
