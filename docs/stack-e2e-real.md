# `ktl stack` real-cluster e2e plan

This plan verifies `ktl stack` end-to-end against a real Kubernetes cluster, using the safe fixtures under `testdata/stack/e2e` (ConfigMaps only).

## Safety

- The runner copies fixtures to a temp directory and runs from there (it does not write into `testdata/`).
- It creates the target namespace if missing.
- It installs/uninstalls Helm releases (ConfigMaps only), but it still changes cluster state.
- It requires `KTL_STACK_E2E_CONFIRM=1` to run.

## Prereqs

- `kubectl` in `PATH`
- A working kubeconfig for a non-production-ish cluster
- Repo build works (`make build`)

## Run

```bash
export KUBECONFIG_PATH="$HOME/.kube/archimedes.yaml"
export KTL_STACK_E2E_NAMESPACE="ktl-stack-e2e"
export KTL_STACK_E2E_CONFIRM=1

./scripts/stack-e2e-real.sh
```

Optional:

- `KUBE_CONTEXT=<context>` to pin context
- `ITERATIONS=3` to repeat the suite

## Coverage (what gets exercised)

Per success fixture (`01-...` through `10-...`):

- `ktl stack plan` in `table` + `json` output modes (selection reasons included when applicable)
- `ktl stack graph` in `dot` + `mermaid`
- `ktl stack explain` by ID and by name (`--why`)
- `ktl stack apply`:
  - `--dry-run`
  - `--diff`
  - `--concurrency` + `--progressive-concurrency`
  - `--retry`
- `ktl stack status`:
  - `--format raw|table|json`
  - `--follow` for sqlite-backed runs (follows until it observes `RUN_COMPLETED`, then stops)
- `ktl stack runs` (`table`)
- resume flows:
  - `ktl stack rerun-failed`
  - drift detection on `--resume` (expects failure without `--allow-drift`, then success with it)
- sealing flows:
  - `ktl stack seal --bundle`
  - `ktl stack apply --sealed-dir`
- `ktl stack delete` with concurrency controls

Expected-failure fixtures:

- `x1-cycle-detect`: `ktl stack plan` must fail
- `x2-missing-dep`: `ktl stack plan` must fail

Selection feature checks:

- `--allow-missing-deps` when selecting a dependent without its dependency
- `--include-deps` / `--include-dependents`
- Fixture-specific selectors:
  - `08-tags-selection`: `--tag team-a`
  - `09-from-path-selection`: `--from-path apps/`

## Notes

- This suite focuses on real cluster correctness and UX surfaces. It is intentionally conservative and does not attempt lock contention or takeover scenarios.
- If you need to validate git-range selection behavior (`--git-range`), add an additional dedicated step that runs inside a temporary git repo; the real-cluster suite keeps cluster-facing operations and git behavior separate.

