# Recipes (golden paths)

Copy/paste workflows that cover the common “happy paths” for `torque`.

## Zero-conf onboarding

```bash
# Initialize repo defaults and detect your kubecontext
torque init

# Generate a starter stack.yaml from existing cluster state
torque init from-cluster
torque init from-cluster --all-namespaces --dry-run
# Exports installed Helm chart archives by default; add current values too
torque init from-cluster --all-namespaces --write-values

# Run the interactive setup wizard
torque init --interactive

# Scaffold chart/ and values/ plus gitignore entries
torque init --layout --gitignore

# Use an opinionated preset
torque init --preset prod

# Apply an init template (built-in or URL)
torque init --template platform
torque init --template https://example.com/torque-init.yaml

# Scaffold a Vault secrets provider
torque init --secrets-provider vault

# Preview the config without writing
torque init --dry-run

# Generate a replayable init plan
torque init --plan --plan-output .torque/init-plan.json

# Apply a saved init plan
torque init --apply-plan .torque/init-plan.json

# Launch the interactive help UI
torque --help --ui
```

## Apply a chart (with and without the UI)

```bash
# Preview what will change
torque apply plan --chart ./chart --release foo -n default

# Write a PR-ready Markdown summary
torque apply plan --chart ./chart --release foo -n default --github-comment --output plan.md

# Attach verifier and build evidence to the PR summary
verifier --chart ./chart --release foo -n default \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json
torque build . --tag ghcr.io/acme/foo:dev --capture ./build.sqlite
torque apply plan --chart ./chart --release foo -n default \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md

# Simulate live API behavior and write a replayable proof directory
torque apply simulate --chart ./chart --release foo -n default \
  --security-evidence ./torque-security-evidence \
  --out ./torque-sim-proof
torque replay ./torque-sim-proof --lab k3s

# Prove runtime drift after simulation
torque guardian install --namespace torque-system --mode observe
torque guardian report --since 24h --out runtime-proof.json
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
torque guardian pr --from drift-proof.json --branch fix/runtime-drift

# Capture and replay an incident window
torque incident capture --release foo -n default --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof
torque incident explain --from incident-replay-proof --out root-cause.json
torque incident pr --from root-cause.json --branch fix/foo-incident

# Deploy
torque apply --chart ./chart --release foo -n default

# Deploy with the viewer UI
torque apply --chart ./chart --release foo -n default --ui

# Predict rollout risk and write a portable proof bundle
torque apply --chart ./chart --release foo -n default \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes

# Deploy with auto rollback proof and rollout SLO gates
cat > slo.yaml <<'YAML'
apiVersion: torque.ingresslabs.dev/v1alpha1
kind: RolloutSLO
spec:
  minReadyPercent: 100
  maxFailedResources: 0
  maxPendingResources: 0
YAML
torque apply --chart ./chart --release foo -n default \
  --auto-rollback --slo ./slo.yaml \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes

# Turn a failed proof bundle into a repair branch and PR body
torque repair --from ./apply-proof.json --chart ./chart \
  --branch fix/foo-rollout --apply --pr-body ./repair-pr.md --yes
```

## Build → verify → plan → apply

```bash
# Build the image and capture build evidence.
torque build . --tag ghcr.io/acme/foo:dev --capture ./build.sqlite

# Verify the rendered chart.
verifier --chart ./chart --release foo -n default --format json --report verify.json

# Verify with evidence-first secret flow checks and a redaction proof bundle.
verifier --chart ./chart --release foo -n default \
  --security-profile enterprise \
  --security-boundary-matrix \
  --secrets-report secrets.json \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json

# Write a PR-ready plan with verifier and build evidence attached.
torque apply plan --chart ./chart --release foo -n default \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md

# Apply with the verify report enforced, capture the rollout, and explain it.
torque apply --chart ./chart --release foo -n default \
  --require-verified verify.json \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes
torque repair --from ./apply-proof.json --chart ./chart --pr-body ./repair-pr.md
torque explain ./apply.sqlite --format markdown
```

## Agent MCP bridge

```bash
# Run the MCP server over stdio for an IDE or agent host.
torque-mcp --stdio

# Route agent tool calls to a remote Torque node over gRPC.
export TORQUE_REMOTE_TOKEN="$(openssl rand -hex 24)"
torque-agent -listen :7443 -token "$TORQUE_REMOTE_TOKEN"
torque-mcp --stdio \
  --remote-agent 127.0.0.1:7443 \
  --remote-token "$TORQUE_REMOTE_TOKEN"
```

## Durable Linux agent host

```bash
# Install torque, torque-agent, torque-mcp, and systemd units.
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon

# Inspect generated tokens and verify authenticated HTTP MCP.
. /etc/torque/agent.env
systemctl status torque-agent.service torque-mcp.service
curl -fsS -H "authorization: Bearer $TORQUE_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  http://127.0.0.1:7331/mcp
```

Enterprise remote bridge with mTLS:

```bash
export TORQUE_REMOTE_TOKEN="<from secret manager>"
torque-agent -listen 0.0.0.0:7443 -token "$TORQUE_REMOTE_TOKEN" \
  -tls-cert /etc/torque/tls/agent.crt \
  -tls-key /etc/torque/tls/agent.key \
  -tls-client-ca /etc/torque/tls/client-ca.crt \
  -mirror-store /var/lib/torque/agent/mirror.sqlite

torque-mcp --stdio --remote-agent torque-agent.prod.internal:7443 \
  --remote-tls --remote-tls-ca /etc/torque/tls/ca.crt \
  --remote-tls-client-cert /etc/torque/tls/client.crt \
  --remote-tls-client-key /etc/torque/tls/client.key \
  --remote-tls-server-name torque-agent.prod.internal \
  --enable-write
```

## Build and ship with S3 cache

```bash
# Build with native BuildKit S3 cache import/export.
torque build . --tag ghcr.io/acme/foo:dev \
  --s3-cache s3://acme-build-cache/torque/main \
  --s3-cache-region us-east-1

# Forward the same cache settings through the full ship workflow.
torque ship --chart ./chart --release foo -n default --build . \
  --tag ghcr.io/acme/foo:dev \
  --s3-cache s3://acme-build-cache/torque/main \
  --s3-cache-region us-east-1 --yes
```

For MCP agents, use first-class cache tools before build fanout:

```json
{
  "tool": "torque.cache.plan",
  "arguments": {
    "contextDir": ".",
    "dockerfile": "Dockerfile",
    "tags": ["ghcr.io/acme/foo:dev"],
    "changedPaths": ["go.mod", "cmd/foo/main.go"],
    "s3Cache": "s3://acme-build-cache/torque/main",
    "s3CacheRegion": "us-east-1",
    "s3CacheName": "foo-main"
  }
}
```

Warm writes cache exports, so it requires `torque-mcp --enable-write` and
`"safety": {"confirm": true}`. AWS credentials stay on the BuildKit daemon or
workload identity, not in MCP arguments.

## 5-minute demo (public chart)

Do this:
```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

torque apply plan --chart bitnami/nginx --release demo-nginx -n default --visualize
torque apply --chart bitnami/nginx --release demo-nginx -n default --yes
torque delete --release demo-nginx -n default --yes
```

## Recommended `.torque.yaml` layout

Do this:
```yaml
build:
  profile: ci

secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml
```

## Apply with secret references

```bash
# Define providers in .torque.yaml (or pass --secret-config)
cat > .torque.yaml <<'YAML'
secrets:
  defaultProvider: local
  providers:
    local:
      type: file
      path: ./secrets.dev.yaml
YAML

# Use secret:// references in values
cat > values.dev.yaml <<'YAML'
db:
  password: secret://local/db/password
YAML

torque apply plan --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider local
torque apply --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider local
torque stack apply --config ./stacks/prod --secret-provider local --yes
```

## Vault-backed secrets

```bash
cat > .torque.yaml <<'YAML'
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
      key: value
      # kubernetesRole: torque
      # kubernetesTokenPath: /var/run/secrets/kubernetes.io/serviceaccount/token
      # awsRole: torque
      # awsRegion: us-east-1
      # awsHeaderValue: vault.example.com
YAML

cat > values.dev.yaml <<'YAML'
db:
  password: secret://vault/app/db#password
YAML

torque apply plan --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider vault
torque apply --chart ./chart --release foo -n default -f values.dev.yaml --secret-provider vault
torque stack apply --config ./stacks/prod --secret-provider vault --yes
```

Inspect providers and references:
```bash
torque secrets test --secret-provider vault --ref secret://vault/app/db#password
torque secrets list --secret-provider vault --path app --format json
```

Minimal CLI workflow (sanity check before apply):
```bash
torque secrets test --secret-provider vault --ref secret://vault/app/db#password
torque secrets list --secret-provider vault --path app
torque secrets scan --scope repo --report secrets.json --mode block --flow-graph
torque secrets scan --scope render --manifest ./rendered.yaml --report render-secrets.json --mode block --flow-graph
torque security benchmark --corpus ./testdata/security --report benchmark.json
```

## Regression-proof plans

Do this:
```bash
torque apply plan --chart ./chart --release foo -n default --baseline ./plan.json
torque apply plan --chart ./chart --release foo -n default --compare-to ./plan.json
```

## Regression-proof verifier

Do this:
```bash
verifier verify.yaml --baseline ./baseline.json
verifier verify.yaml --compare-to ./baseline.json
```

## Share an `apply plan` visualization

```bash
torque apply plan --visualize --chart ./chart --release foo -n default
```

## Stack: minimal-flags workflow (plan → apply)

```bash
export TORQUE_STACK_ROOT=./stacks/prod

# Read-only plan (default `torque stack` behaves like `torque stack plan`)
torque stack

# Execute (DAG order)
torque stack apply --yes

# Capture the full stack run evidence bundle
torque stack apply --yes --capture ./stack.sqlite
```

## Stack: resume / rerun failed

```bash
export TORQUE_STACK_ROOT=./stacks/prod

# Resume the most recent run (frozen plan unless --replan is set)
torque stack apply --resume --yes

# Convenience: resume and schedule only failed nodes
torque stack rerun-failed --yes
```

## Stack: mixed action nodes

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: db-cutover
defaults:
  cluster:
    name: prod
releases:
  - name: api
    chart: ./charts/api

  - name: precheck
    kind: action.script
    needs: [api]
    action:
      idempotent: true
      apply:
        command: ["sh", "-c", "./scripts/precheck.sh"]

  - name: db-cutover
    kind: db.cutover
    needs: [precheck]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      prepareSQL: "SELECT 1"
      commitSQL: "UPDATE cutover_flags SET live = true WHERE name = 'api'"
      verifySQL: "SELECT live FROM cutover_flags WHERE name = 'api'"
      finalizeSQL: "UPDATE cutover_flags SET verified = true WHERE name = 'api'"
```

`db.cutover` supports `postgres`, `mysql`, `mariadb`, and `sqlite` drivers. Use
`dsnEnv` for live credentials so the stack plan can be committed without baking
secrets into `stack.yaml`.

```bash
TORQUE_DB_DSN='postgres://user:pass@127.0.0.1:5432/app?sslmode=disable' \
  torque stack plan --config ./stacks/db-cutover

TORQUE_DB_DSN='postgres://user:pass@127.0.0.1:5432/app?sslmode=disable' \
  torque stack apply --config ./stacks/db-cutover --yes

TORQUE_DB_DSN='root:pass@tcp(127.0.0.1:3306)/app' \
  torque stack apply --config ./stacks/db-cutover --yes
```

## Stack: evidence-backed action plugins

Use `action.plugin` when a step needs Ansible-style automation outside Helm but
still has to participate in Torque planning, evidence, retries, and export.
The plugin reads a JSON request on stdin and must print a JSON result.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: ops-program
defaults:
  cluster:
    name: prod
releases:
  - name: host-precheck
    kind: action.plugin
    action:
      idempotent: true
      plugin:
        command: ["./plugins/host-precheck"]
        phases: [observe, plan, apply, verify, export]
        timeout: 2m
        config:
          host: web-1
          package: openssl
```

Result contract:

```json
{"status":"planned","safeToRun":true,"risk":"low","message":"already current"}
```

`status=blocked` or `safeToRun=false` stops the node before mutation. Torque
records `plugin-<phase>.json`, `decision.json`, and any plugin-provided
artifacts in the normal stack audit/export bundle.

## Stack: render a host file

Use `host.file.render` for small host-side configuration files that need a
reviewable content digest, mode/owner intent, validation command, and no-op
repeat evidence.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-file-render
nodes:
  - name: render-nginx-snippet
    kind: host.file.render
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      path: /tmp/torque-nginx-snippet.conf
      mode: "0644"
      owner: root
      group: root
      template: |
        server_name {{ .ServerName }};
        proxy_read_timeout {{ .Timeout }};
      data:
        ServerName: app.example.com
        Timeout: 30s
      validate: 'test -s "$TORQUE_FILE_RENDER_TEMP_PATH"'
      removeOnDelete: true
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-file-render --yes

torque stack audit --config ./stacks/host-file-render \
  --output json --include-artifacts > host-file-render-audit.json

torque stack delete --config ./stacks/host-file-render --yes
```

## Stack: copy a host file

Use `host.file.copy` when the source already exists on the Torque controller
and the target host needs exact checksum evidence, backup/restore, permissions,
validation, and no-op repeat proof.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-file-copy
nodes:
  - name: copy-nginx-snippet
    kind: host.file.copy
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      sourcePath: files/nginx-snippet.conf
      path: /tmp/torque-nginx-snippet.conf
      mode: "0644"
      owner: root
      group: root
      backup: true
      restoreOnDelete: true
      validate: 'test -s "$TORQUE_FILE_COPY_TEMP_PATH"'
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-file-copy --yes

torque stack audit --config ./stacks/host-file-copy \
  --output json --include-artifacts > host-file-copy-audit.json

torque stack delete --config ./stacks/host-file-copy --yes
```

## Stack: install a host package

Use `host.package.install` for one-package lifecycle changes that need exact
before/after package evidence, package-manager command receipts, repeat no-op
proof, and cleanup through stack delete.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-package
nodes:
  - name: install-figlet
    kind: host.package.install
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      package: figlet
      packageManager: apt
      state: present
      removeOnDelete: true
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-package --yes

torque stack audit --config ./stacks/host-package \
  --output json --include-artifacts > host-package-audit.json

torque stack delete --config ./stacks/host-package --yes
```

## Stack: manage a host service

Use `host.service.manage` for one-service lifecycle changes that need exact
before/after service state, systemd command receipts, repeat no-op proof, and
cleanup through stack delete.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-service
nodes:
  - name: start-example
    kind: host.service.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      service: example.service
      serviceManager: systemd
      state: started
      enabled: true
      stopOnDelete: true
      disableOnDelete: true
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-service --yes

torque stack audit --config ./stacks/host-service \
  --output json --include-artifacts > host-service-audit.json

torque stack delete --config ./stacks/host-service --yes
```

## Stack: Kubernetes certificate lifecycle

Use `k8s.cluster.inspect`, `k8s.cert.inspect`, `k8s.cert.renew`, and
`k8s.cluster.verify` for cluster maintenance steps that need the same DAG
ordering, evidence, and idempotent rerun behavior as app deployments.
`k8s.cluster.inspect` discovers topology and provider hints from kubectl-visible
state. `provider: auto` detects kubeadm, k3s, or RKE2 on each maintenance
target; use `targetsFrom` to derive maintenance targets from inspect evidence,
or use `provider: custom` with explicit commands for other distributions.
`k8s.cert.renew` can enforce a lifecycle policy before mutation: rolling
`maxUnavailable`, fresh and healthy inspect evidence, supported provider hints,
maintenance windows, and pre-mutation app probes. The policy decision is written
as `k8s-lifecycle-policy-decision.json`. A blocked policy can only be bypassed
when the operator passes `--policy-override` and the stackfile contains scoped
approval evidence; the override decision is written as
`k8s-lifecycle-policy-override.json`.
`k8s.cluster.verify` also writes `k8s-lifecycle-summary.json`, a compact proof
chain that links source artifact digests for inspect evidence, policy decisions,
derived targets, certificate inspect/renew decisions, per-target checkpoints,
verify results, and app probes. If the stack defines both pre-mutation policy
app probes and post-maintenance verify app probes, the summary records an
`applicationGate` with before/after probe results and digest links to the source
artifacts.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: cluster-lifecycle
nodes:
  - name: cluster-inspect
    kind: k8s.cluster.inspect
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_SSH
        kubeconfig: /etc/rancher/k3s/k3s.yaml
        namespaces: [kube-system, app]

  - name: cert-inspect
    kind: k8s.cert.inspect
    needs: [cluster-inspect]
    kubernetes:
      provider: auto
      certificates:
        renewBefore: 720h
        order: control-plane-first
        batchSize: 1
        healthCheckCommand: "kubectl get nodes --no-headers | awk '$2==\"Ready\"{c++} END{print c+0}'"
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
        healthCheckCommand: "kubectl get nodes --no-headers | awk '$2==\"Ready\"{c++} END{print c+0}'"
        policy:
          maxUnavailable: 1
          requireFreshInspect: true
          maxInspectAge: 15m
          requireHealthyInspect: true
          requireSupportedProvider: true
          maintenanceWindow:
            start: "00:00"
            end: "04:00"
            timeZone: UTC
            days: [sunday]
          appProbes:
            - id: app-before-renew
              command: "curl -fsS http://127.0.0.1/healthz"
              expect: ok
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
        namespaces: [kube-system, app]
        stableIterations: 3
        stableInterval: 5s
        appProbes:
          - id: public-health
            command: "curl -fsS http://127.0.0.1/healthz"
            expect: ok
```

```bash
TORQUE_LAB_SSH='ssh://root@cluster-admin.example.com' \
  torque stack apply --config ./stacks/cluster-lifecycle --yes

# Use only when a policy block has approved, scoped override evidence.
TORQUE_LAB_SSH='ssh://root@cluster-admin.example.com' \
  torque stack apply --config ./stacks/cluster-lifecycle --yes --policy-override

torque stack audit --config ./stacks/cluster-lifecycle --include-artifacts \
  --output json > cluster-lifecycle-audit.json

torque stack export --config ./stacks/cluster-lifecycle \
  --out ./cluster-lifecycle-run.tgz
```

## Stack: full DB change program

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: db-program
defaults:
  cluster:
    name: prod
releases:
  - name: restore
    kind: db.restore-point
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      restorePointSQL: "SELECT 1"
      verifySQL: "SELECT TRUE, 'restore-point-created'"

  - name: expand
    kind: db.schema-expand
    needs: [restore]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      expandSQL: "ALTER TABLE users ADD COLUMN IF NOT EXISTS shadow_name TEXT"
      verifySQL: "SELECT TRUE, 'expanded'"

  - name: backfill
    kind: db.backfill
    needs: [expand]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      verifySQL: "SELECT (SELECT COUNT(*) FROM users_shadow) = (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM users_shadow)"
      backfill:
        checkpointTable: torque_backfill_state
        checkpointKey: users-shadow
        startSQL: "SELECT COALESCE(MIN(id), 1) - 1 FROM users"
        endSQL: "SELECT COALESCE(MAX(id), 0) FROM users"
        batchSQL: "INSERT INTO users_shadow(id, name) SELECT id, name FROM users WHERE id > {{.cursor_start}} AND id <= {{.cursor_end}} ON CONFLICT(id) DO NOTHING"
        batchSize: 500

  - name: verify
    kind: db.verify
    needs: [backfill]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      verifySQL: "SELECT (SELECT COUNT(*) FROM users_shadow) = (SELECT COUNT(*) FROM users), (SELECT COUNT(*) FROM users_shadow)"

  - name: cutover
    kind: db.cutover
    needs: [verify]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      metadataTable: torque_cutover_state
      prepareSQL: "UPDATE cutover_flags SET live = FALSE, verified = FALSE WHERE name = 'users'"
      commitSQL: "UPDATE cutover_flags SET live = TRUE WHERE name = 'users'"
      verifySQL: "SELECT live, verified FROM cutover_flags WHERE name = 'users'"
      finalizeSQL: "UPDATE cutover_flags SET verified = TRUE WHERE name = 'users'"

  - name: contract
    kind: db.schema-contract
    needs: [cutover]
    database:
      driver: postgres
      dsnEnv: TORQUE_DB_DSN
      contractSQL: "ALTER TABLE users DROP COLUMN IF EXISTS old_name"
      verifySQL: "SELECT TRUE, 'contract-complete'"
```

```bash
TORQUE_DB_DSN='postgres://user:pass@127.0.0.1:5432/app?sslmode=disable' \
  torque stack apply --config ./stacks/db-program --yes

torque stack audit --config ./stacks/db-program --output json > audit.json
torque stack export --config ./stacks/db-program --out ./db-program-export.tgz
```

## Stack: Oracle or APEX to PostgreSQL in Kubernetes

This showcase models a realistic migration shape:

- standalone Oracle or Oracle APEX as the source system;
- Kubernetes PostgreSQL stack readiness gates on the target side;
- a typed PostgreSQL restore/expand/backfill/verify/cutover/contract program;
- durable approval, export, and route-promotion receipts.

Use [`docs/showcase/oracle-postgres-k8s`](./showcase/oracle-postgres-k8s/README.md)
for the full product-spec walkthrough and runnable scripts.

```bash
# Local proof harness: same graph, SQLite target, no external database required.
TORQUE_ORACLE_PG_DSN="$PWD/docs/showcase/oracle-postgres-k8s/runtime/oracle-pg.sqlite" \
  torque stack apply --config ./docs/showcase/oracle-postgres-k8s/stack.sqlite.yaml --yes

# Real PostgreSQL target: same graph, PostgreSQL driver, same artifacts.
TORQUE_ORACLE_PG_DSN='postgres://postgres:postgres@127.0.0.1:5432/oracle_cutover?sslmode=disable' \
  torque stack apply --config ./docs/showcase/oracle-postgres-k8s/stack.postgres.yaml --yes

torque stack audit \
  --config ./docs/showcase/oracle-postgres-k8s/stack.sqlite.yaml \
  --output json \
  --include-artifacts > oracle-postgres-audit.json

torque stack export \
  --config ./docs/showcase/oracle-postgres-k8s/stack.sqlite.yaml \
  --out ./oracle-postgres-run.tgz

# The export is a redacted portable bundle with manifest hashes, so it can be
# audited without the original .torque state directory.
torque stack audit \
  --from-bundle ./oracle-postgres-run.tgz \
  --output json \
  --include-artifacts > oracle-postgres-bundle-audit.json
```

## Ops: adapter capability discovery

Use the capability catalog before wiring a stack node to an ops adapter. The
JSON contract records implemented versus planned adapters, supported phases,
evidence artifacts, privilege requirements, and probe results when a target is
provided.

```bash
torque ops adapter capabilities --format json > adapter-capabilities.json

torque ops adapter capabilities host.command.run \
  --target local://localhost \
  --format json > host-command-local-probe.json

torque ops adapter capabilities host.command.run \
  --target ssh://root@lab-host \
  --format json > host-command-ssh-probe.json
```

## Stack: inspect runs

```bash
export TORQUE_STACK_ROOT=./stacks/prod

torque stack runs --limit 50
torque stack status --follow
torque stack audit --output html > stack-audit.html
torque stack audit --from-bundle ./stack-run.tgz --output json --include-artifacts
```

## Build: share the build stream over WebSocket

```bash
torque build . --tag ghcr.io/acme/app:dev --ws-listen :9085
```

## Capture: record deploy/build/log evidence

```bash
# Record a deploy evidence file
torque apply --chart ./chart --release foo -n default --capture ./apply.sqlite --capture-tag change=CHG-1234

# Record a stack evidence file
torque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite

# Save it as a CI/review artifact
tar -czf torque-evidence.tgz ./apply.sqlite

# Explain a captured session locally or in CI logs
torque explain ./apply.sqlite
torque explain ./apply.sqlite --format markdown
```

## Verifier: validate a chart render in CI

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

verifier verify-chart-render.yaml

# Package a chart then verify the archive
torque-package ./chart --output dist/chart.sqlite
torque-package --verify dist/chart.sqlite
```
