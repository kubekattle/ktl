# Agent Playbook (ktl)

This repo is optimized for small, reviewable changes. Prefer making the minimal change in the correct module, then run the narrowest tests that cover it.

## Where To Change What

- `cmd/ktl/*`: Cobra command wiring, flags, CLI UX, help text, and wiring into `internal/*`.
- `cmd/ktl-agent/*`: remote agent entrypoint and CLI wiring for `--remote-agent` / `--mirror-bus`.
- `internal/*`: core implementation (tailing, deploy/apply, UI mirroring, BuildKit workflows, config/feature flags).
- `proto/*` + `pkg/api/v1/*`: protobuf API definitions and generated stubs.
- `testdata/*`: fixtures and golden files (keep secrets out).

## Golden-Path Tasks

### Understand sandbox safety (Linux)

Threat model + safe reproduction steps: `docs/sandbox-security.md`.

### Add a new CLI flag (existing command)

1. Update the Cobra command under `cmd/ktl/*` (flag definition + help text).
2. Thread the value into an `internal/*` options struct (avoid global state).
3. Add/extend a unit test in the closest package.

Validate:

```bash
make fmt && make lint
go test ./cmd/ktl -run TestYourThing
go test ./...
```

### Add a new subcommand

1. Add a new `*_cmd.go` under `cmd/ktl/` and wire it from `cmd/ktl/main.go`.
2. Keep the command file “thin”: parse flags, construct options, call `internal/*`.
3. Put the real logic in a new or existing `internal/*` package.

Validate:

```bash
make fmt && make lint
go test ./cmd/ktl
```

### Change protobuf API

1. Edit `proto/*.proto`.
2. Regenerate stubs.

Validate:

```bash
make proto
go test ./...
```

### Add or update a fixture

1. Add/modify files under `testdata/...`.
2. If tests compare “goldens”, update the matching expected outputs in `testdata/`.

Validate:

```bash
go test ./... -run TestName
```

### Work on UI HTML/CSS surfaces

1. Follow the “Frontend Design System” rules in `AGENTS.md`.
2. Prefer extending tokens/components rather than one-off styles.

Validate:

```bash
go test ./... -run TestName
```
