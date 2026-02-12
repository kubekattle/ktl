#!/usr/bin/env bash
set -euo pipefail

cd /src

OUT_DIR="${OUT_DIR:-/out}"
VERSION="${VERSION:-}"
LDFLAGS="${LDFLAGS:--s -w}"
TARGETOS="${TARGETOS:-linux}"
TARGETARCH="${TARGETARCH:-amd64}"

if [[ -z "${VERSION}" ]]; then
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || true)"
  fi
fi
if [[ -z "${VERSION}" ]]; then
  VERSION="dev"
fi

mkdir -p "${OUT_DIR}"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

bin="${work}/ktl"
echo ">> building ktl ${VERSION} for ${TARGETOS}/${TARGETARCH}"
GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" CGO_ENABLED=0 \
  go build -trimpath -buildvcs=false -ldflags "${LDFLAGS}" -o "${bin}" ./cmd/ktl

root="${work}/root"
install -d "${root}/usr/bin"
install -m 0755 "${bin}" "${root}/usr/bin/ktl"

name="ktl"
maintainer="${MAINTAINER:-ktl maintainers}"
license="${LICENSE:-Apache-2.0}"
url="${URL:-https://github.com/kubekattle/ktl}"
desc="${DESCRIPTION:-ktl: Kubernetes toolbelt (BuildKit builds + Helm apply UI)}"

deb_arch="${TARGETARCH}"
rpm_arch="${TARGETARCH}"
case "${TARGETARCH}" in
  amd64) deb_arch="amd64"; rpm_arch="x86_64" ;;
  arm64) deb_arch="arm64"; rpm_arch="aarch64" ;;
esac

echo ">> packaging deb (${deb_arch})"
fpm -s dir -t deb \
  -n "${name}" \
  -v "${VERSION}" \
  --architecture "${deb_arch}" \
  --maintainer "${maintainer}" \
  --license "${license}" \
  --url "${url}" \
  --description "${desc}" \
  -C "${root}" \
  --package "${OUT_DIR}/${name}_${VERSION}_${deb_arch}.deb" \
  usr/bin/ktl

echo ">> packaging rpm (${rpm_arch})"
fpm -s dir -t rpm \
  -n "${name}" \
  -v "${VERSION}" \
  --architecture "${rpm_arch}" \
  --maintainer "${maintainer}" \
  --license "${license}" \
  --url "${url}" \
  --description "${desc}" \
  -C "${root}" \
  --package "${OUT_DIR}/${name}-${VERSION}-1.${rpm_arch}.rpm" \
  usr/bin/ktl

echo ">> wrote:"
ls -la "${OUT_DIR}" | sed -n '1,200p'
