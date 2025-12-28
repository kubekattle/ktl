# Stack e2e fixtures

These fixtures are used by `scripts/stack-e2e-suite.sh` to exercise `ktl stack` end-to-end.

- Success fixtures live under `testdata/stack/e2e/01-...` through `testdata/stack/e2e/10-...`.
- Expected-failure fixtures live under `testdata/stack/e2e/x1-...` etc and should fail at `ktl stack plan`.

All fixtures target a single namespace (`KTL_STACK_E2E_NAMESPACE`, default `ktl-stack-e2e`) and are safe (ConfigMaps only).

These fixtures may also include `stack.yaml` `cli:` defaults so the real-cluster suite can exercise the “minimal flags” flow. See `docs/stack-cli-defaults.md`.
