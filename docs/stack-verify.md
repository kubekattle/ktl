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
    eventsWindow: 15m

releases:
  - name: api
    chart: ./charts/app
    verify:
      enabled: true
```

## Semantics

- `enabled`: if false/omitted, no verification is performed.
- `failOnWarnings`: when enabled, the verify phase fails the release if it sees recent `type=Warning` events whose `involvedObject` matches a resource from the release manifest.
- `eventsWindow`: limits how far back warning events are considered (prevents old noisy events from failing new runs). Default is `15m`.

## Output

Verification is recorded as a regular node phase named `verify` and is persisted in the sqlite run store, so it appears in:

- `ktl stack status --follow`
- `ktl stack audit`

