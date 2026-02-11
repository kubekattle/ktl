# Contributing to ktl

Thanks for helping improve ktl. This document highlights the minimum testing steps reviewers expect before a pull request lands. Align your workflow with the repo guidelines in `AGENTS.md` and the Makefile whenever you touch code.

## Test Matrix

| Change type | Required commands | Notes |
| --- | --- | --- |
| Any Go code change | `make fmt`, `make lint`, `make test` | `make fmt` enforces gofmt; `make lint` runs `go vet` (and `staticcheck` when available); `make test` is equivalent to `go test ./...`. Run them locally before pushing. |
| CLI / Cobra wiring | `go test ./cmd/...` in addition to the default matrix | Focuses on fast command-scope tests when you only altered CLI wiring. |
| Integration features (logs, capture, report, etc.) | `KTL_TEST_KUBECONFIG=$HOME/.kube/config go test ./integration/...` | Requires access to a Kubernetes cluster plus `kubectl`. Example kubeconfig: `$HOME/.kube/archimedes.yaml`. The harness builds `bin/ktl.test`, applies the `testdata/ktl-logger.yaml` fixture, and exercises real kubectl/ktl flows. Expect ~1 minute runtime. |
| Docs only (Markdown, design notes) | No tests required | Call out “docs only” in the PR body; still run `make fmt` if you touched Go code. |

### Running Unit Tests

```bash
make preflight # fmt + lint + unit tests
make fmt   # gofmt all modules
make lint  # go vet (+staticcheck when installed)
make test  # go test ./...
```

Use `GO_TEST_FLAGS` when you need verbose output, e.g. `GO_TEST_FLAGS=-run TestKtlCaptureReplayFilters make test`.

### Running Integration Tests

1. Ensure you have a kubeconfig for a test cluster (example: `~/.kube/config` or `~/.kube/archimedes.yaml`).
2. Run:
   ```bash
   KTL_TEST_KUBECONFIG=$HOME/.kube/config go test ./integration/... # e.g. $HOME/.kube/archimedes.yaml
   ```
3. The harness will:
   - Build `bin/ktl.test`.
   - Apply `testdata/ktl-logger.yaml` using `kubectl --kubeconfig ...` and wait for the pods.
   - Exercise log tailing plus the capture E2E scenarios.

If the cluster is missing optional namespaces (e.g., `kubernetes-dashboard`), some dashboard-only assertions will skip automatically; note this in your PR if it happens frequently.

### When to Re-run Tests

- Any time you rebase or resolve conflicts, re-run the relevant matrix above.
- If you touch files under `internal/`, `cmd/`, or `integration/`, run at least `make test`; add the integration suite when behavior depends on live clusters.
- Mention exact commands (copy/paste from above) in your PR description so reviewers know what passed.
