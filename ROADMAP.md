# ktl Roadmap

This roadmap is focused on making `ktl` agent-grade: every long-running operation is queryable, replayable, and easy to embed into IDEs and browser UIs.

## North Star: Session-Centric Control Plane

- Every operation accepts/emits a `session_id` (build/apply/delete/verify/logs).
- Sessions are durable (DB-backed), replayable, and exportable (NDJSON/HTML).
- gRPC is the primary API for agents; HTTP+SSE is the lowest-friction surface for IDE/browser clients.

## Near-Term Milestones

### 1) Durable Sessions (gRPC + Store)

- Make `session_id` the universal correlation key across all RPCs.
- Per-session metadata and tags: `cluster`, `namespace`, `release`, `command`, `args`, `requester`, `repo`.
- Session lifecycle status: `running` -> `done`/`error` (exit code + error message).
- `GetSession` + richer `ListSessions` filtering (by meta/tags/state/last_seen).

### 2) Retention & Hygiene

- Retention knobs: max sessions, max frames, max DB bytes.
- Scheduled pruning and `DeleteSession`.
- Add a "pin/protect" affordance (tag or explicit field) so important sessions are not pruned.

### 3) Security Hardening

- TLS and optional mTLS for `ktl-agent` (token auth stays supported).
- Explicit server identity (`--remote-tls-server-name`) and safe defaults for clients.
- Audit-friendly metadata (who ran what, against which cluster/namespace/release), without leaking secrets.

### 4) Browser + IDE Gateway (HTTP + SSE)

- HTTP endpoints for: session list, session detail, session export (NDJSON), and live tail (SSE).
- Harden SSE tail for real IDE/browser use:
  - Resume via `Last-Event-ID` (or `last_event_id` query param).
  - `retry:` hints and tunable heartbeat.
  - Backpressure semantics: block or emit an explicit "dropped frames" marker with a missing-range.
- Browser auth constraints:
  - Native `EventSource` cannot set `Authorization` headers, so support cookie-based auth.
  - Keep `?token=` as an explicit dev-only option (off by default).

### 5) Editor Integration (LSP)

Build a small `ktl` LSP server that bridges to `ktl-agent`:

- `workspace/executeCommand` for `ktl` actions (apply/build/logs/verify), returning/attaching to a `session_id`.
- Stream progress through LSP progress (`workDoneProgress`) and logs via `window/logMessage`.
- Publish diagnostics into workspace files (`.ktl.yaml`, Helm values, rendered manifest outputs) with file:line mapping.
- Provide CodeLens-style actions that open a session replay URL in the browser UI.

## Longer-Term Differentiators

- Session bundles: export a self-contained artifact (frames + metadata + plan artifacts + diffs) for CI/PR attachments.
- Multi-session comparison: diff two sessions (timings/resources/errors) to spot regressions.
- Bidirectional control: cancel, pause-on-warning, approvals/interactive prompts for "agent runs" (CI + IDE).
