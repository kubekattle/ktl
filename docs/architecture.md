# TORQUE Architecture (Current)

This repo is a single-module Go CLI with an optional companion agent.

Fleet mode is designed as an optional Kubernetes-installed control plane, not a
replacement for local CLI operation. The fleet design lives in
[`docs/fleet-control-plane-spec.md`](fleet-control-plane-spec.md): dedicated
etcd stores inventory and coordination metadata, NATS JetStream stores
high-volume assignments and receipts, durable `torque-agent` processes execute
typed resources, and local SQLite remains the portable evidence/export cache.

## Layout
- `cmd/torque`: end-user CLI (Cobra) and CLI-only helpers.
- `cmd/torque-agent`: gRPC agent used by `--remote-agent` / `--mirror-bus`.
- `internal/*`: non-exported libraries used by the CLI/agent (tailing, deploy/apply, UI mirroring, BuildKit workflows, config/feature flags).
- `pkg/*`: reusable non-`internal` packages (BuildKit/Compose/registry helpers and generated API stubs under `pkg/api/torque/api/v1`).
- `testdata/*`: fixtures and golden files.

## Main Commands (wired today)
- `init`
- `build`
- `explain`
- `apply` (including `apply plan` and `apply simulate`)
- `delete`
- `stack`
- `revert`
- `list`
- `lint`
- `logs`
- `env`
- `guardian`
- `incident`
- `proof`
- `secrets`
- `version`

The root command wiring lives in `cmd/torque/main.go`.

Legacy/experimental commands (`analyze`, `up`, `wait`) remain callable for compatibility but are hidden from the focused CLI surface.

## Companion Binaries
- `verifier`: standalone policy verifier. The older `verify` binary remains as a compatibility shim for existing CI scripts.
- `torque-package`: chart archive helper built from `cmd/package`; the old standalone name `package` is no longer used in repo build/package outputs.

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

- Purpose: translate between internal runtime structs and protobuf API types (`pkg/api/torque/api/v1`).
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
- Key types: `InstallOptions`/`InstallResult`, `TemplateOptions`/`TemplateResult`, `ServerDryRunReport`, `StreamBroadcaster`, `StreamEvent`, `ResourceTracker`, `ResourceStatus`.
- Invariants: observers are optional and must not block the core deploy loop; events should remain stable for UI consumers.

### `internal/deployplan`

- Purpose: shared apply-plan rendering helpers for `torque apply plan`.
- Key types: `ResourceKey`, `ManifestDoc`, `QuotaReport`, `QuotaHeadroom`.
- Invariants: manifest parsing, graph node IDs, manifest blob/diff rendering, and quota/headroom calculations stay here rather than being copied into command packages.

### `internal/secretstore`

- Purpose: resolve `secret://` references in deploy-time values using pluggable providers.
- Key types: `Resolver`, `Config`, `Provider`.
- Invariants: never log secret values; audit references only.

### `internal/securityevidence`

- Purpose: write security evidence bundles that connect verifier findings, secret-flow scan reports, and redaction proof artifacts.
- Key types: `BundleManifest`, `BundleOptions`.
- Invariants: bundle artifacts contain redacted previews and counts only; raw secret values must not be stored.

### `internal/terraformadapter`

- Purpose: run Terraform/OpenTofu provider resources behind the stack `module.resource` lifecycle.
- Key entrypoints: `terraformadapter.Run` via the hidden `torque terraform-adapter` module command.
- Invariants: `plan` writes saved plan metadata; `apply`/`delete` only execute the exact saved plan after node, command, intent, config, and plan digest checks; Terraform plan/state contents stay in `.torque/terraform` and are not copied into stack audit artifacts.

### Guardian Runtime Proof

- Purpose: top-level `torque guardian` command for observe-only runtime proof.
- Key surfaces: `guardian install`, `guardian report`, `guardian diff`, `guardian pr`.
- Invariants: Guardian is observe-only; installed RBAC grants only `get`, `list`, and `watch`; drift, event, and secret-boundary evidence must redact secret-like strings.

### Incident Replay Proof

- Purpose: top-level `torque incident` command for observe-only incident capture, replay, explanation, and PR artifacts.
- Key surfaces: `incident capture`, `incident replay`, `incident explain`, `incident pr`.
- Invariants: Incident commands do not mutate clusters; captures must redact secret-like strings and omit Kubernetes Secret `data`/`stringData`; replay writes portable proof files only.

### Release Proof Graph

- Purpose: top-level `torque proof` command for building, signing, verifying, and diffing release evidence graphs.
- Key surfaces: `proof graph`, `proof verify`, `proof diff`, `proof gate`, `proof attest`.
- Invariants: proof graphs store artifact paths, hashes, statuses, image digests, and optional ed25519 signatures; gates evaluate policy against verified graph evidence; attestations sign compact release verdicts; proof outputs do not inline raw SQLite captures, logs, manifests, or secret values.

### Agent-Safe Operations

- Purpose: top-level `torque agent` command for proof-backed authorization of AI and automation operations.
- Key surfaces: `agent policy check`, `agent run`.
- Invariants: mutating operations require explicit allow-list permission and a passing proof gate; request release and namespace must match proof metadata when both are present; `agent run` is non-mutating and writes an authorization record only.

### Release Score

- Purpose: top-level `torque release` command for scoring release readiness from proof graph and gate evidence.
- Key surfaces: `release score`, `release autopilot`, `release promote`.
- Invariants: scores are derived from verified graph evidence and failed gate checks; `--fail-below` is the CI/release guard path; score output must remain machine-readable for PRs and release notes.

### Progressive Promotion

- Purpose: proof-backed canary and blue/green promotion decisions.
- Key surfaces: `release promote --strategy canary`, `release promote --strategy blue-green`, `--provider kubernetes`, `--provider argo-rollouts`.
- Invariants: promotion evaluates gate, score, flight, SLO/smoke evidence, and agent authorization before writing traffic-provider state; default provider is non-mutating evidence mode; `--execute` requires `--yes`; mutating providers must record actions and touched objects in provider state before that state is attached to the promoted graph.

### Release Autopilot

- Purpose: compose the proof-backed release path into one artifact-producing command.
- Key surfaces: `release autopilot`.
- Invariants: default mode is non-mutating and operates on an existing proof source; `--execute` requires `--yes`; every run writes graph, HTML, gate, score, flight, replay, explain, agent authorization, and optional attestation artifacts before deciding pass/fail.

### Release Flight Recorder

- Purpose: top-level `torque flight` command for turning a proof graph into a portable release timeline.
- Key surfaces: `flight record`, `flight replay`, `flight explain`.
- Invariants: flight files are read-only evidence artifacts; replay validates timeline shape and graph digest; explain summarizes evidence phases without touching a cluster.

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
- Notes: progress observers emit cache diagnostics and a post-build “cache intelligence” report (input diffs, cache key/graph diffs, slow steps, and OCI layer size rollups when an OCI layout is produced). First-class S3 cache options are normalized here before they become BuildKit `cache-from`/`cache-to` entries.

### `internal/agent`

- Purpose: gRPC server implementation for the remote agent.
- Key types: `Server` (`Run`), per-service handlers (`LogServer`, `BuildServer`, `DeployServer`, `MirrorServer`, `StackServer`).
- Invariants: agent handlers forward events via existing observer interfaces (don’t duplicate deploy/build logic inside the agent).

### `internal/mcpserver`

- Purpose: MCP adapter for agent-facing Torque workflows over stdio/HTTP, with optional remote gRPC bridge mode.
- Key entrypoints: `Server` tool/resource/prompt handlers, session store, root/path policy checks.
- Invariants: MCP handlers validate schemas and enforce MCP roots, then delegate to local Torque commands or existing `torque-agent` gRPC services; never log or return remote tokens.

## Agent-Facing Docs

- Golden paths + validation commands: `docs/agent-playbook.md`
- UI design system (HTML/CSS surfaces): `DESIGN.md`
- gRPC agent API: `docs/grpc-agent.md`
- MCP server design: `docs/mcp-server-spec.md`
- Evidence-first secrets and verifier spec: `docs/secrets-verifier-evidence-spec.md`
- Release proof graph contract: `docs/proof-graph.md`
- S3 BuildKit cache: `docs/s3-build-cache.md`
- Generated package dependency map: `docs/deps.md` (refresh with `make deps`)
