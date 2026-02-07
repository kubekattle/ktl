# Agent Playbook (Humans + AI Agents)

Start here when you want a change to land cleanly:

1. Read `AGENTS.md` (repo guardrails, golden paths, release flow).
2. Skim `docs/architecture.md` (repo layout and package ownership).

## Preflight

Run early and always before you push:

```bash
make preflight # fmt + lint + unit tests
```

If you changed user-facing behavior (flags/output/config parsing), also do a local smoke:

```bash
make build
./bin/ktl --help
```

## Repo Map

- `cmd/`: Cobra wiring, flags, CLI UX.
- `internal/`: core logic packages.
- `pkg/`: reusable exported libs (generated API stubs live under `pkg/api/`).
- `testdata/`: fixtures and goldens (charts, stacks, render fixtures).
- `integration/`: live-cluster harnesses (opt-in; guarded by env/tags).
- `docs/`: contributor docs and embedded help-ui content (see `docs/embed.go`).

## Golden Paths

### Add a CLI flag

1. Add Cobra flag wiring under `cmd/ktl/*`.
2. Thread into an options struct under `internal/*`.
3. Add/extend a unit test near the behavior.
4. If user-facing: update help-ui search/examples under `internal/helpui/`.

### Add a subcommand

1. Cobra wiring: `cmd/ktl/*` only.
2. Logic: implement in `internal/*`.
3. Tests: start with `go test ./cmd/ktl/...`, then run `make test` before review.

### Touch HTML/CSS/UI

Source of truth: `DESIGN.md`.

- Prefer extending tokens and existing components over one-off styles.
- Verify the relevant surface manually (`ktl help --ui`, `ktl apply --ui`, `ktl delete --ui`).

### Update protobuf / API stubs

```bash
make proto-lint
make proto
git diff --exit-code
```

## Testing Map

- Unit tests: `make test` (or `go test ./...`).
- CI parity: `make test-ci` (fmt + lint + tests + package/verify smoke).
- Live-cluster integration suite (requires `kubectl` + kubeconfig): `KTL_TEST_KUBECONFIG=$HOME/.kube/config go test ./integration/...` (example kubeconfig: `$HOME/.kube/archimedes.yaml`).

## Guardrails

- Do not commit build outputs (`bin/`, `dist/`).
- Keep secrets out of the repo (kubeconfigs, captured logs, rendered manifests with real values).
- When generating code or goldens, include the exact command you ran in the PR description.

## References

- gRPC agent API (ktl-agent): `docs/grpc-agent.md`
- Dependency map (generated): `docs/deps.md` (refresh via `make deps`)
- Troubleshooting: `docs/troubleshooting.md`
- Sandbox policy: `sandbox/*.cfg`
