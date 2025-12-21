#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-root@188.124.37.233}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_DIR="${REMOTE_DIR:-/root/ktl}"
SANDBOX_CFG="${SANDBOX_CFG:-${REMOTE_DIR}/testdata/sandbox/linux-ci.cfg}"

echo "==> Building linux/amd64 ktl locally"
(cd "${REPO_ROOT}" && GOOS=linux GOARCH=amd64 go build -ldflags '-s -w' -o /tmp/ktl-linux-amd64 ./cmd/ktl)

echo "==> Syncing repo to ${HOST}:${REMOTE_DIR}"
rsync -az --delete \
  --exclude '.git' \
  --exclude 'bin/' \
  --exclude 'dist/' \
  --exclude '**/node_modules' \
  "${REPO_ROOT}/" "${HOST}:${REMOTE_DIR}/"

echo "==> Uploading ktl binary"
ssh "${HOST}" mkdir -p "${REMOTE_DIR}/bin"
scp /tmp/ktl-linux-amd64 "${HOST}:${REMOTE_DIR}/bin/ktl"
ssh "${HOST}" chmod +x "${REMOTE_DIR}/bin/ktl"

echo "==> Dockerfile fixture (sandbox)"
ssh "${HOST}" "${REMOTE_DIR}/bin/ktl" build "${REMOTE_DIR}/testdata/build/dockerfiles/basic" --no-cache --sandbox --sandbox-config "${SANDBOX_CFG}" >/dev/null

echo "==> Compose fixture (sandbox)"
ssh "${HOST}" "${REMOTE_DIR}/bin/ktl" build "${REMOTE_DIR}/testdata/build/compose" --no-cache --sandbox --sandbox-config "${SANDBOX_CFG}" >/dev/null

echo "OK"
