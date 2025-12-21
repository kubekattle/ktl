# ktl build policy demo (OPA/Rego)

This demo policy shows how `ktl build --policy` can fail fast on laptops and in CI.

## Run

```bash
ktl build . \
  --attest-dir dist/attest \
  --policy ./examples/policy/demo \
  --policy-mode enforce
```

## What it enforces

- Base images must be pinned (`FROM ...@sha256:...`), unless explicitly allowed.
- Registries can be blocked by prefix.
- Required OCI labels must be present in the Dockerfile `LABEL` instructions.

The policy reads from `input`:

- `input.bases`: base images from `FROM` lines.
- `input.labels`: labels parsed from `LABEL` lines.
- `input.tags`: requested image tags.
- `input.files`: JSON files under `--attest-dir` (attestations + ktl artifacts).
- `input.external`: `ktl-external-fetches.json` (best-effort).
- `input.data`: content of `data.json` in the policy bundle.

