# gRPC Agent API (ktl-agent)

`ktl-agent` exposes `ktl` capabilities over gRPC for automation and AI agents:

- Logs: `LogService.StreamLogs`
- Builds: `BuildService.RunBuild`
- Deploy: `DeployService.Apply` / `DeployService.Destroy`
- Verify: `VerifyService.Verify`
- Mirror bus: `MirrorService.Publish` / `MirrorService.Subscribe`
- Agent metadata: `AgentInfoService.GetInfo`

The API definitions live in `proto/ktl/api/v1/agent.proto`.

## Running ktl-agent

```bash
go install ./cmd/ktl-agent

# Insecure gRPC by default (plaintext). Prefer SSH tunnels or a private network.
ktl-agent -listen :7443 -kubeconfig ~/.kube/config -context <ctx>

# Optional auth token (required for all RPCs when set).
ktl-agent -listen :7443 -token "$KTL_REMOTE_TOKEN" -kubeconfig ~/.kube/config -context <ctx>

# Optional MirrorService flight recorder (durable sessions + ListSessions/Export).
ktl-agent -listen :7443 -mirror-store ~/.ktl/agent/mirror.sqlite -kubeconfig ~/.kube/config -context <ctx>

# Optional retention knobs for the flight recorder.
ktl-agent -listen :7443 -mirror-store ~/.ktl/agent/mirror.sqlite \
  -mirror-max-sessions 200 -mirror-max-frames 5000 -mirror-max-bytes 1000000000

# Optional HTTP gateway for browser UIs (same auth token as gRPC).
ktl-agent -listen :7443 -http-listen :8081 -mirror-store ~/.ktl/agent/mirror.sqlite

# Optional TLS (and mTLS).
ktl-agent -listen :7443 -tls-cert ./server.crt -tls-key ./server.key
ktl-agent -listen :7443 -tls-cert ./server.crt -tls-key ./server.key -tls-client-ca ./client-ca.crt
```

## Introspection (reflection)

The agent enables gRPC reflection so dynamic clients can discover the API at runtime.

If you have `grpcurl` installed:

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" 127.0.0.1:7443 list
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" 127.0.0.1:7443 list ktl.api.v1
```

If the agent is running with TLS, omit `-plaintext` and pass a CA bundle instead:

```bash
grpcurl -cacert ./ca.crt -H "authorization: Bearer $KTL_REMOTE_TOKEN" 127.0.0.1:7443 list
```

If the agent requires mTLS (`-tls-client-ca`), also pass a client cert/key:

```bash
grpcurl -cacert ./ca.crt -cert ./client.crt -key ./client.key \
  -H "authorization: Bearer $KTL_REMOTE_TOKEN" 127.0.0.1:7443 list
```

## Health checks

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  127.0.0.1:7443 grpc.health.v1.Health/Check
```

## Agent info

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  127.0.0.1:7443 ktl.api.v1.AgentInfoService/GetInfo
```

## Mirror Flight Recorder (sessions)

When `-mirror-store` is set, `ktl-agent` persists `MirrorService` frames to SQLite and exposes session metadata:

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  127.0.0.1:7443 ktl.api.v1.MirrorService/ListSessions
```

List sessions also supports query filters (meta/tags/state/last-seen window), for example:

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  -d '{"limit":200,"meta":{"namespace":"prod","release":"checkout"},"state":"MIRROR_SESSION_STATE_RUNNING"}' \
  127.0.0.1:7443 ktl.api.v1.MirrorService/ListSessions
```

Get a single session (metadata + latest cursor):

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>"}' \
  127.0.0.1:7443 ktl.api.v1.MirrorService/GetSession
```

Set session metadata/tags (useful for IDEs/UIs):

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>","meta":{"command":"ktl logs","args":["checkout-.*","--namespace","prod"],"requester":"me@host"},"tags":{"team":"infra"}}' \
  127.0.0.1:7443 ktl.api.v1.MirrorService/SetSessionMeta
```

Set session lifecycle status (optional; `ktl-agent` also sets this automatically for built-in streaming RPCs when `session_id` is provided):

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>","status":{"state":"MIRROR_SESSION_STATE_DONE","exit_code":0,"completed_unix_nano":123}}' \
  127.0.0.1:7443 ktl.api.v1.MirrorService/SetSessionStatus
```

Delete a session:

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>"}' \
  127.0.0.1:7443 ktl.api.v1.MirrorService/DeleteSession
```

You can export a session as JSONL (one `MirrorFrame` per line, with `sequence` and `received_unix_nano` set):

```bash
grpcurl -plaintext -H "authorization: Bearer $KTL_REMOTE_TOKEN" \
  -d '{"session_id":"<session-id>","format":"jsonl"}' \
  127.0.0.1:7443 ktl.api.v1.MirrorService/Export
```

## HTTP Gateway (Browser UIs)

When `-http-listen` is set, `ktl-agent` exposes a tiny HTTP API that mirrors the MirrorService session surface:

- `POST /api/v1/auth/cookie` (sets an HttpOnly `ktl_token` cookie; useful for native browser `EventSource`)
- `DELETE /api/v1/auth/cookie` (clears the cookie)
- `GET /api/v1/mirror/sessions?limit=200`
- `GET /api/v1/mirror/sessions?limit=200&namespace=prod&release=checkout&state=running`
- `GET /api/v1/mirror/sessions/<session-id>`
- `GET /api/v1/mirror/sessions/<session-id>/export?from_sequence=1` (JSONL)
- `GET /api/v1/mirror/sessions/<session-id>/tail?from_sequence=1&replay=1` (SSE: `event: frame` per `MirrorFrame`)
  - Resume: send `Last-Event-ID: <sequence>` (or `?last_event_id=<sequence>`)
  - Tuning: `?heartbeat=15s` (or `heartbeat_ms=15000`), `?retry_ms=1000`
  - Backpressure: if frames cannot be replayed (retention, slow consumer, etc.), the stream emits `event: dropped` with a JSON payload describing the missing sequence range.

Authentication uses the same headers as gRPC (`authorization: Bearer ...` or `x-ktl-token: ...`), or the `ktl_token` cookie set by `POST /api/v1/auth/cookie`.

## Session IDs

For agent/IDE integrations, treat `session_id` as the cross-RPC correlation key:

- Send `session_id` on `BuildService.RunBuild`, `LogService.StreamLogs`, `DeployService.Apply`/`Destroy`, and `VerifyService.Verify` to have the agent mirror those streams into `MirrorService` (so multiple subscribers can replay/tail the same session).
- `MirrorService.Publish` also records inbound frames with the same `session_id` and a server-assigned `sequence`.

## Client auth header

When `ktl-agent -token ...` is set, clients must send one of:

- `authorization: Bearer <token>`
- `x-ktl-token: <token>`

## ktl Client TLS Flags

When the agent runs with TLS (`-tls-cert/-tls-key`), `ktl` can be pointed at it with:

```bash
ktl --remote-agent <host:port> --remote-tls --remote-tls-ca ./ca.crt --remote-token "$KTL_REMOTE_TOKEN" logs ...
ktl --remote-agent <host:port> --remote-tls --remote-tls-insecure-skip-verify logs ...
ktl --remote-agent <host:port> --remote-tls --remote-tls-server-name <name> logs ...
ktl --remote-agent <host:port> --remote-tls --remote-tls-ca ./ca.crt \
  --remote-tls-client-cert ./client.crt --remote-tls-client-key ./client.key \
  --remote-token "$KTL_REMOTE_TOKEN" logs ...
```
