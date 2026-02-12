#!/usr/bin/env bash
set -euo pipefail

# Generate the static help-ui site under ./site by running the local ktl binary
# and scraping its HTML + JSON endpoints.
#
# This avoids duplicating help-ui template/index logic in another generator.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

OUT_DIR="${OUT_DIR:-site}"
KTL_BIN="${KTL_BIN:-${repo_root}/bin/ktl}"
INCLUDE_ALL="${INCLUDE_ALL:-0}" # set to 1 to include hidden/internal flags + env vars

mkdir -p "${OUT_DIR}"
touch "${OUT_DIR}/.nojekyll"

if [[ ! -x "${KTL_BIN}" ]]; then
  echo ">> building ${KTL_BIN}"
  mkdir -p "$(dirname "${KTL_BIN}")"
  go build -trimpath -buildvcs=false -o "${KTL_BIN}" ./cmd/ktl
fi

port="$(
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
_, port = s.getsockname()
s.close()
print(port)
PY
)"

addr="127.0.0.1:${port}"
base_url="http://${addr}"

args=(help --ui "${addr}")
if [[ "${INCLUDE_ALL}" == "1" ]]; then
  args+=(--all)
fi

echo ">> starting help UI on ${base_url}"
set +e
"${KTL_BIN}" "${args[@]}" >/tmp/ktl-site.log 2>&1 &
pid="$!"
set -e

cleanup() {
  if kill -0 "${pid}" >/dev/null 2>&1; then
    kill "${pid}" >/dev/null 2>&1 || true
    wait "${pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo ">> waiting for ${base_url}/healthz"
for i in $(seq 1 80); do
  if curl -fsS "${base_url}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.05
  if [[ "${i}" -eq 80 ]]; then
    echo "help UI did not become ready; log follows:" >&2
    sed -n '1,200p' /tmp/ktl-site.log >&2 || true
    exit 2
  fi
done

tmp_html="$(mktemp)"
tmp_json="$(mktemp)"

echo ">> fetching HTML + index.json"
curl -fsS "${base_url}/" >"${tmp_html}"
curl -fsS "${base_url}/api/index.json" >"${tmp_json}"

mv "${tmp_html}" "${OUT_DIR}/index.html"
mv "${tmp_json}" "${OUT_DIR}/index.json"

echo ">> wrote:"
ls -la "${OUT_DIR}/index.html" "${OUT_DIR}/index.json" "${OUT_DIR}/.nojekyll" | sed -n '1,200p'

