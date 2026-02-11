# Sandbox demo fixtures

These fixtures are used by `scripts/sandbox-demo.sh` to demonstrate the difference between:

- running `ktl build` *without* the `ktl` sandbox (`KTL_SANDBOX_DISABLE=1`), and
- running `ktl build` *with* the `ktl` sandbox (default when a sandbox runtime is present on Linux).

They are intentionally non-destructive and do not read sensitive host paths.

