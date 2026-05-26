# Torque Fleet Control Plane Spec

Status: design draft; first local NATS heartbeat/status slice implemented.

This spec defines how Torque evolves from a CLI with local SQLite evidence into
an optional Kubernetes-installed fleet control plane that can operate 10,000
hosts through durable agents, NATS JetStream, and an etcd-backed inventory
registry.

The short version:

- Local Torque stays local: CLI, stack files, and SQLite evidence keep working
  with no server.
- Fleet Torque is an installed control plane: Kubernetes deployments plus
  dedicated etcd and NATS JetStream.
- etcd stores small, strongly-consistent metadata: inventory, enrollment,
  target bindings, run coordination, leases, policy digests, and compact status.
- NATS JetStream stores high-volume operational streams: heartbeats,
  assignments, receipts, retries, dead letters, and evidence offsets.
- SQLite remains the portable local cache/export format, not the shared source
  of truth for 10,000-host execution.

Implemented local slice:

- `torque-agent nats heartbeat` publishes typed `AgentHeartbeat` messages to
  `torque.v1.agent.heartbeat.<tenant>.<shard>.<agent-id>`.
- `torque ops agent status` subscribes to live heartbeat subjects, builds an
  `AgentStatusSnapshot`, supports label selectors, and outputs table or JSON.
- `scripts/e2e/ops/OPS-AGENT-004.sh` proves the local NATS loop end to end and
  exports a standard redacted ops evidence bundle.
- `torque-agent nats heartbeat --jetstream` publishes through JetStream and
  waits for server acknowledgment.
- `torque ops agent registry compact` consumes a durable JetStream pull
  consumer and writes compact latest-agent status into a registry store.
- `torque ops agent status --source store` reads compact registry status from a
  file or etcd backend.
- `scripts/e2e/ops/OPS-AGENT-005.sh` proves JetStream-to-etcd compaction end to
  end.

This is intentionally not the full fleet registry yet. It proves the
cross-process contract that the Kubernetes controller, etcd compactor, and
JetStream durability layers will harden.

## Engineering Posture

The control plane should be built like trading infrastructure, not like a pile
of remote shell scripts:

- small number of explicit state machines;
- typed envelopes for every cross-process message;
- deterministic replay from logs and checkpoints;
- bounded queues with visible backpressure;
- idempotency keys on all mutation;
- leases instead of implicit ownership;
- boring storage rules that operators can explain during an incident;
- no hidden mutable singleton in the CLI;
- no unbounded fan-out without a failure budget;
- every surprising branch writes evidence.

## Design Goals

1. Preserve local mode.

   `torque stack plan/apply/audit/export` must keep working from a laptop or CI
   runner with only local files and `.torque/stack/state.sqlite`.

2. Add fleet mode without changing stack semantics.

   A typed resource such as `host.file.ensure` or `mysql.replication.verify`
   must keep the same lifecycle:

   ```text
   observe -> diff -> plan -> apply/delete -> verify -> receipt
   ```

   The transport changes from SSH to NATS, but the stack node kind and receipt
   shape stay stable.

3. Separate state from events.

   etcd is for coordination and indexes. JetStream is for durable event flow.
   Do not turn etcd into a message bus. Do not turn JetStream into the
   authoritative inventory database.

4. Make agents durable.

   `torque-agent` must run as systemd, a container, or a Kubernetes DaemonSet.
   The CLI may bootstrap agents, but it must not be responsible for keeping
   them alive.

5. Make every mutation provable.

   Assignments, policy decisions, capabilities, observed state, applied change,
   verification, and final receipt offsets must be reconstructable from durable
   evidence.

6. Scale to 10,000 hosts.

   The design must avoid one controller goroutine, one etcd watcher, or one
   hot key per host. It must use sharded controllers, coalesced status writes,
   bounded streams, and explicit backpressure.

## Non-goals

- Replace Kubernetes, Helm, etcd, or NATS.
- Build a web UI as the primary automation surface.
- Require a central control plane for local or small-team use.
- Store all module integrations in the core repo.
- Store raw command output or secrets in etcd.
- Use Kubernetes' own internal etcd directly. Fleet mode uses a dedicated etcd
  cluster or a carefully-scoped storage adapter; the Kubernetes API server's
  backing etcd is not a public application database.

## Modes

### Local Mode

Local mode is the default.

Components:

- `torque` CLI
- stack files
- local module collections
- local `.torque/stack/state.sqlite`
- optional direct SSH
- optional local NATS worker for development

Local mode is good for:

- authoring
- CI
- labs
- single-host or small-fleet execution
- portable evidence bundles

Local mode does not promise:

- agent liveness across a large fleet
- durable assignment retry after the CLI exits
- shared inventory across operators
- HA control-plane semantics

### Fleet Mode

Fleet mode is installed into Kubernetes and behaves more like Argo CD in
deployment shape, but not in product semantics. Argo CD reconciles Kubernetes
objects from Git. Torque Fleet coordinates typed operational resources across
hosts, Kubernetes, databases, and services with proof-backed receipts.

Components:

- `torque-control-api`
- `torque-run-coordinator`
- `torque-agent-registry`
- `torque-assignment-controller`
- `torque-receipt-ingestor`
- `torque-inventory-indexer`
- dedicated etcd StatefulSet
- NATS JetStream cluster
- optional object storage for large evidence artifacts
- `torque-agent` on target hosts
- `torque` CLI as a client

Fleet mode is good for:

- 100 to 10,000 hosts
- outbound-only target connectivity
- progressive rollout
- agent readiness gates
- durable retries
- evidence replay
- shared operator state

## Kubernetes Install Profile

The install command should be:

```bash
torque control-plane install \
  --namespace torque-system \
  --storage dedicated-etcd \
  --nats jetstream \
  --replicas 3
```

The Helm chart should install:

- namespace and service accounts;
- RBAC for Torque controllers;
- etcd StatefulSet, Service, PDB, backup CronJob, and TLS secret references;
- NATS JetStream StatefulSet, Service, PDB, account/operator config, and TLS;
- API deployment;
- controller deployments;
- metrics ServiceMonitors when enabled;
- NetworkPolicies for API, etcd, NATS, and controller traffic;
- optional ingress for the API only.

The chart must support using external etcd and external NATS:

```yaml
storage:
  mode: external-etcd
  endpoints:
    - https://etcd-0.example:2379

nats:
  mode: external
  url: tls://nats.example:4222
```

The default production install should run:

- 3 etcd members;
- 3 NATS JetStream servers;
- 2 or more API replicas;
- controller replicas by shard ownership, not all-active processing of the same
  shard.

## Component Responsibilities

### `torque` CLI

The CLI remains the user entrypoint.

Local mode:

- reads stack files;
- plans and applies locally;
- writes SQLite evidence;
- exports bundles.

Fleet mode:

- submits stack runs to `torque-control-api`;
- streams progress from JetStream or the API;
- syncs receipts into local SQLite for offline audit/export;
- can run `--local-only` to bypass the control plane.

The CLI must never be required to stay alive for a fleet run to finish.

### `torque-control-api`

The API is a thin control boundary:

- authenticates operators and automation;
- validates stack submissions;
- stores run records in etcd;
- returns selectors, readiness summaries, and run status;
- exposes read-only audit/export endpoints;
- never performs high-volume per-host execution itself.

### `torque-run-coordinator`

The run coordinator owns run-level state:

- resolves target selectors through the inventory index;
- checks readiness gates;
- creates immutable run plan records;
- shards the target set;
- writes assignment batches to JetStream;
- tracks run completion from receipt offsets;
- applies failure budgets and rollout windows.

It stores only compact run state in etcd.

### `torque-agent-registry`

The registry controller owns agent presence:

- consumes heartbeat streams;
- validates enrollment and target bindings;
- maintains in-memory liveness indexes;
- writes coalesced compact status to etcd;
- marks agents stale, draining, quarantined, disabled, or revoked.

It must not write every heartbeat to etcd.

### `torque-assignment-controller`

The assignment controller owns durable work delivery policy:

- creates JetStream streams and consumers;
- enforces retry, max-deliver, ack wait, and dead-letter policy;
- tracks assignment leases;
- handles cancellation and drain;
- prevents duplicate mutation acceptance when a run is already terminal.

### `torque-receipt-ingestor`

The receipt ingestor owns proof assembly:

- consumes receipt streams;
- validates signed receipt envelopes;
- stores compact receipt indexes in etcd;
- stores large evidence bodies in object storage or keeps them in JetStream by
  digest reference;
- exposes run evidence for CLI sync and export.

### `torque-inventory-indexer`

The inventory indexer owns target lookup:

- imports static TargetGraph files;
- ingests dynamic inventory adapters;
- binds agents to target IDs;
- maintains label, role, site, capability, and health indexes;
- computes selector results without scanning all agents on every run.

## Storage Model

### etcd

etcd is the strongly-consistent metadata store.

Use it for:

- tenants and projects;
- target inventory;
- agent enrollment;
- agent to target binding;
- compact latest agent status;
- capability digest;
- policy digest;
- run metadata;
- immutable plan digest;
- shard ownership leases;
- assignment index by run and shard;
- receipt offset checkpoints;
- quarantine and drain flags;
- credential generation metadata.

Do not use etcd for:

- every heartbeat event;
- command stdout/stderr bodies;
- module artifact bodies;
- large run logs;
- high-volume per-phase event streams;
- pub/sub.

Suggested key layout:

```text
/torque/v1/tenants/<tenant>
/torque/v1/targets/<tenant>/<target-id>
/torque/v1/target-index/label/<tenant>/<key>/<value>/<target-id>
/torque/v1/agents/<tenant>/<agent-id>/enrollment
/torque/v1/agents/<tenant>/<agent-id>/binding
/torque/v1/agents/<tenant>/<agent-id>/status
/torque/v1/agents/<tenant>/<agent-id>/capabilities
/torque/v1/runs/<tenant>/<run-id>/meta
/torque/v1/runs/<tenant>/<run-id>/plan
/torque/v1/runs/<tenant>/<run-id>/shards/<shard-id>
/torque/v1/runs/<tenant>/<run-id>/receipt-checkpoints/<shard-id>
/torque/v1/leases/controllers/<component>/<shard-id>
```

All etcd values must be small JSON or protobuf records. Large artifacts are
stored by digest elsewhere.

### NATS JetStream

JetStream is the durable event fabric.

Use it for:

- agent heartbeat events;
- assignment envelopes;
- receipt envelopes;
- stream offsets;
- retries and redelivery;
- dead letters;
- advisory capture;
- replay after controller restart.

Suggested streams:

```text
TORQUE_AGENT_EVENTS
  subjects:
    torque.v1.agent.heartbeat.<tenant>.<shard>.<agent-id>
    torque.v1.agent.lifecycle.<tenant>.<shard>.<agent-id>

TORQUE_ASSIGNMENTS
  subjects:
    torque.v1.assign.<tenant>.<shard>.<agent-id>
    torque.v1.assign.<tenant>.<shard>.capability.<capability>

TORQUE_RECEIPTS
  subjects:
    torque.v1.receipt.<tenant>.<run-id>.<shard>.<agent-id>

TORQUE_DEADLETTER
  subjects:
    torque.v1.deadletter.<tenant>.<run-id>.<shard>

TORQUE_AUDIT
  subjects:
    torque.v1.audit.<tenant>.<actor>.<event-type>
```

Stream retention:

- heartbeats: short retention, compacted into registry status;
- assignments: retained until run retention expires;
- receipts: retained until evidence retention expires;
- dead letters: retained longer than assignments;
- audit: retained according to compliance policy.

### SQLite

SQLite remains the portable evidence cache.

Use it for:

- local mode source of truth;
- CLI-side run cache in fleet mode;
- exported evidence bundles;
- offline audit;
- support bundles.

Fleet mode must be able to reconstruct a SQLite export from control-plane run
metadata plus JetStream/object-storage evidence.

## Agent Model

### Agent Identity

Every agent has:

- stable `agentId`;
- tenant/project;
- target binding;
- public key or NKey identity;
- enrollment state;
- labels;
- capabilities;
- version;
- config digest;
- policy digest.

Agent IDs must be stable across restarts and credential rotations. Hostnames are
metadata, not identity.

### Enrollment

Bootstrap over SSH:

```bash
torque ops agent bootstrap \
  --target ssh://root@host-141 \
  --nats-url tls://nats.torque-system.svc:4222 \
  --tenant prod \
  --labels site=lab,role=mysql
```

Bootstrap installs:

- `/usr/local/bin/torque-agent`;
- `/etc/torque/agent.yaml`;
- credentials or NKey seed;
- systemd unit;
- watchdog/restart policy;
- local spool directory;
- initial labels and capabilities.

Enrollment flow:

1. Bootstrap writes a pending enrollment token.
2. Agent connects outbound to NATS.
3. Agent publishes an enrollment request.
4. Control plane records pending enrollment in etcd.
5. Operator or policy approves binding to a target.
6. Agent receives signed enrollment approval.
7. Agent starts accepting assignments only after approval.

Unapproved agents may publish enrollment and heartbeat only. They cannot receive
mutating assignments.

### Heartbeat

Heartbeat subject:

```text
torque.v1.agent.heartbeat.<tenant>.<shard>.<agent-id>
```

Payload:

```yaml
apiVersion: torque.dev/agent-heartbeat/v1
kind: AgentHeartbeat
agentId: agent-01HF...
tenant: prod
targetId: host/141
version: v0.8.0
labels:
  site: lab
  role: mysql
capabilitiesDigest: sha256:...
policyDigest: sha256:...
configDigest: sha256:...
slots:
  total: 8
  used: 2
offsets:
  assignmentConsumer: 184455
  lastReceipt: 184440
resources:
  load1: 0.7
  memAvailableBytes: 8123129856
  diskAvailableBytes: 120034123776
state: ready
observedAt: "2026-05-26T19:00:00Z"
```

Default cadence:

- ready agents: 15 seconds plus jitter;
- busy agents: 5 seconds plus jitter;
- draining or unhealthy agents: 5 seconds plus jitter;
- disconnected detection: stale after 45 seconds by default.

Registry controllers coalesce etcd status writes:

- write immediately on state change;
- write at most once per 60 seconds for unchanged ready state;
- write final stale/quarantine/drain transitions.

For 10,000 agents at 15 second cadence, NATS receives about 667 heartbeat
messages per second. etcd should not receive 667 status writes per second when
nothing changed.

### Local Spool

Agents maintain a local spool:

```text
/var/lib/torque/agent/spool/
  assignments/
  receipts/
  artifacts/
  checkpoints/
```

The spool handles:

- NATS disconnect;
- process restart;
- receipt publish retry;
- idempotency key replay;
- evidence upload retry.

Agents must write receipt intent locally before acknowledging a mutating
assignment.

## Assignment Model

### Envelope

Every assignment is signed.

```yaml
apiVersion: torque.dev/assignment/v1
kind: AssignmentEnvelope
assignmentId: asg_01HF...
runId: run_01HF...
tenant: prod
targetId: host/141
agentId: agent_01HF...
resource:
  nodeId: host.file.ensure/file-001
  kind: host.file.ensure
  lifecyclePhase: apply
operation: run
idempotencyKey: run_01HF:file-001:apply:1
policy:
  decisionDigest: sha256:...
  approvedBy: user/alice
  mode: guarded
desiredDigest: sha256:...
planDigest: sha256:...
deadline: "2026-05-26T19:10:00Z"
attempt: 1
replyTo: torque.v1.receipt.prod.run_01HF.042.agent_01HF
signature:
  keyId: torque-control/prod
  alg: ed25519
  value: ...
```

Agents verify:

- signature;
- tenant;
- target binding;
- policy digest;
- capability allowance;
- deadline;
- idempotency key;
- quarantine/drain state.

A NATS connection alone grants no authority.

### Lifecycle

1. Run coordinator resolves selector to target IDs.
2. Readiness gate verifies enough agents are ready.
3. Coordinator writes immutable run plan to etcd.
4. Assignment controller publishes signed assignments to JetStream.
5. Agent receives assignment.
6. Agent verifies envelope.
7. Agent writes local assignment checkpoint.
8. Agent executes local module/transport contract.
9. Agent writes local receipt checkpoint.
10. Agent publishes signed receipt.
11. Agent ACKs assignment only after receipt publish succeeds or is safely
    spooled.
12. Receipt ingestor validates and indexes receipt.
13. Run coordinator advances run state.

### Idempotency

The idempotency key is mandatory for every mutating phase.

Agents keep a bounded idempotency cache:

```text
<tenant>/<run-id>/<node-id>/<phase>/<attempt>
```

If the same assignment is redelivered after a crash, the agent returns the
previous receipt when the local checkpoint proves it already executed.

This is how Torque makes at-least-once JetStream delivery safe enough for
operations.

## Readiness Gates

Stack runner options:

```yaml
runner:
  mode: fleet
  requireAgents: true
  minReadyPercent: 95
  failureBudget: 5
  maxUnavailable: 50
  maxInFlight: 500
  staleAfter: 45s
  fallback:
    ssh: false
```

Before mutation:

1. Resolve desired targets.
2. Check enrolled agent binding for each target.
3. Check heartbeat freshness.
4. Check capability digest.
5. Check policy digest.
6. Check quarantine/drain flags.
7. Check current slot availability.
8. Compute allowed target set.

Outcomes:

- `blocked`: readiness below policy.
- `partial`: allowed only when the stack explicitly permits partial execution.
- `fallback`: use SSH only when explicitly allowed.
- `ready`: create assignment stream records.

Readiness is evidence. The gate writes:

- selected target count;
- ready target count;
- stale target IDs or digests;
- blocked reasons;
- selector digest;
- inventory revision;
- heartbeat watermark.

## Inventory Model

Torque inventory is not only an Ansible-style host list. It is a TargetGraph
with bindings.

Target record:

```yaml
apiVersion: torque.dev/target/v1
kind: Target
id: host/141
tenant: prod
labels:
  site: lab
  role: mysql
  env: dev
addresses:
  ssh: ssh://root@141.105.65.227
agent:
  expected: true
  binding: agent_01HF...
capabilities:
  - host.file
  - host.systemd
  - mysql.replication
```

Agent binding record:

```yaml
apiVersion: torque.dev/agent-binding/v1
kind: AgentBinding
agentId: agent_01HF...
targetId: host/141
approved: true
approvedBy: user/alice
approvedAt: "2026-05-26T19:00:00Z"
bindingDigest: sha256:...
```

Selectors operate over target inventory, then readiness gates join that set
with live agent state.

## 10,000 Host Scale Design

### Sharding

Use deterministic shard assignment:

```text
shard = fnv1a(agentId) % 256
```

Shard count defaults:

- 64 shards for <= 2,000 agents;
- 256 shards for <= 10,000 agents;
- 1024 shards for larger installs.

Controllers claim shard leases in etcd. Each shard has one active owner per
component.

### Heartbeat Budget

At 10,000 agents:

- 15 second heartbeat: about 667 msg/s;
- 5 second busy heartbeat during rollout: up to 2,000 msg/s;
- status writes to etcd: coalesced, typically below 200 writes/s steady state;
- reconnect storm: jittered reconnect and server-side rate limits required.

### Assignment Budget

For a 10,000 target run with 7 lifecycle operations per target:

- 70,000 operation receipts;
- 70,000 assignment attempts before retries;
- receipt bodies are small;
- large artifacts are digest-referenced.

The run coordinator must batch by shard:

- create one run record;
- create shard records for target ranges;
- publish assignments in bounded windows;
- avoid writing one etcd key per phase event.

### Fan-out

Default fan-out policy:

```yaml
maxInFlight: 500
perShardMaxInFlight: 32
perAgentSlots: 1
ackWait: 2m
maxDeliver: 5
deadLetterAfter: 5
```

The control plane should prefer progressive fan-out:

1. canary: 1 or 5 targets;
2. first batch: 1 percent;
3. normal batches: bounded by `maxInFlight`;
4. pause on failure-budget breach;
5. resume from assignment and receipt offsets.

### Indexing

Selector lookup must not scan 10,000 JSON documents for common paths.

Maintain indexes:

```text
label role=mysql -> target IDs
label site=lab -> target IDs
capability host.file -> target IDs
agent-state ready -> agent IDs
target->agent binding -> agent ID
```

The in-memory index is rebuilt from etcd on controller start. etcd remains the
source of truth for inventory; memory is the fast path.

### Reconnect Storm Handling

When NATS restarts or a site link returns:

- agents reconnect with jitter;
- heartbeat publish is jittered;
- registry writes are coalesced;
- assignments are not immediately redelivered to every agent;
- run coordinator resumes by shard and offset;
- stale-to-ready transitions are rate-limited in etcd.

## Security Model

### Authentication

Agents authenticate to NATS with NKey or mTLS.

Operators authenticate to the API with OIDC, mTLS, or service account tokens.

Controllers authenticate to etcd and NATS with separate identities.

### Authorization

Subject permissions are scoped:

```text
agent can publish:
  torque.v1.agent.heartbeat.<tenant>.<shard>.<agent-id>
  torque.v1.receipt.<tenant>.*.<shard>.<agent-id>

agent can subscribe:
  torque.v1.assign.<tenant>.<shard>.<agent-id>
```

Agents cannot subscribe to other agents' assignments. Agents cannot publish
assignments.

### Signed Assignments

NATS auth is necessary but insufficient. Agents must verify signed assignment
envelopes before execution.

### Quarantine

Quarantine is a hard deny for mutation:

- stored in etcd;
- included in readiness gate;
- optionally pushed as a signed policy update;
- observe-only diagnostics can be allowed by policy.

### Secrets

Secrets are never stored raw in etcd, JetStream, SQLite, or logs.

Receipts store:

- redacted stdout/stderr;
- digests;
- secret reference IDs;
- redaction proof.

## Failure Semantics

### Agent Dies Before Execution

Assignment remains unacked. JetStream redelivers after `ackWait`.

### Agent Dies After Execution Before Receipt Publish

Agent local spool must contain execution checkpoint. On restart, it publishes
the receipt and then ACKs or handles duplicate redelivery by idempotency key.

### Agent Publishes Receipt But ACK Fails

Assignment may redeliver. Agent returns the existing receipt for the same
idempotency key.

### Controller Dies

Shard lease expires. Another controller claims the shard and resumes from etcd
run state plus JetStream offsets.

### NATS Unavailable

Agents keep local state and reconnect with jitter. New fleet runs are blocked or
queued according to policy. Local mode remains available.

### etcd Unavailable

No new run coordination or inventory mutation. Existing assignments may finish
and receipts may continue to JetStream. Ingestor catches up when etcd returns.

### Network Partition

Site-local agents can keep heartbeating to a site NATS leaf node if configured.
Global readiness marks the site degraded. New global mutations pause unless the
run policy permits isolated-site execution.

## API And CLI Surface

Install:

```bash
torque control-plane install --namespace torque-system
torque control-plane status
torque control-plane doctor
```

Agent lifecycle:

```bash
torque ops agent bootstrap --target ssh://root@host-141 --nats-url tls://nats:4222
torque ops agent enroll approve agent/host-141 --target host/141
torque ops agent list --selector role=mysql
torque ops agent status agent/host-141 --output json
torque ops agent drain agent/host-141 --reason "maintenance"
torque ops agent quarantine agent/host-141 --reason "bad capability digest"
torque ops agent rotate-credentials --selector site=lab --canary 10 --yes
torque ops agent upgrade --selector role=mysql --to v0.9.0 --canary 1 --yes
```

Fleet readiness:

```bash
torque fleet status --selector role=mysql
torque fleet wait --selector role=mysql --min-ready 95% --timeout 5m
```

Fleet stack run:

```bash
torque stack apply --config ./mysql.yaml --fleet --yes
torque stack status --run-id run_01HF --watch
torque stack audit --run-id run_01HF --output html
torque stack export --run-id run_01HF --out mysql-run.tgz
```

Stack shape:

```yaml
apiVersion: torque.dev/v1
kind: Stack
name: mysql-day2
runner:
  mode: fleet
  requireAgents: true
  minReadyPercent: 95
  maxInFlight: 500
nodes:
  - name: mysql-replication
    kind: mysql.replication.verify
    module:
      source: oci://ghcr.io/torque-modules/mysql
      version: 0.1.0
      input:
        transport: nats
        selector:
          role: mysql
          cluster: lab
```

## Resource: `torque.agent.ensure`

`torque.agent.ensure` manages the agent itself.

```yaml
kind: torque.agent.ensure
input:
  transport: ssh
  target: ssh://root@141.105.65.227
  natsUrl: tls://nats.torque-system.svc:4222
  service: systemd
  tenant: lab
  labels:
    site: lab
    role: mysql
```

Lifecycle:

- observe: check installed binary, service, config, credentials, current
  enrollment;
- diff: compare desired agent version, labels, NATS URL, service policy;
- plan: produce install/upgrade/rotate plan with risk;
- apply: install or update systemd service and credentials;
- verify: prove service is active and heartbeat observed;
- receipt: include service status, version, config digest, heartbeat offset.

This resource is the bridge from SSH bootstrap to NATS operations.

## Evidence Model

Every fleet run produces:

- run metadata;
- inventory snapshot digest;
- readiness gate receipt;
- immutable plan digest;
- assignment stream offsets;
- per-target phase receipts;
- dead-letter report if any;
- retry report;
- final run summary;
- redaction report;
- export manifest.

The CLI export should reconstruct the same shape as local SQLite stack export.

## Operating SLOs

Initial targets for 10,000 hosts:

- register 10,000 connected agents;
- process steady heartbeats at 15 second cadence;
- detect stale agents within 45 seconds plus one registry reconciliation tick;
- resolve common label selectors over 10,000 targets in under 500 ms from warm
  index;
- start a 10,000 target no-op run without writing more than O(shards) etcd run
  records;
- keep assignment and receipt streams bounded by retention policy;
- survive one controller restart without losing run state;
- survive one agent crash during mutation without duplicate side effects;
- export a completed run without requiring live agents.

These are product SLOs, not just benchmarks. Each must have an E2E or scale-sim
proof before the feature is called production-ready.

## Implementation Slices

### Slice 1: Agent Bootstrap Resource

- `torque.agent.ensure` module/resource.
- SSH install to systemd.
- Agent config file and service verification.
- Heartbeat publish to NATS.
- One real host proof.

### Slice 2: Heartbeat Registry

- NATS heartbeat schema. Implemented locally as
  `torque.dev/agent-heartbeat/v1`.
- Registry controller. Implemented locally as `torque ops agent registry
  compact`, a one-shot command ready to become a looped controller.
- etcd compact status schema. Implemented by the `AgentCompactStatus` record
  under `/torque/agent-registry/v1/tenants/<tenant>/agents/<agent-key>`.
- `torque fleet status`. Local precursor implemented as
  `torque ops agent status`.
- stale/drain/quarantine states.

### Slice 3: Fleet Readiness Gate

- stack `runner.mode: fleet`;
- selector resolution from inventory;
- readiness receipt;
- block/partial/fallback policy.

### Slice 4: Durable Assignments

- JetStream assignment stream;
- signed assignment envelope;
- per-agent subject auth;
- ack/redelivery/dead-letter;
- local agent idempotency cache.

### Slice 5: Receipt Ingestion And Export

- receipt stream;
- receipt signature verification;
- object/digest artifact references;
- SQLite export reconstruction.

### Slice 6: Kubernetes Install

- Helm chart for API, controllers, etcd, NATS;
- external etcd/NATS mode;
- backup/restore and NetworkPolicy;
- `torque control-plane install/status/doctor`.

### Slice 7: 10,000 Agent Scale Proof

- deterministic synthetic agents using the same NATS protocol;
- reconnect storm;
- controller restart;
- stale heartbeat injection;
- assignment retry/dead-letter;
- evidence-offset resume;
- bounded etcd write rate.

### Slice 8: Real Fleet Proof

- 100 real Firecracker VMs;
- 1,000 synthetic plus 100 real mixed run;
- 10,000 synthetic run;
- production readiness report.

## Design Invariants

- Stack semantics do not depend on transport.
- Agents execute only signed assignments.
- etcd stores metadata, not logs.
- JetStream stores events, not canonical inventory.
- SQLite exports must remain portable.
- Every mutating assignment has an idempotency key.
- Every run can be explained after the fact without live targets.
- Local mode must not regress while fleet mode grows.
