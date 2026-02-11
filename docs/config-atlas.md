# Configuration atlas

Practical, copy/paste-friendly examples for the configs that power `ktl`.

This is intentionally biased toward “what do I put in the file?” rather than exhaustive schema docs.

## ktl config (`.ktl.yaml` / `~/.ktl/config.yaml`)

Use the repo or global config file to set deploy-time secret providers or build defaults.

```yaml
# .ktl.yaml
build:
  profile: ci

secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml

  # Example Vault provider
  # vault:
  #   type: vault
  #   address: https://vault.example.com
  #   authMethod: approle
  #   authMount: approle
  #   roleId: 00000000-0000-0000-0000-000000000000
  #   secretId: s.0000000000000000000000
  #   # kubernetesRole: ktl
  #   # kubernetesTokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
  #   # awsRole: ktl
  #   # awsRegion: us-east-1
  #   # awsHeaderValue: vault.example.com
  #   mount: secret
  #   kvVersion: 2
  #   key: value
```

### Vault auth method examples

AppRole:
```yaml
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
      authMethod: approle
      authMount: approle
      roleId: 00000000-0000-0000-0000-000000000000
      secretId: s.0000000000000000000000
      mount: secret
      kvVersion: 2
```

Kubernetes:
```yaml
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
      authMethod: kubernetes
      authMount: kubernetes
      kubernetesRole: ktl
      kubernetesTokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
      mount: secret
      kvVersion: 2
```

AWS IAM:
```yaml
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
      authMethod: aws
      authMount: aws
      awsRole: ktl
      awsRegion: us-east-1
      awsHeaderValue: vault.example.com
      mount: secret
      kvVersion: 2
```

## `stack.yaml` (minimal, with CLI defaults)

This is the “minimal-flags” stack workflow: keep defaults in `stack.yaml` under `cli:` and override with `KTL_STACK_*` only when needed.

```yaml
# stack.yaml
name: prod

# Defaults applied to all releases unless overridden.
defaults:
  namespace: platform

  # Optional Kubernetes-only post-apply health gates (see docs/stack-verify.md).
  verify:
    enabled: true
    failOnWarnings: true
    warnOnly: false
    eventsWindow: 15m
    timeout: 2m
    denyReasons: ["FailedMount", "FailedScheduling", "ImagePullBackOff", "ErrImagePull", "BackOff"]

  # Runner behavior (how releases are scheduled/executed).
  runner:
    concurrency: 6
    progressiveConcurrency: true

# CLI defaults for `ktl stack ...` so you can run with fewer flags.
# Precedence: flags > KTL_STACK_* env > stack.yaml cli > built-in defaults.
cli:
  output: table
  inferDeps: true
  inferConfigRefs: false
  selector:
    clusters: ["prod-us"]
    tags: ["critical"]
    includeDeps: true
    includeDependents: false
    allowMissingDeps: false
  apply:
    dryRun: false
    diff: true
  delete:
    confirmThreshold: 50
  resume:
    allowDrift: false
    rerunFailed: false

releases:
  - name: api
    chart: ./charts/app
    values: ["./values/api.yaml"]
    tags: ["critical", "team-payments"]

  - name: worker
    chart: ./charts/app
    values: ["./values/worker.yaml"]
    tags: ["team-payments"]
    # Override verify settings per release.
    verify:
      enabled: false
```

Notes:
- `ktl stack` is read-only by default (prints a plan); use `ktl stack apply` / `ktl stack delete` to execute.
- Profile overlays: use `profiles.<name>.cli` and `profiles.<name>.defaults` to override per environment (dev/stage/prod).
- For CLI schema details, see `docs/stack-cli-defaults.md`.

## `verify` YAML (chart render + live checks)

`verify` supports multiple targets. Two common ones:

Tip: generate a starter config with `verify init chart|namespace` and then customize it.

### Verify a chart render (no cluster access)

```yaml
# verify-chart-render.yaml
version: v1

target:
  kind: chart
  chart:
    chart: ./chart
    release: foo
    namespace: default
    values:
      - values.yaml
    set:
      - image.tag=dev
    useCluster: false
    includeCRDs: false

verify:
  mode: block        # warn|block
  failOn: high       # low|medium|high|critical
  selectors:
    include:
      namespaces: ["default"]
    exclude:
      kinds: ["ConfigMap"]
  baseline:
    write: ./baseline.json   # write a JSON baseline snapshot
    read: ./baseline.json    # compare against baseline on next run
    exitOnDelta: true        # fail when new/changed findings appear

output:
  format: table      # table|json|sarif|html|md
  report: "-"        # "-" stdout, or a path
```

Tip: CLI overrides are available for baselines:
```bash
verify verify.yaml --baseline ./baseline.json
verify verify.yaml --compare-to ./baseline.json
```

### Verify a live namespace

```yaml
# verify-namespace.yaml
version: v1

target:
  kind: namespace
  namespace: default

kube:
  context: my-context

verify:
  mode: warn
  failOn: high

output:
  format: table
  report: "-"
```

## Sandbox profiles (`sandbox/*.cfg`)

Sandbox policies live under `sandbox/` and are selected via `KTL_SANDBOX_CONFIG` (or `--sandbox-config`).

Example (CI-like policy):

```bash
export KTL_SANDBOX_CONFIG="$(pwd)/sandbox/linux-ci.cfg"
ktl build --context . --tag ghcr.io/acme/app:dev
```

What matters most in a policy:
- `name`/`hostname` so logs clearly identify the profile in use.
- `clone_new*` namespace settings (user/pid/cgroup isolation vs compatibility).
- `rlimit_*` ceilings (tmpfs sizes, file size, nproc, etc.).
- `mount` blocks (what is visible in the sandbox).

For threat model + guidance, see `docs/sandbox-security.md`.
