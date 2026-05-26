# Agent Appliance

`torque agent appliance run` builds a local evidence bundle for AI-assisted
engineering work. It is meant for Codex, Claude, OpenCode, CI jobs, and humans
that need the same file-first view of a task before deciding what to change or
what to trust.

The first implementation is an appliance workbench, not a daemon. It collects:

- Repo intelligence: file inventory, Git branch/commit/status, dependency
  manifests, test files, and a simple changed-file to nearby-test impact map.
- API probes: redacted GET captures with status, headers, body preview, hash,
  and timing.
- Browser workbench captures: Playwright screenshots, DOM snapshots, HAR files
  with response bodies omitted, and console logs when Node and Playwright are
  available.
- Command checks: repeatable shell checks run from the repo root with redacted
  stdout/stderr artifacts, hashes, exit code, and timeout status.

## Quick Start

```bash
torque agent appliance run . \
  --actor codex \
  --task "review checkout UI regression" \
  --api-url http://localhost:3000/api/health \
  --browser-url http://localhost:3000/checkout \
  --check "go test ./internal/checkout" \
  --out-dir .torque/agent-appliance/checkout-review
```

The command writes:

- `manifest.json`: top-level verdict, summary, paths, warnings, and evidence
  file hashes.
- `manifest.sha256`: checksum for `manifest.json`.
- `summary.md`: human-readable run summary.
- `repo.json`, `api.json`, `browser.json`, `checks.json`: detailed workbench
  reports.
- `browser/*` and `checks/*`: captured artifacts.

`--format json` prints the manifest to stdout for automation.

## Browser Requirements

Browser capture is best-effort. If `node` or the `playwright` package is not
available from the repository root, browser URLs are recorded as skipped instead
of failing the run. Install Playwright in the target project when screenshots,
DOM snapshots, HAR files, and console logs are required:

```bash
npm install --save-dev playwright
npx playwright install chromium
```

Use `--browser-mode visible` for a visible Chromium session on a host that has a
display. The default is headless.

## Failure Rules

The run fails when an API probe returns a non-2xx/3xx status, a browser capture
that actually ran fails, a command check exits non-zero, or a command times out.
Missing Playwright causes a browser skip warning, not a failure.

Text artifacts are redacted with Torque's default secret rules before being
written. Browser screenshots can still contain page-rendered sensitive data, so
run browser captures only against test sessions or intentionally scoped accounts.
