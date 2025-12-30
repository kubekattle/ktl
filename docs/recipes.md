# Recipes (golden paths)

Copy/paste workflows that cover the common “happy paths” for `ktl`.

## Apply a chart (with and without the UI)

```bash
# Preview what will change
ktl apply plan --chart ./chart --release foo -n default

# Deploy
ktl apply --chart ./chart --release foo -n default

# Deploy with the viewer UI
ktl apply --chart ./chart --release foo -n default --ui
```

## Share an `apply plan` visualization

```bash
ktl apply plan --visualize --chart ./chart --release foo -n default
```

## Stack: minimal-flags workflow (plan → apply)

```bash
export KTL_STACK_ROOT=./stacks/prod

# Read-only plan (default `ktl stack` behaves like `ktl stack plan`)
ktl stack

# Execute (DAG order)
ktl stack apply --yes
```

## Stack: resume / rerun failed

```bash
export KTL_STACK_ROOT=./stacks/prod

# Resume the most recent run (frozen plan unless --replan is set)
ktl stack apply --resume --yes

# Convenience: resume and schedule only failed nodes
ktl stack rerun-failed --yes
```

## Stack: inspect runs

```bash
export KTL_STACK_ROOT=./stacks/prod

ktl stack runs --limit 50
ktl stack status --follow
ktl stack audit --output html > stack-audit.html
```

## Build: share the build stream over WebSocket

```bash
ktl build --context . --tag ghcr.io/acme/app:dev --ws-listen :9085
```

## Verify: validate a chart render in CI

```bash
cat > verify-chart-render.yaml <<'YAML'
version: v1

target:
  kind: chart
  chart:
    chart: ./chart
    release: foo
    namespace: default
    useCluster: false

verify:
  mode: block
  failOn: high

output:
  format: table
  report: "-"
YAML

ktl verify verify-chart-render.yaml
```

