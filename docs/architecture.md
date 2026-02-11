# KTL Architecture (Current)

This repo is a single-module Go CLI with an optional companion agent.

## Layout
- `cmd/ktl`: end-user CLI (Cobra) and CLI-only helpers.
- `cmd/ktl-agent`: gRPC agent used by `--remote-agent` / `--mirror-bus`.
- `internal/*`: non-exported libraries used by the CLI/agent (tailing, deploy/apply, UI mirroring, BuildKit workflows, config/feature flags).
- `pkg/*`: reusable non-`internal` packages (BuildKit/Compose/registry helpers and generated API stubs under `pkg/api/ktl/api/v1`).
- `testdata/*`: fixtures and golden files.

## Main Commands (wired today)
- `init`
- `build`
- `apply` (and `apply plan`)
- `delete`
- `stack`
- `revert`
- `list`
- `lint`
- `logs`
- `env`
- `secrets`
- `version`

The root command wiring lives in `cmd/ktl/main.go`.

## Internal Package Map (Purpose, Key Types, Invariants)

This section is intentionally short and repetitive: AI agents do best with a stable “map” of where responsibilities live, which symbols are entrypoints, and what must not change.

### `internal/config`

- Purpose: shared CLI/config-layer options (flags + config binding + validation).
- Key types: `Options` (`AddFlags`, `BindFlags`, `Validate`).
- Invariants: CLI packages should call into `Options` rather than re-parsing env/config ad-hoc.

### `internal/featureflags`

- Purpose: register and resolve feature flags consistently.
- Key types: `Definition`, `Flags` (`Enabled`, `EnabledNames`), `Name`, `Stage`.
- Invariants: flag names stay kebab-cased; toggles flow via context/config/env (don’t introduce new toggle mechanisms).

### `internal/logging`

- Purpose: structured logging configuration and shared logger helpers.
- Key entrypoints: `internal/logging/logger.go` (logger construction/config).
- Invariants: avoid global loggers; pass loggers/context through call chains.

### `internal/grpcutil`

- Purpose: gRPC dial/wiring helpers for local/remote agent connections.
- Key entrypoints: `internal/grpcutil/dial.go`.
- Invariants: keep connection/security defaults centralized here (avoid duplicating dial options in commands).

### `internal/api/convert`

- Purpose: translate between internal runtime structs and protobuf API types (`pkg/api/ktl/api/v1`).
- Key types: `BuildConfig`, `DeployApplyConfig`, `DeployDestroyConfig`.
- Invariants: conversion is one-way “boundary glue”; don’t leak protobuf types into core packages.

### `internal/kube`

- Purpose: Kubernetes client helpers used by tailing/deploy.
- Key types: `Client` (`Exec`).
- Invariants: Kubernetes API calls accept `context.Context` and are cancellable.

### `internal/tailer`

- Purpose: stream logs (pods/nodes) and feed observers (terminal, UI).
- Key types: `Tailer` (`Run`), `LogRecord`, `LogObserver`, `Option`.
- Invariants: tailing is streaming and cancellation-driven; observers must tolerate bursts and duplicates.

### `internal/deploy`

- Purpose: Helm apply/delete orchestration and progress/event streaming to observers (TTY + UI).
- Key types: `InstallOptions`/`InstallResult`, `TemplateOptions`/`TemplateResult`, `StreamBroadcaster`, `StreamEvent`, `ResourceTracker`, `ResourceStatus`.
- Invariants: observers are optional and must not block the core deploy loop; events should remain stable for UI consumers.

### `internal/secretstore`

- Purpose: resolve `secret://` references in deploy-time values using pluggable providers.
- Key types: `Resolver`, `Config`, `Provider`.
- Invariants: never log secret values; audit references only.

### `internal/ui`

- Purpose: terminal UX primitives (deploy console, spinner).
- Key types: `DeployConsole` + `DeployConsoleOptions`, `DeployMetadata`.
- Invariants: UI code should consume observer/event interfaces rather than reaching into core packages.

### `internal/caststream`

- Purpose: “UI mirror” server (HTTP/WebSocket) that streams logs/build/deploy events to the browser.
- Key types: `Server` (`Run`, `ObserveLog`, `HandleDeployEvent`), `Mode`, `Option`.
- Invariants: server is a pure observer of streaming events; it must not own core business logic.

### `internal/mirrorbus`

- Purpose: publish log streams onto the gRPC “mirror bus” for remote viewers.
- Key types: `Publisher` (`ObserveLog`, `Close`).
- Invariants: publisher must remain non-blocking and safe to close at any point.

### `internal/dockerconfig`

- Purpose: Docker auth/config resolution used during builds and remote operations.
- Key entrypoints: `internal/dockerconfig/dockerconfig.go`.
- Invariants: never log credentials; keep file paths/config parsing centralized.

### `internal/csvutil` / `internal/castutil`

- Purpose: small utilities for CSV/cast formatting used across workflows.
- Invariants: keep these packages dependency-light and free of side effects.

### `internal/workflows/buildsvc`

- Purpose: BuildKit-based image build workflow orchestration (including sandbox support and progress observers).
- Key types: `service.Run(ctx, opts) (*Result, error)`, `Result`, `Dependencies`, `Streams`, `BuildMode`.
- Invariants: build is streaming + cancellable; sandbox policy is selected/configured centrally (don’t fork policy logic in commands).
- Notes: progress observers emit cache diagnostics and a post-build “cache intelligence” report (input diffs, cache key/graph diffs, slow steps, and OCI layer size rollups when an OCI layout is produced).

### `internal/agent`

- Purpose: gRPC server implementation for the remote agent.
- Key types: `Server` (`Run`), per-service handlers (`LogServer`, `BuildServer`, `DeployServer`, `MirrorServer`).
- Invariants: agent handlers forward events via existing observer interfaces (don’t duplicate deploy/build logic inside the agent).

## Agent-Facing Docs

- Golden paths + validation commands: `docs/agent-playbook.md`
- UI design system (HTML/CSS surfaces): `DESIGN.md`
- gRPC agent API: `docs/grpc-agent.md`
- Generated package dependency map: `docs/deps.md` (refresh with `make deps`)
