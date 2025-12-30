# ktl

`ktl` is a kubectl-friendly Swiss Army knife focused on fast log tailing, BuildKit image builds, and Helm chart workflows (plan/apply/delete), with optional HTML viewers for mirrors and deploy streams.

## Why ktl?
- **Instant pod logs** – informer-backed discovery keeps startup under a second, even in large clusters.
- **Human-friendly output** – colors for namespaces/containers, Go-template rendering, JSON passthrough, and regex highlighting.
- **Deploy safety** – use `ktl apply plan` before `ktl apply`, and reuse plan artifacts during rollouts via `--reuse-plan`.
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

### Binaries (standalone CLIs)
- `verify`: `go install ./cmd/verify` or `make build-verify` (writes `./bin/verify`). Uses the same version string as ktl; run `verify --version`.
- `package`: `go install ./cmd/package` or `make build-packagecli` (writes `./bin/package`). Uses the same version string; run `package --version`.
Both binaries are also published as release assets (see Releases).

## Examples
See `docs/recipes.md` for copy/paste workflows, or run `ktl help --ui` for searchable command examples.

## Releasing (tags + GitHub Releases)
1) Create an annotated tag: `git tag -a vX.Y.Z -m "vX.Y.Z"` and `git push origin vX.Y.Z`.
2) Create a matching GitHub Release: `gh release create vX.Y.Z --title "vX.Y.Z" --notes "<summary>" [assets...]`.
3) Attach build artifacts (ktl/verify/package binaries or archives) or explicitly note when none are attached.

### Install from releases
1) Download the tarball for your OS/ARCH (ktl/verify/package) from the Releases page.
2) Verify checksum: `sha256sum -c checksums.txt` (or the matching `.sha256` file).
3) Install: `chmod +x ./<tool> && sudo mv ./<tool> /usr/local/bin/<tool>`.

Sample versions:
- `ktl --version`
- `verify --version`
- `package --version`

## CI / Branch hygiene
- CI should run at least `make fmt lint test` on PRs; add a smoke test that packages a sample chart then verifies the archive to catch CLI regressions.
- Cache Go modules in CI to speed builds.
- Protect `main`/`dev`: require PRs and checks; prune stale remote branches regularly (policy noted in `AGENTS.md`).

## Standalone CLIs in recipes
`docs/recipes.md` includes copy/paste examples for `verify` and `package`; help-ui search also covers both commands.

## Profiles and config (build defaults)
`ktl` supports execution profiles to apply sensible defaults.

- `ktl --profile dev|ci|secure|remote ...` sets a global profile (works with subcommands like `ktl build ...`).
- Build defaults can also come from config files:
  - Global: `~/.ktl/config.yaml`
  - Repo-local: `.ktl.yaml` at the repo root

CLI flags always win over profile/config defaults.

## Build policy gate (OPA/Rego)
`ktl build` supports a fast “policy gate” so security can codify what’s allowed and developers get actionable failures locally and in CI.

- `--policy <dir|https-url>` points at an OPA/Rego bundle.
- `--policy-mode warn|enforce` starts in warn mode and ratchets to enforcement.
- `--policy-report <path>` writes a machine-readable JSON report (defaults to `--attest-dir/ktl-policy-report.json` when `--attest-dir` is set).

## Build secrets guardrails
`ktl build` can also detect common secret leaks and stop (or warn) with pinpointed reasons:

- `--secrets warn|block|off` controls enforcement.
- `--secrets-config <file|https-url>` loads a YAML/JSON rule set (regex-based), similar to Trivy’s configurable checks.
- `--secrets-report <path>` writes a machine-readable JSON report (defaults to `--attest-dir/ktl-secrets-report.json` when `--attest-dir` is set).

See `docs/recipes.md` for practical examples.

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

codex resume 019b52ae-9c33-7611-9548-f68b56f1ff56
