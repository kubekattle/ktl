# ktl build secrets guardrails demo

This demo intentionally bakes a token into an image layer so `ktl build` can detect it.

## Run

```bash
ktl build ./examples/secrets/demo \
  --attest-dir dist/attest-secrets \
  --secrets warn
```

Using a custom config (Trivy-like):

```bash
ktl build ./examples/secrets/demo \
  --attest-dir dist/attest-secrets \
  --secrets warn \
  --secrets-config ./examples/secrets/config/default.yaml
```

To ratchet to strict mode:

```bash
ktl build ./examples/secrets/demo \
  --attest-dir dist/attest-secrets \
  --secrets block \
  --secrets-config ./examples/secrets/config/strict.yaml
```

To block the build on findings:

```bash
ktl build ./examples/secrets/demo \
  --attest-dir dist/attest-secrets \
  --secrets block
```

## Build-arg preflight demo

```bash
export NPM_TOKEN="ghp_0123456789abcdef0123456789abcdef0123"
ktl build . --build-arg NPM_TOKEN=$NPM_TOKEN --secrets warn
```

Expected guidance: use `--secret NPM_TOKEN` and mount it via BuildKit instead of passing it as a build arg.
