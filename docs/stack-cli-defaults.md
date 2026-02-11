# `ktl stack` minimal-flags workflow

Goal: run `ktl stack ...` with as few flags as possible by moving defaults into `stack.yaml` and environment variables.

## Precedence

For `ktl stack` configuration, precedence is:

1. CLI flags (explicit invocation)
2. `KTL_STACK_*` environment variables
3. `stack.yaml` `cli:` block (with optional `profiles.<name>.cli:` overrides)
4. Built-in defaults (kept for backward compatibility)

## `stack.yaml` `cli:` schema (v1)

Example:

```yaml
cli:
  output: json            # table|json (plan/runs)
  inferDeps: false
  inferConfigRefs: false
  selector:
    clusters: ["dev"]
    tags: ["team-a"]
    fromPaths: ["apps/"]
    releases: ["payments"]
    gitRange: "origin/main...HEAD"
    includeDeps: true
    includeDependents: false
    allowMissingDeps: false
  apply:
    dryRun: true
    diff: false
  delete:
    confirmThreshold: 50
  resume:
    allowDrift: false
    rerunFailed: true
```

Notes:

- `profiles.<name>.cli` merges on top of `cli` (so you can keep prod/stage/dev behaviors separate).
- Runner controls (concurrency, limits, adaptive behavior) remain under `runner:` (not `cli:`).
- Kubernetes-only health gates live under `defaults.verify` / `releases[].verify` (see `docs/stack-verify.md`).

## Recommended minimal invocation

Typical “no flags”:

```bash
export KTL_STACK_ROOT=/path/to/stack
ktl stack plan
ktl stack apply --yes
```

Override selection without flags:

```bash
export KTL_STACK_TAG=team-b
ktl stack plan
```

## Environment variables

Run `ktl env --match stack` to list the `KTL_STACK_*` variables and their descriptions.
