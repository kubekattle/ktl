# KTL Architecture (Current)

This repo is a single-module Go CLI with an optional companion agent.

## Layout
- `cmd/ktl`: end-user CLI (Cobra) and CLI-only helpers.
- `cmd/ktl-agent`: gRPC agent used by `--remote-agent` / `--mirror-bus`.
- `internal/*`: non-exported libraries used by the CLI/agent (tailing, capture, deploy/apply, UI mirroring, BuildKit workflows, config/feature flags).
- `pkg/*`: reusable non-`internal` packages (BuildKit/Compose/registry helpers and generated API stubs under `pkg/api/v1`).
- `testdata/*`: fixtures and golden files.

## Main Commands (wired today)
- `logs` (plus `logs capture` and `logs drift`)
- `build`
- `plan`
- `apply`
- `delete`
- `deploy`
- `mirror`
- `completion`

The root command wiring lives in `cmd/ktl/main.go`.

## Key Packages
- Log tailing + capture: `internal/tailer`, `internal/capture`, `internal/sqlitewriter`
- Build/apply/deploy orchestration: `internal/workflows/buildsvc`, `internal/deploy`
- UI mirrors + streaming: `internal/caststream`, `internal/mirrorbus`, `internal/ui`
- Remote agent server: `internal/agent`, protobuf API types: `pkg/api/v1`
