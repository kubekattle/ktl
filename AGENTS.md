# AGENTS Playbook

One-page guardrails for anyone touching `ktl`: start here before coding, keep it open while you work, and update it first when the ground rules change.

## How To Use This File
- Read **Quick Checklist** when you pick up a task; everything else dives deeper.
- Follow the **Day-to-Day Workflow** steps in order (pre-flight → coding → PR).
- Reference the section headers when your reviewer asks “did you run X?” or “what’s the design spec?”
- When guidance changes, edit this document and point to the exact subsection in your PR/issue description.

## Quick Checklist
1. Confirm you understand the repo layout and that changes stay inside the right module (`cmd/` wiring vs `internal/` logic vs `testdata/` fixtures).
2. Run `make fmt && make lint` before you push, and `go test ./...` (or narrower) before you request review.
3. Keep generated binaries under `bin/`/`dist/` locally only—never stage them.
4. Document any new build, lint, or Pandoc steps inside this file and in `docs/pandoc-build.md` if relevant.
5. When touching HTML/CSS, align with the Frontend Design System below and update tokens/components there first.

## AI-Agent Guide (Repo Conventions)

If you’re an AI agent (or using one), start with:

- `docs/architecture.md`: repo layout + key packages.
- `docs/agent-playbook.md`: common workflows (“golden paths”) + validation commands.
- `docs/deps.md`: generated package-level dependency map (`make deps`).

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
| `go test -tags=integration ./cmd/ktl -run TestBuildDockerfileFixtures` | Builds all Dockerfile fixtures in `testdata/build/dockerfiles/*`; requires Docker on Linux. |
| `go test -tags=integration ./cmd/ktl -run TestBuildComposeFixtures` | Builds every Compose stack under `testdata/build/compose/`; requires Docker + docker compose. |
| `go test -tags=integration ./cmd/ktl -run TestBuildRunsInsideSandbox` | Linux + Docker + sandbox runtime required; proves `ktl build` re-execs inside the sandbox end-to-end. |
| `make fmt` / `make lint` | Enforce gofmt + `go vet`. No manual whitespace tweaks. |
| `make release` | Cross-platform builds under `dist/`; only on clean tags. For ad-hoc GOOS/GOARCH, follow the README recipe. |
| `make package` | Build Linux `.deb` + `.rpm` packages into `dist/` via Docker (see `packaging/`). |
| `ktl build ... --ws-listen :9085` | Expose the build stream over WebSocket (for external consumers). |
| `ktl apply ... --ui :8080 --ws-listen :9086` | Mirror Helm rollouts with the deploy viewer (phase timeline, resource readiness grid, manifest diff, event feed) so reviewers can follow along remotely. |
| `ktl apply ...` (TTY) | Auto-enables the deploy console: metadata banner, inline phase badges, sticky warning rail, and adaptive resource table (use `--console-wide` to force the 100+ col layout). |
| `ktl plan --visualize --chart ./chart --release foo --kubeconfig ~/.kube/archimedes.yaml` | Render the tree-based dependency browser + YAML/diff viewer (with optional comparison upload), auto-write `./ktl-deploy-visualize-<release>-<timestamp>.html` (override with `--output`, use `--output -` for stdout). |
| `./bin/ktl --kubeconfig /Users/antonkrylov/.kube/archimedes.yaml` | Manual smoke test against the shared Archimedes cluster (add `--context/--namespace` as needed). |

`ktl deploy` has been removed; use `ktl apply`/`ktl delete` going forward.

Enable verbose sandbox diagnostics only when needed with `ktl build ... --sandbox-logs`. The flag streams `[sandbox]`-prefixed lines to stderr and mirrors them into any `--ws-listen` session so reviewers can watch sandbox ACL errors without rerunning the build.

### Sandbox Profiles

- Check every sandbox policy into `testdata/sandbox/<env>.cfg` (never under `bin/` or `dist/`) so reviewers can diff resource limits and mounts like any other fixture.
- Set `KTL_SANDBOX_CONFIG` in your shell profile (or pass `--sandbox-config`) to match the host you’re building on; this keeps local runs aligned with CI.
- Add a new row to the table below whenever you introduce another builder class.

| Environment | Policy file | Intended hosts | Notes |
| --- | --- | --- | --- |
| `linux-ci` | `testdata/sandbox/linux-ci.cfg` | Dedicated Linux BuildKit runners (Archimedes + CI) | Copy of the default policy with doubled tmpfs limits; export `KTL_SANDBOX_CONFIG=$REPO_ROOT/testdata/sandbox/linux-ci.cfg` to opt in. |
| `linux-strict` | `testdata/sandbox/linux-strict.cfg` | Linux hosts with user namespaces enabled | Tightens the default policy (user/pid/cgroup namespaces, drops env + caps, no sysfs, minimal /dev). If userns is unavailable, use `linux-ci` instead. |

Example:

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/testdata/sandbox/linux-ci.cfg"
```

### Feature Flags

- Register new flags and their descriptions in `internal/featureflags/featureflags.go`, keep names kebab-cased, and document them here.
- Toggle flags per-invocation with `ktl --feature <name>` (repeatable or comma-separated), via config files (`feature: ["<name>"]`), or with environment variables named `KTL_FEATURE_<FLAG>` (use uppercase with `_` instead of `-`).
- Use `featureflags.ContextWithFlags` to thread the resolved set through new code paths; add unit tests for both enabled/disabled cases before flipping defaults.

| Flag | Stage | Description | Enable via env |
| --- | --- | --- | --- |
| `deploy-plan-html-v3` | experimental | Switch `ktl plan --visualize` output to the v3 UI components. | `KTL_FEATURE_DEPLOY_PLAN_HTML_V3=1` |

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
- For integration changes, note whether the tagged suites ran and what kubeconfig/context you used.
- Summarize user-facing impact, commands touched, and link issues/tickets. Mention build/log deltas with before/after snippets when output changes.
- Keep commit subjects ≤ 70 chars, using `<type>: <imperative>` when adding features (e.g., `feat: add deploy timeline`).

## Documentation Generation (Pandoc)
- Install once per machine: `brew install pandoc`, `brew install --cask mactex`, then copy `docs/eisvogel-template/template-multi-file/eisvogel.latex` into your Pandoc templates directory (`pandoc --version` reveals the path).
- Russian feature guide build:
  ```bash
  pandoc docs/ktl_features_ru.md \
    --from markdown+yaml_metadata_block+grid_tables+pipe_tables \
    --template eisvogel \
    --table-of-contents --toc-depth 3 \
    --number-sections --highlight-style tango \
    --pdf-engine=xelatex --variable papersize=a4 \
    --include-in-header=docs/custom-header.tex \
    --include-before-body=docs/titlepage.tex \
    -o dist/ktl_features_ru.pdf
  ```
- HTML handoff: `pandoc docs/ktl_features_ru.md -t html5 --filter mermaid-filter -o dist/ktl_features_ru.html` to preserve Mermaid diagrams.
- Keep generated PDFs/HTML strictly under `dist/` and list the build command in your PR notes whenever docs change.
- `docs/pandoc-build.md` carries extended options (resource paths, alt templates). Update it first when altering the recipe, then summarize the delta here.

## Security & Configuration
- Never commit kubeconfigs, captured logs, or `dist/*.k8s` artifacts—treat them as secrets.
- When pointing to production-like clusters, scope access via dedicated contexts and prefer `KTL_*` env overrides instead of hardcoded credentials.
- Sanitize fixtures so Secrets/ConfigMaps contain placeholders, and document any required environment variables in the module README before asking for review.

---

## Frontend Design System Overview

Source of truth for every HTML-based `ktl` surface (`ktl apply --ui`, `ktl delete --ui`, `ktl plan --format=html --visualize`, etc.). Extend tokens/components here first, then ship UI. Use the navigation below to jump to the exact rule you’re touching.

### Navigation
1. [Design Foundations](#1-design-foundations)
2. [Layout & Containers](#2-layout--containers)
3. [Core Components](#3-core-components)
4. [Interaction Patterns](#4-interaction-patterns)
5. [Accessibility Checklist](#5-accessibility-checklist)
6. [Extending the System](#6-extending-the-system)

### 1. Design Foundations

#### 1.1 Color + Elevation Tokens

| Token | Value | Intent |
| --- | --- | --- |
| `--surface` | `rgba(255,255,255,0.9)` | Default panel background for any data viewport. |
| `--surface-soft` | `rgba(255,255,255,0.82)` | Score/summary tiles and stacked cards. |
| `--border` | `rgba(15,23,42,0.12)` | Neutral dividers, panel outlines, table rules. |
| `--text` | `#0f172a` | Primary body text. |
| `--muted` | `rgba(15,23,42,0.65)` | Secondary labels/subtitles. |
| `--accent` | `#2563eb` | Focus states, links, chart highlights. |
| `--chip-bg` / `--chip-text` | `rgba(37,99,235,0.08)` / `#1d4ed8` | Filter chips background/text. |
| `--sparkline-color` | `#0ea5e9` | Trend lines for compact charts. |
| `--warn` | `#fbbf24` | Warning states (lagging scores, pending rollouts). |
| `--fail` | `#ef4444` | Failure states (blocking alerts, hard errors). |

**Elevation:** Primary panels use `0 40px 80px rgba(16,23,36,0.12)` plus `backdrop-filter: blur(18px)` for the frosted effect. Secondary widgets drop to `0 18px 40px rgba(15,23,42,0.12)`—never heavier than the core shadow.

#### 1.2 Typography

- Stack: `"SF Pro Display", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif`.
- Hierarchy: `h1` 2.8rem/600/-0.04em; card headline numbers 2.2rem/~570; score values 2rem tight; section labels/table headers uppercase 0.75–0.9rem with 0.18–0.2em tracking; body copy 1rem in `--text`, helper captions in `--muted`.
- Align numerics for scanning and keep subtitles muted so primary metrics stay dominant.

#### 1.3 Spacing + Radii

- Canvas padding `48px 56px 72px` (desktop), panel padding 32px on all sides.
- Grid gap: 1.1rem (summary cards) / 1rem (score cards).
- Radii: 28px panels, 24px cards, 16px insight widgets; chips remain fully rounded.

#### 1.4 Responsive Behaviour

- `.layout` defaults to a two-column flex row with a 320px `.insight-stack`; collapse to single column under 1100px and disable sticky behavior.
- Tables/accordions stretch to full width—avoid fixed widths so blocks slot into future dashboards.

#### 1.5 Print & Export Modes

- Surfaces supporting `?print`/PDF hide chrome-only sections (`.insight-stack`, toolbars, chip rows, toasts) and swap shadows for flat `border-color:#000` outlines.
- Extend the shared `@media print` rule whenever you add a new component so exports stay clean.

#### 1.6 Motion & Micro-interactions

- Default motion easing to `--ease: cubic-bezier(.16,1,.3,1)` and keep durations ≤300 ms; wrap all transitions/animations in `@media (prefers-reduced-motion: no-preference)` so accessibility settings are respected.
- Stagger panels by setting `style="--panel-delay:<n>"` or a matching CSS variable; the deploy/install viewer expects the first hero panel at `0`, timeline/resource panels increasing down the column, and sidebar cards mirroring their counterparts.
- When animating stateful widgets (timeline dots, hero progress bar, chips), mutate CSS custom properties (`--hero-progress`, `--usage`, etc.) so JS only sets intent and CSS handles the actual motion.
- Pulse/shine effects must stay subtle: e.g., timeline dots get a single 1.4 s `pulseDot` when fresh data arrives, log entries slide in with ≤260 ms fade, and hero progress bars glide rather than jump.

### 2. Layout & Containers

#### 2.1 Body + Chrome

- Background stays radial (white → `#dce3f1`) until theming exists.
- Wrap top-level sections in `.chrome` capped at 1200px width.
- Headers pair `h1` with `.subtitle` describing cluster/time context; piggyback new metadata there before inventing new chrome.

#### 2.2 Panels

- Use `<div class="panel">` for anything needing depth (charts, rollups, insights). Namespace accordions reuse `class="panel namespace"` to inherit spacing, radii, shadows.

### 3. Core Components

#### 3.1 Toolbar

- Structure: search input + dynamic chip row. Inputs stay pill-shaped (`border-radius:999px`, 0.75rem vertical padding) with 3px accent glow focus rings.
- Chips use uppercase weight-600 text; `.active` adds a 2px accent outline plus slight translateY. Name chips with `data-filter-chip` so JS autowires them.

#### 3.2 Summary Cards (`.grid .card`)

- Anatomy: uppercase label + bold value, optional inline delta badge.
- Grid layout: `grid-template-columns: repeat(auto-fit, minmax(180px,1fr))` so cards remain responsive.

#### 3.3 Score Cards (`.score-card`)

- Contents: title/delta block, percentage value, optional sparkline, budget bar, supporting blurb, optional `<details>` drilldown.
- States: default pass (green gradient), `.warn`, `.fail` drive yellow/red progress bars and icon accents.
- Drilldowns stay a single `<details class="score-drilldown">` per card with a chevron-only `<summary>` plus `.sr-only` text for accessibility.

#### 3.4 Namespace & Resource Accordions

- Structure: `<details class="panel namespace">` with summary row (name, counts, chevron) and tables inside `.content`.
- Table headers are uppercase/muted; rows split by `rgba(15,23,42,0.06)` borders.
- Use `.hidden` for filtering parity—no custom hide classes.

#### 3.5 Insight Stack (Right Column)

1. **Timeline (`.insight-panel .timeline`)**: dot + text entries with `li.warn`/`li.fail` states.
2. **Budget Widgets (`.budget-widget`)**: conic-gradient donuts driven by `style="--usage:<0-100>"` or `data-usage`, toggling `.active` to feed `data-namespaces` into filters.
3. **Runbook Cards (`.runbook-card`)**: CTA surfaces with optional `.cta` button/link; reuse for remediation lists or onboarding prompts.

#### 3.6 Toasts

- Element `#copyToast.toast` toggles `.visible` for <2s feedback; reuse instead of creating new toast implementations.

#### 3.7 Deploy Hero Panel & Progress Track

- The first panel in `ktl apply --ui` is a `.panel.hero-panel` that introduces release metadata (`Release`, `Namespace`, `Elapsed`) plus a live stage rail; update this component before tweaking downstream cards.
- Stages are rendered via `.hero-track` with one `.hero-stage` per phase (`render`, `diff`, `upgrade`, `install`, `wait`, `post-hooks`). JS updates `data-state` and the inner `.hero-status` label, while CSS animates the gradient fill using `--hero-progress`.
- Keep hero copy tight: `heroTitle` follows `Deploying <release>` and `heroSubtitle` concatenates `ns/<namespace> · <chart> · <version>`; chips mirror this data for quick scanning.
- Resource rows mirror the rendered manifest: every namespace/kind listed there (Deployments, StatefulSets, CronJobs, HPAs, Pods, PDBs, hooks, cross-namespace objects) streams through the tracker so the terminal table and webcast stay accurate even when releases span namespaces.
- Any new deploy phases must hook into `phaseOrder`, emit `deploy.StreamEvent` updates, and call `updateHeroStages()` so the progress rail, timeline pulses, and status chip stay in sync.

### 4. Interaction Patterns

| Pattern | Description | Implementation Notes |
| --- | --- | --- |
| Filter search | Live filtering across pods/resources/log lines. | Input `#podFilter`; extend selectors but keep the `data-filter` contract. |
| Chip filters | Multi-select toggles for readiness/namespaces/severities. | Shared handler via `data-filter-chip`; new chips only need dataset hooks. |
| Budget widget filter | Focus on namespace groups or app tiers. | Populate `data-namespaces`; `applyFilters()` handles highlighting. |
| Score drilldown | Expandable `<details>` for bullet lists/stat history. | One chevron trigger per card plus `.sr-only` summary text. |
| Namespace accordion | Native `<details>` with CSS chevron rotation. | Body uses `.content` flex column gap 24px; reuse for upcoming resource types. |
| Copy-to-clipboard | `.cta.copy` buttons with `data-command`. | Centralized toast + clipboard wiring; use for CLI snippets/queries. |

### 5. Accessibility Checklist

- Provide visible text or `aria-label` for every interactive control (search inputs, chips, donuts, chevrons).
- Preserve focus states: pill inputs keep accent glow, `.cta` buttons use border/color shifts, drilldown summaries get a solid black outline.
- Default `#0f172a` on `--surface` already meets WCAG AA; verify 4.5:1 contrast before introducing darker backgrounds.

### 6. Extending the System

1. **Start with tokens** – add CSS variables (`--success`, `--latency`, etc.) at `:root` instead of hard-coding.
2. **Reuse containers** – anchor new modules in `.panel`/`.insight-panel` before altering padding or margins.
3. **Follow component anatomy** – summary metrics = `.card`, thresholds = `.score-card`, guidance = `.runbook-card`.
4. **Hook into JS utilities** – extend `applyFilters()` and shared clipboard helpers; avoid bespoke logic.
5. **Update export modes** – decide if the component appears in print/PDF and extend the shared `@media print` rule.
6. **Document deviations** – when you must break the mold, capture the rationale here so future contributors understand the exception.

Use this section as the gate before merging UI changes: if your component cannot be described with these primitives, evolve the system here first.
