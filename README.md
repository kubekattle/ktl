# ktl

`ktl` is a kubectl-friendly Swiss Army knife focused on fast log tailing, capture/replay for incident artifacts, BuildKit image builds, and Helm chart workflows (plan/apply/delete), with optional HTML viewers for mirrors and deploy streams.

## Why ktl?
- **Instant pod logs** – informer-backed discovery keeps startup under a second, even in large clusters.
- **Human-friendly output** – colors for namespaces/containers, Go-template rendering, JSON passthrough, and regex highlighting.
- **Capture & replay** – record logs/events/metadata into a portable artifact and replay it offline.
- **Deploy safety** – use `ktl plan` before `ktl apply`, and reuse plan artifacts during rollouts via `--reuse-plan`.
- **Shareable mirrors** – broadcast `ktl logs` / capture replays / builds / deploys via `--ui` / `--ws-listen`.

### UI mirror defaults
Any command that supports `--ui` lets you omit the host/port. Unless noted, the mirror binds to `:8080` when you pass just `--ui`. Override the port (or host) by supplying an explicit value such as `--ui 0.0.0.0:9000`.

| Command | Default `--ui` bind |
| --- | --- |
| `ktl logs` mirrors | `:8080` |
| `ktl build` | `:8080` |
| `ktl apply` (alias: `ktl deploy apply`) | `:8080` |
| `ktl delete` (alias: `ktl deploy destroy`) | `:8080` |

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

## Development
```bash
make test   # go test ./...
make fmt    # gofmt
make lint   # go vet ./...
```

See `AGENTS.md` for contributor guidance.
