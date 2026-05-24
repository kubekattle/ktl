# Configuration atlas

Practical, copy/paste-friendly examples for the configs that power `torque`.

This is intentionally biased toward “what do I put in the file?” rather than exhaustive schema docs.

## torque config (`.torque.yaml` / `~/.torque/config.yaml`)

Use the repo or global config file to set deploy-time secret providers or build defaults.

```yaml
# .torque.yaml
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
  #   # kubernetesRole: torque
  #   # kubernetesTokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
  #   # awsRole: torque
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
      kubernetesRole: torque
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
      awsRole: torque
      awsRegion: us-east-1
      awsHeaderValue: vault.example.com
      mount: secret
      kvVersion: 2
```

## `stack.yaml` (minimal, with CLI defaults)

This is the “minimal-flags” stack workflow: keep defaults in `stack.yaml` under `cli:` and override with `TORQUE_STACK_*` only when needed.

```yaml
# stack.yaml
name: prod

# Defaults applied to all stack nodes unless overridden.
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

  # Runner behavior (how nodes are scheduled/executed).
  runner:
    concurrency: 6
    progressiveConcurrency: true

# CLI defaults for `torque stack ...` so you can run with fewer flags.
# Precedence: flags > TORQUE_STACK_* env > stack.yaml cli > built-in defaults.
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

nodes:
  - name: api
    kind: release.helm
    chart: ./charts/app
    values: ["./values/api.yaml"]
    tags: ["critical", "team-payments"]

  - name: worker
    kind: release.helm
    chart: ./charts/app
    values: ["./values/worker.yaml"]
    tags: ["team-payments"]
    # Override verify settings per node.
    verify:
      enabled: false

  - name: host-preflight
    kind: host.command.run
    input:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      command: "systemctl is-active nginx"

  - name: cluster-inspect
    kind: k8s.cluster.inspect
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_SSH
        kubeconfig: /etc/rancher/k3s/k3s.yaml
        namespaces: [kube-system]

  - name: cert-inspect
    kind: k8s.cert.inspect
    needs: [cluster-inspect]
    kubernetes:
      provider: auto
      certificates:
        renewBefore: 720h
        order: control-plane-first
        batchSize: 1
        targetsFrom:
          sourceNode: cluster-inspect
          roles: [control-plane]
          transport: ssh
          targetTemplate: "ssh://root@{{ .InternalIP }}"

  - name: cert-renew
    kind: k8s.cert.renew
    needs: [cert-inspect]
    kubernetes:
      provider: auto
      certificates:
        renewBefore: 720h
        forceOnceId: 2026-q2-cert-renewal
        statePath: /var/lib/torque/cluster-lifecycle/cert-renewal.json
        order: control-plane-first
        batchSize: 1
        policy:
          maxUnavailable: 1
          requireFreshInspect: true
          maxInspectAge: 15m
          requireHealthyInspect: true
          requireSupportedProvider: true
          override:
            reason: "Emergency renewal approved by CAB"
            changeId: CHG-2026-1234
            approver: sre-lead@example.com
            expiresAt: "2026-12-31T04:00:00Z"
            scope:
              nodeId: k8s.cert.renew/cert-renew
              intentDigest: sha256:<planned-node-intent>
              targetIds: [cp-1, cp-2, cp-3]
        targetsFrom:
          sourceNode: cluster-inspect
          roles: [control-plane]
          transport: ssh
          targetTemplate: "ssh://root@{{ .InternalIP }}"

  - name: cluster-verify
    kind: k8s.cluster.verify
    needs: [cert-renew]
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_SSH
        kubeconfig: /etc/rancher/k3s/k3s.yaml
        minReadyNodes: 3
        namespaces: [kube-system]
        appProbes:
          - id: ingress-health
            command: "curl -fsS http://127.0.0.1/healthz"
            expect: ok
```

Notes:
- `torque stack` is read-only by default (prints a plan); use `torque stack apply` / `torque stack delete` to execute.
- `releases:` remains accepted as a backward-compatible alias for Helm nodes; entries default to `kind: release.helm`.
- `k8s.cluster.inspect` uses kubectl to emit normalized API, provider, node topology, namespace, core-pod, and certificate-renewal capability evidence.
- `k8s.cert.inspect` and `k8s.cert.renew` support `provider: auto`, `kubeadm`, `k3s`, `rke2`, or `custom` with explicit inspect/renew commands, dynamic `targetsFrom` wiring from cluster inspect evidence, target checkpoints, rolling `order` / `batchSize` controls, and optional lifecycle policy gates (`maxUnavailable`, fresh/healthy inspect, supported provider, maintenance windows, app probes). The supported-provider policy records both the raw inspect hint and the effective renewal provider, so explicit custom commands can be approved without pretending the cluster distribution was auto-detected.
- Policy overrides require both `torque stack apply --policy-override` and scoped stackfile approval evidence. Torque records `k8s-lifecycle-policy-override.json` and blocks expired approvals, wrong node scope, changed intent digests, or target-set drift.
- `k8s.cluster.verify` runs a generic post-maintenance health gate: API stability checks, node readiness, namespace pod readiness, and optional app probes. It also emits `k8s-lifecycle-summary.json`, which links inspect, policy, override, derived target, certificate, checkpoint, verify, and app-probe evidence back to source artifact digests for stack audit/export review. When both pre-mutation policy probes and post-maintenance verify probes are configured, the summary includes an `applicationGate` section proving application availability before and after maintenance.
- Profile overlays: use `profiles.<name>.cli` and `profiles.<name>.defaults` to override per environment (dev/stage/prod).
- For CLI schema details, see `docs/stack-cli-defaults.md`.

## Verifier YAML (chart render + live checks)

`verifier` supports multiple targets. Two common ones:

Tip: generate a starter config with `verifier init chart|namespace` and then customize it.

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
  securityProfile: enterprise
  securityBoundaryMatrix: true
  secretFlowGraph: true
  securityEvidence: ./torque-security-evidence
  secrets:
    report: ./secrets.json

output:
  format: table      # table|json|sarif|html|md
  report: "-"        # "-" stdout, or a path
```

Tip: CLI overrides are available for baselines:
```bash
verifier verify.yaml --baseline ./baseline.json
verifier verify.yaml --compare-to ./baseline.json
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

Sandbox policies live under `sandbox/` and are selected via `TORQUE_SANDBOX_CONFIG` (or `--sandbox-config`).

Example (CI-like policy):

```bash
export TORQUE_SANDBOX_CONFIG="$(pwd)/sandbox/linux-ci.cfg"
torque build . --tag ghcr.io/acme/app:dev
```

What matters most in a policy:
- `name`/`hostname` so logs clearly identify the profile in use.
- `clone_new*` namespace settings (user/pid/cgroup isolation vs compatibility).
- `rlimit_*` ceilings (tmpfs sizes, file size, nproc, etc.).
- `mount` blocks (what is visible in the sandbox).

For threat model + guidance, see `docs/sandbox-security.md`.
