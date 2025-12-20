# Sandbox Profiles

This directory contains versioned nsjail policy files for `ktl build`.

## Strict profile

`testdata/sandbox/linux-strict.cfg` is a more restrictive starting point than
the embedded default. It aims to:

- Use additional namespaces (`user`, `pid`, `cgroup`) where available.
- Drop Linux capabilities (`keep_caps: false`).
- Avoid mounting sysfs by default.

### Quick demo

On a Linux host with `nsjail` installed:

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/testdata/sandbox/linux-strict.cfg"
ktl build ./testdata/build/dockerfiles/sandbox-strict --no-cache --tag ktl.local/sandbox-strict:dev
```

To sanity-check path isolation (without involving BuildKit), run the existing
integration test:

```bash
go test -tags=integration ./cmd/ktl -run TestSandboxBlocksUnboundHostPaths
```

