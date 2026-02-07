# AGENTS Playbook

One-page guardrails for anyone touching `ktl`: start here before coding, keep it open while you work, and update it first when the ground rules change.

## How To Use This File
- Read **Quick Checklist** when you pick up a task; everything else dives deeper.
- Follow the **Day-to-Day Workflow** steps in order (pre-flight → coding → PR).
- Reference the section headers when your reviewer asks “did you run X?” or “what’s the design spec?”
- When guidance changes, edit this document and point to the exact subsection in your PR/issue description.

## Quick Checklist
1. Confirm you understand the repo layout and that changes stay inside the right module (`cmd/` wiring vs `internal/` logic vs `testdata/` fixtures).
2. Run `make preflight` (fmt + lint + unit tests) before you push, and at least the narrowest relevant `go test` packages (full `./...` preferred) before you request review.
3. Keep generated binaries under `bin/`/`dist/` locally only—never stage them.
4. Document any new build, lint, codegen, or release steps inside this file (and the relevant doc under `docs/` when it affects users/contributors).
5. When touching HTML/CSS, align with `DESIGN.md` and update tokens/components there first.
6. When introducing user-facing CLI features (new commands, flags, env vars, feature flags, or output modes), update `ktl help --ui` (search index + curated examples in `internal/helpui/`) so the new surface is discoverable.

## AI-Agent Guide (Repo Conventions)

If you’re an AI agent (or using one), start with:

- `docs/architecture.md`: repo layout + key packages.
- `docs/agent-playbook.md`: common workflows (“golden paths”) + validation commands.
- `docs/deps.md`: generated package-level dependency map (`make deps`).
- `DESIGN.md`: UI design system (HTML/CSS surfaces).

### Golden Paths (condensed)
- Make minimal, targeted changes and run the narrowest relevant tests; prefer `make fmt && make lint` plus scoped `go test` before full `./...`.
- Add a CLI flag: define it in `cmd/ktl/*`, thread through an options struct in `internal/*`, and add/extend a unit test nearby.
- Add a subcommand: keep Cobra wiring in `cmd/ktl/*`; put logic in `internal/*`; test with `go test ./cmd/ktl`.
- Profiles/app config: global `~/.ktl/config.yaml`, repo `.ktl.yaml`; validate with `go test ./cmd/ktl -run TestBuildProfile`.
- Update fixtures: edit `testdata/...`, refresh goldens, and rerun the closest tests.
- UI work: follow `DESIGN.md`; extend tokens/components first.
- Tags & GitHub Releases: create/push an annotated tag (`git tag -a vX.Y.Z -m "vX.Y.Z"`, then `git push origin vX.Y.Z`) and publish a matching GitHub Release (required by `.github/workflows/release-guard.yml`). CI uploads the release artifacts.
- When adding a new CLI surface, update `internal/helpui/examples.go` so help-ui search stays aligned with README/recipes.

## Repository Structure
- `cmd/ktl`: Cobra entrypoint; add flags, top-level wiring, and CLI UX only.
- `internal/*`: reusable packages (e.g., `internal/tailer`, `internal/workflows/buildsvc`). Keep scopes tight; long-running diagnostics live where they already exist.
- `integration/`: live-cluster harnesses. Expect a `KUBECONFIG`, keep them behind tags when slow.
- `testdata/`: CLI fixtures, golden files, and Helm bundles. Charts belong in `testdata/charts/` so render tests stay canonical.
- `bin/` + `dist/`: generated artifacts. Treat them like build outputs; do not commit.

## Build, Test, and Inspect Commands

| Command | Purpose |
| --- | --- |
| `make build` | Compile `ktl` for the host platform into `bin/ktl`. |
| `make install` | `go install ./cmd/ktl` to `$GOBIN`. |
| `make test` / `go test ./...` | Run unit + package tests. Prefer targeted packages when iterating, but run the full suite before PR. |
| `./scripts/testpoint.sh` / `make testpoint` | Single entrypoint: fmt, lint, unit tests, and smoke packaging (plus optional integration/e2e flags). |
| `make testpoint-all` | Unit + integration-tagged tests + chart verify e2e (allowlist). |
| `go test -tags=integration ./cmd/ktl -run TestBuildDockerfileFixtures` | Builds all Dockerfile fixtures in `testdata/build/dockerfiles/*`; requires Docker on Linux. |
| `go test -tags=integration ./cmd/ktl -run TestBuildComposeFixtures` | Builds every Compose stack under `testdata/build/compose/`; requires Docker + docker compose. |
| `go test -tags=integration ./cmd/ktl -run TestBuildRunsInsideSandbox` | Linux + Docker + sandbox runtime required; proves `ktl build` re-execs inside the sandbox end-to-end. |
| `make fmt` / `make lint` | Enforce gofmt + `go vet`. No manual whitespace tweaks. |
| `make preflight` | Alias for `make verify` (fmt + lint + unit tests). |
| `make release` | Cross-platform builds under `dist/`; only on clean tags. For ad-hoc GOOS/GOARCH, follow the README recipe. |
| `make package` | Build Linux `.deb` + `.rpm` packages into `dist/` via Docker (see `packaging/`). |
| `ktl build ... --ws-listen :9085` | Expose the build stream over WebSocket (for external consumers). |
| `ktl apply ... --ui :8080 --ws-listen :9086` | Mirror Helm rollouts with the deploy viewer (phase timeline, resource readiness grid, manifest diff, event feed) so reviewers can follow along remotely. |
| `ktl apply ...` (TTY) | Auto-enables the deploy console: metadata banner, inline phase badges, sticky warning rail, and adaptive resource table (use `--console-wide` to force the 100+ col layout). |
| `ktl apply plan --visualize --chart ./chart --release foo --kubeconfig ~/.kube/archimedes.yaml` | Render the tree-based dependency browser + YAML/diff viewer (with optional comparison upload), auto-write `./ktl-deploy-visualize-<release>-<timestamp>.html` (override with `--output`, use `--output -` for stdout). |
| `./bin/ktl --kubeconfig /Users/antonkrylov/.kube/archimedes.yaml` | Manual smoke test against the shared Archimedes cluster (add `--context/--namespace` as needed). |

`ktl deploy` has been removed; use `ktl apply`/`ktl delete` going forward.

Enable verbose sandbox diagnostics only when needed with `ktl build ... --sandbox-logs`. The flag streams `[sandbox]`-prefixed lines to stderr and mirrors them into any `--ws-listen` session so reviewers can watch sandbox ACL errors without rerunning the build.

### Sandbox Profiles

- Check every sandbox policy into `sandbox/<env>.cfg` (never under `bin/` or `dist/`) so reviewers can diff resource limits and mounts like any other fixture.
- Set `KTL_SANDBOX_CONFIG` in your shell profile (or pass `--sandbox-config`) to match the host you’re building on; this keeps local runs aligned with CI.
- Add a new row to the table below whenever you introduce another builder class.

| Environment | Policy file | Intended hosts | Notes |
| --- | --- | --- | --- |
| `linux-ci` | `sandbox/linux-ci.cfg` | Dedicated Linux BuildKit runners (Archimedes + CI) | Copy of the default policy with doubled tmpfs limits; export `KTL_SANDBOX_CONFIG=$REPO_ROOT/sandbox/linux-ci.cfg` to opt in. |
| `linux-strict` | `sandbox/linux-strict.cfg` | Linux hosts with user namespaces enabled | Tightens the default policy (user/pid/cgroup namespaces, drops env + caps, no sysfs, minimal /dev). If userns is unavailable, use `linux-ci` instead. |

Example:

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/sandbox/linux-ci.cfg"
```

### Feature Flags

- Register new flags and their descriptions in `internal/featureflags/featureflags.go`, keep names kebab-cased, and document them here.
- Toggle flags per-invocation with `ktl --feature <name>` (repeatable or comma-separated), via config files (`feature: ["<name>"]`), or with environment variables named `KTL_FEATURE_<FLAG>` (use uppercase with `_` instead of `-`).
- Use `featureflags.ContextWithFlags` to thread the resolved set through new code paths; add unit tests for both enabled/disabled cases before flipping defaults.

| Flag | Stage | Description | Enable via env |
| --- | --- | --- | --- |
| `deploy-plan-html-v3` | experimental | Switch `ktl apply plan --visualize` output to the v3 UI components. | `KTL_FEATURE_DEPLOY_PLAN_HTML_V3=1` |

### UI Mirror Defaults
- Passing `--ui` without an address binds the default port (`:8080` unless noted below). Use explicit `HOST:PORT` only when you need a custom interface.
- Default bindings: `ktl apply --ui` → `:8080`, `ktl delete --ui` → `:8080`.

## Day-to-Day Workflow

### Before You Code
- Sync main, skim open PRs touching the same area, and note shared fixtures in `testdata/`.
- Decide where logic belongs (CLI vs `internal/` package) before writing code.
- For docs or visuals, plan which section of this file you’ll update once work lands.

### While Coding
- Inject `context.Context` as the first parameter for any API-bound operation; use `logr.Logger` for structured logging.
- Stick to Cobra naming (`foo_cmd.go`) and exported symbol docs that mirror kubectl terminology (`*Options`, `*Strategy`).
- Default to table-driven tests alongside the implementation; keep fixtures under `testdata/<feature>`.
- Long-running or cross-resource diagnostics stay in their current modules—no new top-level packages unless approved.

### Before Opening A PR
- Run `make fmt`, `make lint`, and at least the relevant `go test` packages (full `./...` preferred). Record the results in your PR description.
- When feasible, prefer an end-to-end smoke test against a real cluster (with an explicit `--kubeconfig/--context/--namespace`) over fixture-only tests; record what you ran.
- For integration changes, note whether the tagged suites ran and what kubeconfig/context you used.
- Summarize user-facing impact, commands touched, and link issues/tickets. Mention build/log deltas with before/after snippets when output changes.
- Keep commit subjects ≤ 70 chars, using `<type>: <imperative>` when adding features (e.g., `feat: add deploy timeline`).

## Security & Configuration
- Never commit kubeconfigs, captured logs, or `dist/*.k8s` artifacts—treat them as secrets.
- When pointing to production-like clusters, scope access via dedicated contexts and prefer `KTL_*` env overrides instead of hardcoded credentials.
- Sanitize fixtures so Secrets/ConfigMaps contain placeholders, and document any required environment variables in the module README before asking for review.

## UI Design System

Source of truth: `DESIGN.md` (the frontend style book).

- Any HTML/CSS change should be compatible with `DESIGN.md` tokens/components and export rules.
- If you change a component’s “ground rules”, update `DESIGN.md` first and reference the exact subsection in your PR.
