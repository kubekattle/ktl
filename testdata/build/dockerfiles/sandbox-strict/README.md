# sandbox-strict build fixture

This context is used to smoke-test `ktl build` while running under a stricter
nsjail policy (`sandbox/linux-strict.cfg`).

Run:

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/sandbox/linux-strict.cfg"
ktl build ./testdata/build/dockerfiles/sandbox-strict --no-cache --tag ktl.local/sandbox-strict:dev
```

Expected:

- `ktl build` prints the sandbox banner (it re-execs inside nsjail).
- The build succeeds and produces the image tag.

To demonstrate host-path isolation directly, run:

```bash
go test -tags=integration ./cmd/ktl -run TestSandboxBlocksUnboundHostPaths
```
