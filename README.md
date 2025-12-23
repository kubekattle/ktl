# ktl

`ktl` is a kubectl-friendly Swiss Army knife focused on fast log tailing, BuildKit image builds, and Helm chart workflows (plan/apply/delete), with optional HTML viewers for mirrors and deploy streams.

## Why ktl?
- **Instant pod logs** – informer-backed discovery keeps startup under a second, even in large clusters.
- **Human-friendly output** – colors for namespaces/containers, Go-template rendering, JSON passthrough, and regex highlighting.
- **Deploy safety** – use `ktl plan` before `ktl apply`, and reuse plan artifacts during rollouts via `--reuse-plan`.
- **Shareable mirrors** – broadcast `ktl logs` / builds / deploys via `--ui` / `--ws-listen`.

### UI mirror defaults
Any command that supports `--ui` lets you omit the host/port. Unless noted, the mirror binds to `:8080` when you pass just `--ui`. Override the port (or host) by supplying an explicit value such as `--ui 0.0.0.0:9000`.

| Command | Default `--ui` bind |
| --- | --- |
| `ktl logs` mirrors | `:8080` |
| `ktl help` | `:8080` |
| `ktl apply` | `:8080` |
| `ktl delete` | `:8080` |

## Install
```bash
# From source (Go 1.25+)
go install ./cmd/ktl

# Via Makefile helper
make build     # writes ./bin/ktl
make install   # equivalent to go install ./cmd/ktl
make release   # cross-build archives into ./dist
```

## Examples
See `examples.md` for up-to-date CLI examples.

## Profiles and config (build defaults)
`ktl` supports execution profiles to apply sensible defaults.

- `ktl --profile dev|ci|secure|remote ...` sets a global profile (works with subcommands like `ktl build ...`).
- Build defaults can also come from config files:
  - Global: `~/.ktl/config.yaml`
  - Repo-local: `.ktl.yaml` at the repo root

CLI flags always win over profile/config defaults.

## Build policy gate (OPA/Rego)
`ktl build` supports a fast “policy gate” so security can codify what’s allowed and developers get actionable failures locally and in CI.

- `--policy <dir|https-url>` points at an OPA/Rego bundle (see `examples/policy/demo`).
- `--policy-mode warn|enforce` starts in warn mode and ratchets to enforcement.
- `--policy-report <path>` writes a machine-readable JSON report (defaults to `--attest-dir/ktl-policy-report.json` when `--attest-dir` is set).

## Build secrets guardrails
`ktl build` can also detect common secret leaks and stop (or warn) with pinpointed reasons:

- `--secrets warn|block|off` controls enforcement.
- `--secrets-config <file|https-url>` loads a YAML/JSON rule set (regex-based), similar to Trivy’s configurable checks.
- `--secrets-report <path>` writes a machine-readable JSON report (defaults to `--attest-dir/ktl-secrets-report.json` when `--attest-dir` is set).

Try the demo at `examples/secrets/demo`.

## Cache intelligence (ktl build)
`ktl build` prints a post-build cache summary by default:

- `--cache-intel` / `--cache-intel-top N` toggle and size the report.
- `--cache-intel-format human|json` controls output format.
- `--cache-intel-output <path|->` writes the report to a file (or stdout with `-`).

The report includes slowest steps, cache-hit/miss counts, largest final layers (when an OCI layout is produced), and best-effort attribution for cache misses (input changes vs cache eviction/prune).

## Development
```bash
make test   # go test ./...
make fmt    # gofmt
make lint   # go vet ./...
```

See `AGENTS.md` for contributor guidance.
