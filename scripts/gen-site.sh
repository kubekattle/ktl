#!/usr/bin/env bash
set -euo pipefail

# Generate the static Pages site under ./site by running the local torque binary
# and scraping its HTML + JSON endpoints. The root page is a small landing page;
# the searchable help UI is published as docs.html next to index.json.
#
# This avoids duplicating help-ui template/index logic in another generator.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

OUT_DIR="${OUT_DIR:-site}"
INCLUDE_ALL="${INCLUDE_ALL:-0}" # set to 1 to include hidden/internal flags + env vars
site_bin_dir=""
if [[ -z "${TORQUE_BIN:-}" ]]; then
  site_bin_dir="$(mktemp -d)"
  TORQUE_BIN="${site_bin_dir}/torque"
fi

mkdir -p "${OUT_DIR}"
touch "${OUT_DIR}/.nojekyll"
rm -rf "${OUT_DIR}/assets"
mkdir -p "${OUT_DIR}/assets"

if [[ ! -x "${TORQUE_BIN}" ]]; then
  echo ">> building ${TORQUE_BIN}"
  mkdir -p "$(dirname "${TORQUE_BIN}")"
  go build -trimpath -buildvcs=false -o "${TORQUE_BIN}" ./cmd/torque
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
"${TORQUE_BIN}" "${args[@]}" >/tmp/torque-site.log 2>&1 &
pid="$!"
set -e

cleanup() {
  if kill -0 "${pid}" >/dev/null 2>&1; then
    kill "${pid}" >/dev/null 2>&1 || true
    wait "${pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${site_bin_dir}" ]]; then
    rm -rf "${site_bin_dir}"
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
    sed -n '1,200p' /tmp/torque-site.log >&2 || true
    exit 2
  fi
done

tmp_docs_html="$(mktemp)"
tmp_json="$(mktemp)"

echo ">> fetching HTML + index.json"
curl -fsS "${base_url}/" >"${tmp_docs_html}"
# Prefer /index.json so the fetched HTML works as a static site without an /api/ router.
curl -fsS "${base_url}/index.json" >"${tmp_json}"
python3 - "${tmp_docs_html}" "${TORQUE_SITE_VERSION_LABEL:-}" <<'PY'
import re
import sys
from html import escape

path, version_label = sys.argv[1], sys.argv[2]
with open(path, "r", encoding="utf-8") as fh:
    html = fh.read()
html = re.sub(
    r'(<div class="helpMeta">)(.*?)(</div>)',
    lambda match: match.group(1) + escape(version_label) + match.group(3),
    html,
    count=1,
)
with open(path, "w", encoding="utf-8") as fh:
    fh.write(html)
PY
python3 - "${tmp_json}" "${TORQUE_SITE_GENERATED_AT:-1970-01-01T00:00:00Z}" <<'PY'
import json
import sys

path, generated_at = sys.argv[1], sys.argv[2]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
data["generatedAt"] = generated_at
with open(path, "w", encoding="utf-8") as fh:
    json.dump(data, fh, indent=2)
    fh.write("\n")
PY

install -m 0644 scripts/templates/site_landing.html "${OUT_DIR}/index.html"
install -m 0644 scripts/templates/site_blog_index.html "${OUT_DIR}/blog.html"
install -m 0644 scripts/templates/site_blog_firecracker_fraud_platform.html "${OUT_DIR}/blog-firecracker-fraud-platform.html"
install -m 0644 scripts/templates/site_blog_agentic_proof_gated_change_control.html "${OUT_DIR}/blog-agentic-proof-gated-change-control.html"
install -m 0644 scripts/templates/site_blog_sandbox_cache_docker.html "${OUT_DIR}/blog-sandbox-cache-docker.html"
install -m 0644 scripts/templates/site_blog_mcp_s3_cache.html "${OUT_DIR}/blog-mcp-s3-cache.html"
install -m 0644 scripts/templates/site_blog_atlassian_torque_case_study.html "${OUT_DIR}/blog-atlassian-torque-case-study.html"
install -m 0644 scripts/templates/site_blog_oracle_postgres_k3s_e2e.html "${OUT_DIR}/blog-oracle-postgres-k3s-e2e.html"
mv "${tmp_docs_html}" "${OUT_DIR}/docs.html"
mv "${tmp_json}" "${OUT_DIR}/index.json"
install -m 0644 scripts/install.sh "${OUT_DIR}/install.sh"
mkdir -p "${OUT_DIR}/assets/demos"
install -m 0644 .github/readme/torque-ship.svg "${OUT_DIR}/assets/demos/torque-ship.svg"
install -m 0644 .github/readme/torque-dag.gif "${OUT_DIR}/assets/demos/torque-dag.gif"
install -m 0644 .github/readme/torque-sandbox-secrets.gif "${OUT_DIR}/assets/demos/torque-sandbox-secrets.gif"
install -m 0644 .github/readme/torque-helmer-verifier.gif "${OUT_DIR}/assets/demos/torque-helmer-verifier.gif"
install -m 0644 .github/readme/torque-plan-report.gif "${OUT_DIR}/assets/demos/torque-plan-report.gif"
install -m 0644 .github/readme/torque-dag-performance.gif "${OUT_DIR}/assets/demos/torque-dag-performance.gif"
install -m 0644 .github/readme/torque-logs-capture.gif "${OUT_DIR}/assets/demos/torque-logs-capture.gif"
install -m 0644 .github/readme/torque-agent-mirror.gif "${OUT_DIR}/assets/demos/torque-agent-mirror.gif"
install -m 0644 .github/readme/torque-explain-drilldown.gif "${OUT_DIR}/assets/demos/torque-explain-drilldown.gif"
install -m 0644 .github/readme/torque-secret-safe-evidence.gif "${OUT_DIR}/assets/demos/torque-secret-safe-evidence.gif"
install -m 0644 .github/readme/torque-plan-compare.gif "${OUT_DIR}/assets/demos/torque-plan-compare.gif"
install -m 0644 .github/readme/torque-stack-rerun.gif "${OUT_DIR}/assets/demos/torque-stack-rerun.gif"
mkdir -p "${OUT_DIR}/assets/architecture"
install -m 0644 .github/readme/torque-architecture-secret-path.png "${OUT_DIR}/assets/architecture/torque-architecture-secret-path.png"
install -m 0644 .github/readme/torque-architecture-safety-matrix.png "${OUT_DIR}/assets/architecture/torque-architecture-safety-matrix.png"
mkdir -p "${OUT_DIR}/assets/blog"
install -m 0644 .github/readme/atlassian-devops-tooling.png "${OUT_DIR}/assets/blog/atlassian-devops-tooling.png"
install -m 0644 .github/readme/atlassian-codex-torque.png "${OUT_DIR}/assets/blog/atlassian-codex-torque.png"
install -m 0644 .github/readme/torque-sandbox-venus.png "${OUT_DIR}/assets/blog/torque-sandbox-venus.png"
mkdir -p "${OUT_DIR}/showcase/reports"
install -m 0644 docs/showcase/reports/helmer-plan.html "${OUT_DIR}/showcase/reports/helmer-plan.html"
install -m 0644 docs/showcase/reports/torque-apply-plan.html "${OUT_DIR}/showcase/reports/torque-apply-plan.html"
install -m 0644 docs/showcase/reports/torque-apply-plan.md "${OUT_DIR}/showcase/reports/torque-apply-plan.md"
install -m 0644 docs/showcase/reports/verifier-report.html "${OUT_DIR}/showcase/reports/verifier-report.html"
install -m 0644 docs/showcase/reports/verifier-report.json "${OUT_DIR}/showcase/reports/verifier-report.json"
install -m 0644 docs/showcase/reports/verifier-report.rendered.yaml "${OUT_DIR}/showcase/reports/verifier-report.rendered.yaml"
mkdir -p "${OUT_DIR}/showcase/atlassian/reports"
install -m 0644 docs/showcase/atlassian/*.yaml "${OUT_DIR}/showcase/atlassian/"
install -m 0644 docs/showcase/atlassian/reports/*.html "${OUT_DIR}/showcase/atlassian/reports/"
install -m 0644 docs/showcase/atlassian/reports/*.json "${OUT_DIR}/showcase/atlassian/reports/"
mkdir -p "${OUT_DIR}/showcase/firecracker-fraud-platform"
install -m 0644 testdata/stack/e2e/21-firecracker-fraud-platform/stack.yaml "${OUT_DIR}/showcase/firecracker-fraud-platform/stack.yaml"

echo ">> wrote:"
ls -la "${OUT_DIR}/index.html" "${OUT_DIR}/blog.html" "${OUT_DIR}/blog-firecracker-fraud-platform.html" "${OUT_DIR}/blog-agentic-proof-gated-change-control.html" "${OUT_DIR}/blog-sandbox-cache-docker.html" "${OUT_DIR}/blog-mcp-s3-cache.html" "${OUT_DIR}/blog-atlassian-torque-case-study.html" "${OUT_DIR}/docs.html" "${OUT_DIR}/index.json" "${OUT_DIR}/install.sh" "${OUT_DIR}/.nojekyll" | sed -n '1,200p'
