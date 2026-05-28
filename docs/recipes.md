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

## Agent NATS worker

Start a minimal outbound worker for stack nodes that use `transport: nats`.
The stack target is a NATS assignment subject; the worker executes
accepted `CommandAssignment` payloads through the local transport and replies
with the standard redacted `OperationResult`. Workers discover local
capabilities at startup and reject assignments whose `requiredCapability` is
not available, returning a `blocked` receipt without executing the command.
Receipts include the agent identity, local `workerId`, subject, capability
digest, and matching assignment `runId`/`nodeId` metadata. In fleet fan-out
mode workers normally subscribe to deterministic per-target subjects such as
`torque.assign.lab.host_mysql-01`; explicit stack targets can still use a
single custom subject. Start more than one worker with the same `--queue`,
`--subject`, `--target-id`, and ledger path for target-local HA; the queue is
the shared durable consumer in JetStream mode, not a broadcast primitive.

```bash
torque-agent nats worker \
  --nats-url nats://127.0.0.1:4222 \
  --subject torque.lab.assign.mysql \
  --agent-id agent-mysql-01 \
  --worker-id agent-mysql-01-a \
  --tenant lab \
  --target-id host/mysql-01 \
  --queue mysql-workers \
  --capability host.command.run
```

## NATS Firecracker Kafka and RabbitMQ labs

Run the real lab harness when you need a complete multi-node data-service
proof over NATS transport. The harness starts NATS and two
`torque-agent nats worker` processes on the lab host, then applies the Kafka
and RabbitMQ stackfiles through `transport: nats`. Each stack deploys a
traffic generator workload: Kafka continuously produces to and consumes from
`torque-traffic`, and RabbitMQ continuously publishes to and drains the
`torque_traffic` quorum queue. Use `--no-cleanup` to leave both clusters and
generators running side by side.

```bash
TORQUE_OPS_E2E_CONFIRM=1 \
TORQUE_LAB_SSH="ssh://root@141.105.65.227" \
scripts/e2e/ops/STACK-FC-KAFKA-RABBITMQ-001.sh \
  --destroy-existing \
  --evidence-root /tmp/torque-ops-e2e \
  --no-cleanup
```

Direct stack execution uses the same subjects and NATS URL:

```bash
TORQUE_NATS_URL=nats://127.0.0.1:4222 \
TORQUE_KAFKA_NATS_LAB_SUBJECT=torque.lab.kafka \
torque stack apply \
  --config ./testdata/stack/e2e/26-firecracker-kafka-nats-cluster \
  --yes

TORQUE_NATS_URL=nats://127.0.0.1:4222 \
TORQUE_RABBITMQ_NATS_LAB_SUBJECT=torque.lab.rabbitmq \
torque stack apply \
  --config ./testdata/stack/e2e/27-firecracker-rabbitmq-nats-cluster \
  --yes
```

## Agent capability report

`torque-agent capabilities report` observes the local host and emits the
adapter capabilities this agent can actually run, plus unavailable capabilities
with missing dependency reasons and a digest of the capability set.

```bash
torque-agent capabilities report --format json
```

## Agent NATS heartbeats

Run a minimal heartbeat publisher so operators can verify which agents are live
before a fleet run. Heartbeats discover local capabilities by default; use
`--discover-capabilities=false` only for manual or negative-test heartbeats.

```bash
torque-agent nats heartbeat \
  --nats-url nats://127.0.0.1:4222 \
  --tenant lab \
  --agent-id host-141 \
  --label role=mysql \
  --worker-slots 2

torque ops agent status \
  --nats-url nats://127.0.0.1:4222 \
  --tenant lab \
  --selector role=mysql \
  --format json
```

Durable mode uses JetStream for heartbeat events and a compact registry store
for status reads:

```bash
torque-agent nats heartbeat \
  --nats-url nats://127.0.0.1:4222 \
  --jetstream \
  --tenant lab \
  --agent-id host-141 \
  --label role=mysql \
  --worker-slots 2 \
  --worker-in-use 0

torque ops agent registry compact \
  --nats-url nats://127.0.0.1:4222 \
  --tenant lab \
  --store etcd \
  --etcd-endpoints http://127.0.0.1:2379

torque ops agent status \
  --source store \
  --store etcd \
  --etcd-endpoints http://127.0.0.1:2379 \
  --tenant lab
```

## Stack fleet readiness and capability gate

In local mode, stack nodes can still use direct SSH or NATS transports. Fleet
mode is the NATS-backed path: `torque stack apply` reads the compact agent
registry before hooks or node execution, writes `fleet-readiness.json` into the
stack state store, and blocks mutation when not enough matching agents are
ready. The same receipt also derives required capabilities from the stack node
kinds and requires every ready matching agent to advertise those capabilities.
NATS assignments carry the same required capability and node/run identifiers;
the worker enforces the contract again locally before command execution and
returns identity metadata in the execution receipt. When a fleet NATS node omits
`host.target`, Torque resolves the readiness selector into ready capable agents
and sends one assignment per target subject. Queue groups remain for HA workers
on the same target, not for broadcasting one message to many hosts. Set
`runner.fanout.delivery: jetstream` when assignments must survive a temporarily
offline worker; the worker consumes from `TORQUE_ASSIGNMENTS` and writes
receipts to `TORQUE_RECEIPTS` before ACKing the assignment. Durable workers also
keep a local SQLite assignment ledger, keyed by stable `assignmentId`, so a
redelivered assignment replays the stored receipt instead of running the command
again. With `--queue mysql-target-pool`, several local worker processes can
share one durable consumer and ledger for the same target; receipts identify
the exact `workerId` that executed or blocked the work. Add
`runner.fanout.targetConcurrency` to require advertised `workerSlots`, cap
per-target local concurrency, and reserve/release a durable slot lease for each
assignment and receipt. Use the SQLite `file` ledger for local/shared-disk
labs, or the `etcd` ledger in fleet control plane mode so concurrent
controllers cannot overbook one target. Set `ledger.renewInterval` so long
assignments keep their slot alive until release; the worker that mutates the
host verifies the lease grant digest, renews the slot while the command runs,
releases it after execution, rejects expired or wrong-target slot leases before
command execution, and records the lease decision plus worker-owned
renew/release metadata in receipts. If stack apply resumes after a controller
restart, Torque reloads
the private slot lease token escrow from `.torque/stack/state.sqlite`, renews
the held lease, and releases it after receipt collection without exposing the
raw token in audit artifacts. Add `runner.fanout.retry` to bound transient
failures and force dead-letter evidence when the retry budget is exhausted. Set
`TORQUE_NATS_ASSIGNMENT_SIGNING_KEY` for stack-side JetStream signing, then run
workers with `--verify-assignments --trusted-issuer-key` so agents reject
unsigned or mismatched broker messages before execution. Stack apply also
checkpoints each consumed JetStream receipt offset into
`.torque/stack/state.sqlite` before ACKing it; `torque stack apply --resume
--run-id <run-id>` can then hydrate completed target receipts from SQLite and
continue waiting only for missing targets.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: mysql-fleet
runner:
  mode: fleet
  readiness:
    source: store
    store: etcd
    etcdEndpoints:
      - http://127.0.0.1:2379
    tenant: lab
    selector:
      role: mysql
    requireAgents: true
    minReadyPercent: 95
    failureBudget: 5
    staleAfter: 45s
    onInsufficientReady: block
  fanout:
    delivery: jetstream
    maxParallel: 64
    maxFailed: 5
    minSucceededPercent: 95
    onPartialFailure: block
    targetConcurrency:
      enabled: true
      requireAvailable: true
      maxPerTarget: 2
      leaseTTL: 30s
      ledger:
        enabled: true
        store: etcd
        etcdEndpoints:
          - http://127.0.0.1:2379
        etcdPrefix: /torque
        renewInterval: 10s
    retry:
      maxDeliver: 3
      ackWait: 30s
      backoff:
        - 1s
        - 5s
        - 30s
      onExhausted: block
nodes:
  - name: mysql-check
    kind: host.command.run
    host:
      transport: nats
      command: mysqladmin ping
```

```bash
TORQUE_NATS_URL=nats://127.0.0.1:4222 \
TORQUE_NATS_ASSIGNMENT_STREAM=TORQUE_ASSIGNMENTS \
TORQUE_NATS_RECEIPT_STREAM=TORQUE_RECEIPTS \
TORQUE_NATS_ASSIGNMENT_SIGNING_KEY=./assignment-key.json \
TORQUE_NATS_ASSIGNMENT_ISSUER=torque-stack \
TORQUE_NATS_ASSIGNMENT_POLICY_DIGEST=sha256:approved-policy \
  torque stack apply --config ./stacks/mysql-fleet --yes
```

```bash
torque-agent nats worker \
  --nats-url nats://127.0.0.1:4222 \
  --delivery jetstream \
  --ledger-path ./.torque/agent/assignments.sqlite \
  --subject torque.assign.lab.host_mysql-01 \
  --agent-id agent-mysql-01 \
  --worker-id agent-mysql-01-a \
  --tenant lab \
  --target-id host/mysql-01 \
  --queue mysql-target-pool \
  --capability host.command.run \
  --verify-assignments \
  --trusted-issuer-key ./assignment-pub.json \
  --policy-digest sha256:approved-policy \
  --max-deliver 3 \
  --ack-wait 30s \
  --nak-delay 1s
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

## Stack: typed resource modules

Use module-backed typed resources when the integration should live outside the
Torque core repo. The stack keeps a domain kind such as
`demo.counter.ensure`, while `module.command` supplies the implementation.
Torque still controls the lifecycle and records module phase receipts.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: module-demo
nodes:
  - name: counter
    kind: demo.counter.ensure
    module:
      source: oci://ghcr.io/torque-modules/demo
      version: 0.1.0
      command: ["./modules/demo-counter"]
      input:
        path: /var/lib/demo/counter
        value: ready
```

Default lifecycle:

```text
observe -> diff -> plan -> apply -> verify
```

The module reads a JSON request on stdin and writes one JSON result on stdout.
Torque records `module-observe.json`, `module-diff.json`,
`module-plan.json`, `module-apply.json`, `module-verify.json`,
`module-resource.json`, `decision.json`, and module-provided artifacts in the
normal stack audit/export bundle.

Typed modules can use the same replaceable transports as core adapters. This
fixture keeps `host.file.ensure` outside the core repo while dispatching host
commands through SSH or a NATS assignment worker without changing the stack
node kind:

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-file-nats-module
nodes:
  - name: host-file
    kind: host.file.ensure
    module:
      source: oci://example.test/torque-modules/host
      version: 0.1.0
      command: ["python3", "../../../modules/torque.host/modules/file_ensure.py"]
      input:
        transport: nats
        target: torque.lab.assign.hostfile
        natsUrlEnv: TORQUE_NATS_URL
        path: /tmp/torque-host-file-nats-module.txt
        content: |
          torque host.file.ensure via nats
        mode: "0644"
```

The module payload uses POSIX shell on managed hosts, so targets do not need a
Python runtime. Use the Firecracker benchmark harness to compare the same
resource over SSH and NATS at fleet sizes:

```bash
TORQUE_OPS_E2E_CONFIRM=1 scripts/e2e/ops/OPS-TR-008.sh \
  --counts 1,10,100 \
  --vm-mem 192 \
  --destroy-existing-labs \
  --evidence-root /tmp/torque-ops-e2e
```

## Stack: Terraform providers as modules

Use `torque terraform-adapter` when a stack needs a resource from the
Terraform/OpenTofu provider ecosystem. Torque still owns the stack DAG,
receipts, audit, and replay; Terraform/OpenTofu owns provider installation,
planning, state, and resource CRUD. The adapter writes a saved plan during
`plan`, then `apply`/`delete` executes only that exact plan after checking the
node ID, intent digest, generated config digest, and plan digest.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: terraform-aws-s3
nodes:
  - name: logs-bucket
    kind: aws.s3.bucket.ensure
    module:
      source: oci://ghcr.io/torque-modules/terraform-aws
      version: 0.1.0
      command: ["torque", "terraform-adapter"]
      timeout: 20m
      input:
        provider:
          source: hashicorp/aws
          version: ">= 5.0"
          localName: aws
          config:
            region: us-east-1
        resource:
          type: aws_s3_bucket
          name: this
          values:
            bucket: my-company-prod-logs
            force_destroy: true
            tags:
              managed-by: torque
```

```bash
torque stack plan --config ./stacks/terraform-aws-s3 --output json
torque stack apply --config ./stacks/terraform-aws-s3 --yes
torque stack audit --config ./stacks/terraform-aws-s3 --output json --include-artifacts
torque stack delete --config ./stacks/terraform-aws-s3 --yes
```

See [`docs/terraform-provider-adapter.md`](terraform-provider-adapter.md) for
the full input contract and the opt-in AWS S3 smoke harness.

Run the deterministic 100-node provider harness before certifying generator or
adapter changes:

```bash
scripts/e2e/terraform-provider-100.sh --count 100 --concurrency 20
```

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

## Stack: manage a host user and group

Use `host.user.manage` for one-user and optional group lifecycle changes that
need exact before/after UID/GID evidence, command receipts, repeat no-op proof,
and cleanup through stack delete.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-user
nodes:
  - name: create-worker-user
    kind: host.user.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      user: torque-worker
      groupName: torque-worker
      userGroup: torque-worker
      uid: 24010
      gid: 24010
      home: /var/lib/torque-worker
      shell: /usr/sbin/nologin
      createHome: true
      removeHome: true
      removeOnDelete: true
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-user --yes

torque stack audit --config ./stacks/host-user \
  --output json --include-artifacts > host-user-audit.json

torque stack delete --config ./stacks/host-user --yes
```

## Stack: manage a host cron entry

Use `host.cron.manage` for one cron.d entry that needs exact before/after
content evidence, digest diff proof, repeat no-op proof, and cleanup through
stack delete.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-cron
nodes:
  - name: write-heartbeat-cron
    kind: host.cron.manage
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      path: /etc/cron.d/torque-heartbeat
      cronName: torque-heartbeat
      schedule: '*/5 * * * *'
      cronUser: root
      cronCommand: /usr/bin/touch /var/lib/torque-heartbeat
      mode: '0644'
      removeOnDelete: true
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-cron --yes

torque stack audit --config ./stacks/host-cron \
  --output json --include-artifacts > host-cron-audit.json

torque stack delete --config ./stacks/host-cron --yes
```

## Stack: manage a systemd unit

Use `host.systemd.unit` for one systemd unit file that needs exact content
evidence, daemon-reload proof, runtime verification, journal evidence, repeat
no-op proof, and cleanup through stack delete.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: host-systemd
nodes:
  - name: write-heartbeat-unit
    kind: host.systemd.unit
    host:
      transport: ssh
      targetEnv: TORQUE_LAB_SSH
      unit: torque-heartbeat.service
      path: /etc/systemd/system/torque-heartbeat.service
      state: started
      enabled: true
      content: |
        [Unit]
        Description=Torque heartbeat proof

        [Service]
        Type=oneshot
        RemainAfterExit=yes
        ExecStart=/bin/sh -c "echo torque-heartbeat"

        [Install]
        WantedBy=multi-user.target
      mode: '0644'
      stopOnDelete: true
      disableOnDelete: true
      removeOnDelete: true
```

```bash
TORQUE_LAB_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/host-systemd --yes

torque stack audit --config ./stacks/host-systemd \
  --output json --include-artifacts > host-systemd-audit.json

torque stack delete --config ./stacks/host-systemd --yes
```

## Stack: apply Kubernetes manifests

Use `k8s.manifest.apply` for namespace-scoped Kubernetes objects that need
server-side diff, field-manager ownership verification, repeat no-op proof, and
cleanup through stack delete. The adapter runs `kubectl` locally or over SSH and
records manifest, command output, and live object bodies as digests in evidence.
Create the target namespace before applying namespace-scoped objects.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: k8s-manifest
nodes:
  - name: apply-config
    kind: k8s.manifest.apply
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: k3s kubectl
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      manifest:
        namespace: torque-demo
        fieldManager: torque
        forceConflicts: true
        removeOnDelete: true
        content: |
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: torque-demo-config
            namespace: torque-demo
          data:
            marker: torque
```

```bash
TORQUE_LAB_K3S_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/k8s-manifest --yes

torque stack audit --config ./stacks/k8s-manifest \
  --output json --include-artifacts > k8s-manifest-audit.json

torque stack delete --config ./stacks/k8s-manifest --yes
```

## Stack: delete Kubernetes manifests

Use `k8s.manifest.delete` when deletion itself is the stack operation. The
adapter deletes only objects listed in the manifest, requires each existing
object to be owned by the configured field manager, and records the prune policy
as `listed-only` in evidence. Objects not listed in the manifest are left alone.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: k8s-manifest-delete
nodes:
  - name: delete-owned-config
    kind: k8s.manifest.delete
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: k3s kubectl
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      manifest:
        namespace: torque-demo
        fieldManager: torque
        prunePolicy: listed-only
        content: |
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: torque-demo-config
            namespace: torque-demo
```

```bash
TORQUE_LAB_K3S_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/k8s-manifest-delete --yes

torque stack audit --config ./stacks/k8s-manifest-delete \
  --output json --include-artifacts > k8s-manifest-delete-audit.json
```

## Stack: wait for Kubernetes resources

Use `k8s.resource.wait` when a stack must prove a Kubernetes object became
ready before later nodes run. The adapter records the initial object state, the
`kubectl wait` command receipt, the final readiness state, and related
Kubernetes events with event messages stored as digests only. A timeout is a
failed stack node, but the run still contains event evidence for debugging and
audit.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: k8s-resource-wait
nodes:
  - name: wait-api
    kind: k8s.resource.wait
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: k3s kubectl
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      resource:
        namespace: torque-demo
        kind: deployment
        name: torque-demo-api
        for: condition=Available
        timeout: 2m
        eventLimit: 25
```

```bash
TORQUE_LAB_K3S_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/k8s-resource-wait --yes

torque stack audit --config ./stacks/k8s-resource-wait \
  --output json --include-artifacts > k8s-resource-wait-audit.json
```

## Stack: capture Kubernetes logs

Use `k8s.logs.capture` when a stack needs bounded pod/container logs as
portable evidence. The adapter runs `kubectl logs` locally or over SSH, applies
line and byte limits, stores command output receipts as digests, and writes
redacted log lines plus a redaction proof into the run artifacts.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: k8s-logs-capture
nodes:
  - name: capture-api-logs
    kind: k8s.logs.capture
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: k3s kubectl
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      logs:
        namespace: torque-demo
        kind: deployment
        name: torque-demo-api
        container: app
        tailLines: 100
        limitBytes: 65536
        timestamps: true
```

```bash
TORQUE_LAB_K3S_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/k8s-logs-capture --yes

torque stack audit --config ./stacks/k8s-logs-capture \
  --output json --include-artifacts > k8s-logs-capture-audit.json
```

## Stack: capture Kubernetes events

Use `k8s.events.capture` when a stack needs namespace events as portable
evidence. The adapter records namespace observation, event filters, type/reason
counts, involved object metadata, and event message digests without storing raw
event messages in artifacts.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: k8s-events-capture
nodes:
  - name: capture-warning-events
    kind: k8s.events.capture
    kubernetes:
      cluster:
        transport: ssh
        targetEnv: TORQUE_LAB_K3S_SSH
        kubectlCommand: k3s kubectl
        kubeconfig: /etc/rancher/k3s/k3s.yaml
      events:
        namespace: torque-demo
        types: [Warning]
        reasons: [Failed, BackOff, Unhealthy]
        eventLimit: 100
```

```bash
TORQUE_LAB_K3S_SSH='ssh://root@lab-host' \
  torque stack apply --config ./stacks/k8s-events-capture --yes

torque stack audit --config ./stacks/k8s-events-capture \
  --output json --include-artifacts > k8s-events-capture-audit.json
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

## Stack: Durable PostgreSQL Backup to S3

`postgres.backup.run` can write a local dump, manifest, and catalog record, then
publish the backup artifacts to S3 with multipart upload session evidence. The
same stack shape works with `transport: ssh` for direct host execution or
`transport: nats` for fleet execution through a Postgres-capable agent.

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: postgres-backup-s3
nodes:
  - name: keycloak-backup
    kind: postgres.backup.run
    postgres:
      transport: nats
      database: keycloak
      backup:
        database: keycloak
        id: keycloak/nightly
        path: /var/backups/torque/postgres/keycloak
        file: /var/backups/torque/postgres/keycloak/keycloak.dump
        manifestPath: /var/backups/torque/postgres/keycloak/keycloak.manifest.json
        catalogPath: /var/backups/torque/postgres/keycloak/keycloak.catalog.json
        store:
          type: s3
          ref: s3://company-postgres-backups/prod/
          region: us-east-1
          partSizeBytes: 67108864
          sessionPath: /var/lib/torque/postgres/keycloak-upload-session.json

  - name: keycloak-backup-verify
    kind: postgres.backup.verify
    needs: [keycloak-backup]
    postgres:
      transport: nats
      database: keycloak
      backup:
        database: keycloak
        id: keycloak/nightly
        file: /var/backups/torque/postgres/keycloak/keycloak.dump
        manifestPath: /var/backups/torque/postgres/keycloak/keycloak.manifest.json
        catalogPath: /var/backups/torque/postgres/keycloak/keycloak.catalog.json
        store:
          type: s3
          ref: s3://company-postgres-backups/prod/
          region: us-east-1
```

```bash
AWS_REGION=us-east-1 \
  torque stack apply --config ./stacks/postgres-backup-s3 --yes

torque stack audit \
  --config ./stacks/postgres-backup-s3 \
  --output json \
  --include-artifacts > postgres-backup-s3-audit.json
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
