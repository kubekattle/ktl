# `ktl stack` verify (Kubernetes-only health gates)

`ktl stack` can optionally run a post-apply **verify** phase per release. Verification is Kubernetes-only (no Prometheus): it uses workload readiness signals plus recent Warning events tied to the release manifest inventory.

## Configure in `stack.yaml`

You can set verify defaults for the whole stack and override per release.

```yaml
defaults:
  namespace: platform
  verify:
    enabled: true
    failOnWarnings: true
    warnOnly: false
    eventsWindow: 15m
    timeout: 2m
    denyReasons: ["FailedMount","FailedScheduling","BackOff","ImagePullBackOff","ErrImagePull"]
    # allowReasons: ["KtlVerifyDemo"]   # optional allowlist (case-insensitive)
    # requireConditions:
    #   - group: example.com
    #     kind: Widget
    #     type: Ready
    #     status: "True"
    #     allowMissing: false

releases:
  - name: api
    chart: ./charts/app
    verify:
      enabled: true
```

## Semantics

- `enabled`: if false/omitted, no verification is performed.
- `failOnWarnings`: when enabled, the verify phase fails the release if it sees recent `type=Warning` events whose `involvedObject` matches a resource from the release manifest.
- `warnOnly`: if true, verification never fails the release (it records findings in the run stream).
- `eventsWindow`: limits how far back warning events are considered (prevents old noisy events from failing new runs). Default is `15m`.
- Event watermarking: when the sqlite run store is enabled, ktl records the Events list `resourceVersion` per namespace after successful verifies and uses it as a best-effort watermark to ignore old Warning events on subsequent runs (falls back to the time window when RVs are missing/unparseable).
- `timeout`: bounds how long verify may run for this release. Default is `2m`.
- `denyReasons` / `allowReasons`: optional filters for `involvedObject` warning event reasons (case-insensitive).
- `requireConditions`: optional enforcement of `status.conditions` on matching custom resources (CRs).

## Output

Verification is recorded as a regular node phase named `verify` and is persisted in the sqlite run store, so it appears in:

- `ktl stack status --follow`
- `ktl stack audit`
