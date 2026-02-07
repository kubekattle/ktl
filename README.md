# ktl

`ktl` is a Kubernetes-focused CLI for logs, Helm workflows, and BuildKit builds.

Core commands:

- Fast pod logs: `ktl logs`
- Helm preview/apply/delete/revert: `ktl apply plan`, `ktl apply`, `ktl delete`, `ktl revert`
- Build images with BuildKit: `ktl build`
- Orchestrate many releases as a DAG: `ktl stack`
- HTML viewers: `ktl help --ui`, `ktl apply --ui`, `ktl delete --ui`

## Install

Requires Go 1.25.7+.

```bash
go install ./cmd/ktl
```

Or via Makefile:

```bash
make build     # writes ./bin/ktl
make install   # installs ./cmd/ktl to GOBIN/GOPATH/bin
```

Other binaries:

```bash
go install ./cmd/verify
go install ./cmd/package
```

## Quickstart

```bash
# Initialize repo defaults
ktl init

# Tail logs
ktl logs deploy/my-app -n default

# Preview and deploy a Helm chart (with the viewer)
ktl apply plan --chart ./chart --release my-app -n default
ktl apply --chart ./chart --release my-app -n default --ui

# Delete (with the viewer)
ktl delete --release my-app -n default --ui

# Build an image with BuildKit
ktl build . -t ghcr.io/acme/app:dev

# Searchable interactive help
ktl help --ui
```

## Docs

- Recipes: `docs/recipes.md`
- Architecture: `docs/architecture.md`
- Troubleshooting: `docs/troubleshooting.md`
- Contributor guardrails: `AGENTS.md`

## Development

```bash
make fmt
make lint
make test
```
