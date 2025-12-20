#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

ktl_bin="${KTL_BIN:-./bin/ktl}"
policy="${KTL_SANDBOX_CONFIG:-$repo_root/testdata/sandbox/linux-ci.cfg}"
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

need uname
need docker

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$os" != "linux" ]]; then
  red "this demo is intended to run on Linux hosts (got: $os)"
  exit 2
fi

if ! docker info >/dev/null 2>&1; then
  red "docker daemon not reachable; start docker first"
  exit 2
fi

if [[ ! -x "$ktl_bin" ]]; then
  yellow "ktl binary not found at $ktl_bin; building via: make build"
  make build >/dev/null
fi

printf "Using ktl: %s\n" "$ktl_bin" >&2
printf "Using sandbox policy: %s\n" "$policy" >&2

marker_value="ktl-demo-marker-$(date +%s)-$RANDOM"
printf "%s\n" "$marker_value" >"$marker_path"
chmod 0644 "$marker_path"

printf "\n== Demo A: plain docker can read a mounted host file ==\n" >&2
docker run --rm -v /tmp:/host-tmp alpine:3.20 \
  sh -ceu "test -f /host-tmp/$(basename "$marker_path") && echo 'DOCKER:marker_present' && cat /host-tmp/$(basename "$marker_path")"
green "OK: docker container read host marker via explicit volume mount"

printf "\n== Demo B: docker socket implies host-level control (safe check) ==\n" >&2
if [[ -S /var/run/docker.sock ]]; then
  docker run --rm -v /var/run/docker.sock:/var/run/docker.sock docker:28-cli version >/dev/null 2>&1 \
    && green "OK: container can query docker daemon when docker.sock is mounted" \
    || yellow "SKIP: could not query docker daemon from docker:cli (image pull blocked or daemon policy)"
else
  yellow "SKIP: /var/run/docker.sock not present on host"
fi

printf "\n== Demo C: ktl sandbox blocks implicit host visibility during builds ==\n" >&2
printf "(This uses a Dockerfile that *tries* to read a host marker via a host bind-mount. If your builder blocks this feature, the comparison will be SKIP.)\n" >&2

nosb_out="$(KTL_SANDBOX_DISABLE=1 "$ktl_bin" build "$marker_ctx" 2>&1 || true)"
sandbox_out="$(KTL_SANDBOX_CONFIG="$policy" "$ktl_bin" build "$marker_ctx" 2>&1 || true)"

nosb_present=false
sandbox_present=false

if echo "$nosb_out" | rg -q "HOST_MARKER:present" && echo "$nosb_out" | rg -q "$marker_value" ; then
  nosb_present=true
fi
if echo "$sandbox_out" | rg -q "HOST_MARKER:present" && echo "$sandbox_out" | rg -q "$marker_value" ; then
  sandbox_present=true
fi

if [[ "$nosb_present" != true ]]; then
  yellow "SKIP: builder likely blocks host bind mounts; cannot demonstrate host marker read in builds"
  exit 0
fi

if [[ "$sandbox_present" == true ]]; then
  red "FAIL: sandbox still exposed host marker (policy may bind /tmp or builder bypassed sandbox)"
  exit 1
fi

green "PASS: host marker readable without ktl sandbox, but hidden with ktl sandbox"

