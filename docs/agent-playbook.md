# KTL Agent Playbook

Golden paths + validation commands for contributors (humans and AI agents). Keep this doc short and practical: how to make a change without breaking CLI UX, integrations, or the HTML viewers.

## Repo Map (What Goes Where)

- `cmd/`: Cobra wiring, flags, CLI UX. Keep it thin.
- `internal/`: core logic packages. Prefer threading options structs + `context.Context`.
- `pkg/`: reusable exported libs (and generated API stubs under `pkg/api/v1`).
- `testdata/`: fixtures and goldens (charts, stacks, render fixtures).
- `integration/`: live-cluster harnesses (opt-in; guarded by env/tags).

## Golden Paths

### Add a Flag

1. Add flag wiring under `cmd/ktl/*`.
2. Thread into an options struct under `internal/*` (avoid global vars).
3. Validate behavior with a focused unit test (usually `go test ./cmd/ktl -run <TestName>`).
4. If user-facing: update help-ui search/examples under `internal/helpui/`.

### Add a Subcommand

1. Cobra wiring: `cmd/ktl/*` only.
2. Logic: new/extended package under `internal/*`.
3. Tests: start with `go test ./cmd/ktl`, then expand to `go test ./...` before review.

### Touch Deploy / UI Surfaces

- Design rules live in `AGENTS.md` (Frontend Design System) and `DESIGN.md`.
- Extend tokens/components first, then ship UI changes.
- Ensure print/export behavior stays correct (`@media print` rules).

### Update Protobuf / API Stubs

1. Lint: `make proto-lint`
2. Generate: `make proto`
3. Ensure the tree is clean: `git diff --exit-code`

## Validation (What Reviewers Expect)

Local (fast loop):

```bash
go test ./cmd/ktl -run TestName
```

Before opening a PR:

```bash
make fmt
make lint
make test
```

Single entrypoint (fmt + lint + unit + smoke):

```bash
./scripts/testpoint.sh
```

Optional suites:

- Tagged integration: `./scripts/testpoint.sh --integration`
- Chart verify allowlist: `./scripts/testpoint.sh --charts-e2e`

## Guardrails

- Do not commit build outputs: `bin/`, `dist/`.
- Keep secrets out of the repo (kubeconfigs, captured logs, rendered manifests with real values).
- Prefer deterministic outputs: when generating code or updating goldens, include the exact command you ran.

