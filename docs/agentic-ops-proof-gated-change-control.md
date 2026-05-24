# Proof-Gated Agentic Ops On A Real Workload

Argo CD and Crossplane were designed before agentic operations became the
center of gravity. They are strong control-plane tools: Argo CD continuously
compares Git state with cluster state, while Crossplane turns infrastructure
into Kubernetes APIs. But neither one starts from the question that matters when
an AI agent asks to mutate production:

> What proof makes this write safe enough to allow?

Torque's answer is a deliberately narrow operating loop:

1. The agent asks to mutate.
2. Torque checks the proof.
3. Torque checks the policy.
4. Torque records authorization.
5. The mutation is allowed only with a passing gate.

I tested that loop end to end on a disposable lab host on May 23, 2026. The
host was running a single-node k3s cluster. The workload was the
Oracle/APEX-to-PostgreSQL showcase: a stateful migration graph with change
approval, source read-only receipt, Oracle export receipt, PostgreSQL restore
point, schema expansion, backfill, verification, cutover, route promotion,
schema contract, post-cutover check, stack audit, and exported run evidence.

This was not a terminal-only demo. The run created a disposable Kubernetes
namespace, deployed PostgreSQL in k3s, port-forwarded it to the Torque process,
and ran the stack against that live database. The namespace was removed after
the evidence bundle was written. The durable artifacts remain on the remote
host under:

```text
/root/torque-agentic-ops-e2e-20260523-095146
```

## The Workload

The stack being protected was `oracle-postgres-k8s`. It models a practical
database modernization path:

- prove the Kubernetes target side is ready;
- record change-window approval;
- freeze the source Oracle/APEX system;
- export source data;
- create a PostgreSQL restore point;
- expand the target schema;
- backfill rows into shadow tables;
- verify row counts and route state;
- commit the cutover;
- promote the application route;
- contract the schema;
- run a post-cutover check;
- audit and export the run ledger.

The live target was PostgreSQL running inside k3s. After the authorized stack
apply completed, PostgreSQL reported:

```text
stage rows: 3
shadow rows: 3
route flags: true,true,true
migration audit phase: contracted
```

That is the important difference from a shallow "agent ran a command" demo. The
agent was asking to run a stateful production change program, not just patch a
Deployment.

## The Proof Gate

Before the mutation, Torque built a signed proof graph around the proposed
change. The graph linked the stack plan, verifier report, digest-pinned image
reference, BuildKit capture placeholder, SBOM, provenance, server dry-run
evidence, runtime drift proof, rollout event proof, logs capture, SLO outcome,
and repair channel.

The gate result:

```text
proof gate: passed
gate checks: 23
release score: 100
flight events: 20
proof graph artifacts: 20
checked files: 12
```

The graph was signed with an ed25519 key generated for the run, and
`torque proof verify --require-signature` verified the signature and file
hashes.

The useful product point is not the score by itself. The useful point is that
the score is attached to concrete evidence that can be rechecked later.

## The Agent Was Denied Twice

The first request looked like an agent asking to perform a mutating stack apply:

```json
{
  "actor": "codex-agent",
  "operation": "stack-apply",
  "command": ["torque", "stack", "apply", "--config", "./stack.yaml", "--yes"],
  "release": "oracle-postgres-k8s",
  "namespace": "data-platform",
  "reason": "migrate Oracle/APEX account data to the PostgreSQL target stack"
}
```

Torque denied it without an explicit allow-list entry:

```bash
torque agent policy check agent-request.json \
  --proof proof.graph.json \
  --require-gate \
  --out agent-policy-denied-no-allow.json
```

That failure matters. A passing proof graph is not enough by itself. The
requested operation still has to be explicitly allowed.

Then I tampered with the verifier evidence and tried again with the operation
allowed:

```bash
torque agent policy check agent-request.json \
  --proof proof.graph.json \
  --allow stack-apply \
  --require-gate \
  --out agent-policy-denied-tampered.json
```

Torque denied the request again because the proof gate no longer passed. The
agent had permission, but the evidence had been changed after the graph was
signed.

This is the operating model Torque should own: no permission-only writes, and no
proof-only writes. Mutating automation needs both.

## The Agent Was Authorized, But Did Not Execute

After restoring the evidence, the policy check passed:

```bash
torque agent policy check agent-request.json \
  --proof proof.graph.json \
  --allow stack-apply \
  --require-gate \
  --out agent-policy-allowed.json
```

Then Torque wrote the authorization record:

```bash
torque agent run agent-request.json \
  --proof proof.graph.json \
  --allow stack-apply \
  --require-gate \
  --out agent-run.json
```

The run record said `authorized: true` and `executed: false`. That is the
boundary. `agent run` does not mutate the cluster or the database. It records
that the caller is allowed to perform the write. The actual write still happens
as a separate, explicit operation:

```bash
torque stack apply \
  --config ./stack.yaml \
  --yes \
  --capture=./runtime/stack-apply.sqlite
```

That separation is important for agentic ops. The authorization artifact can be
attached to a change request, CI job, release record, or incident timeline. The
write itself remains auditable as a normal Torque stack run.

## The Mutation

Once proof and policy passed, Torque ran the stack apply against the live
PostgreSQL target in k3s. The run succeeded and produced:

```text
stack run status: succeeded
stack run id: 2026-05-23T10-02-37.032394298Z
stack audit artifacts: 30
stack audit events: 108
event-chain integrity: true
run-digest integrity: true
```

Torque then exported the stack run:

```text
oracle-postgres-run.tgz
sha256: 22399861e1073d24219512d5a323c23c0c92295ceea88d905a06495d69af72f9
```

The stack capture was also preserved:

```text
stack-apply.sqlite
sha256: 8893d62ed389ed3f8d23a19de9109a2fb1aa2bba5616cfac8bb0fc0ca250f0ba
```

So the final state is reviewable in three ways:

- the signed proof graph explains why the mutation was allowed;
- the agent policy and run records explain who was authorized and why;
- the stack audit/export artifacts explain what happened during execution.

## Why This Beats A Generic GitOps Story

Argo CD is excellent at keeping cluster state aligned with Git. Crossplane is
excellent at turning infrastructure into declarative APIs. For many teams, that
is enough.

But agentic production operations need a different primitive. They need a way
to say:

- this proposed write is tied to these exact files;
- those files still hash to the signed graph;
- the graph includes the required evidence;
- the release gate passed;
- the requested operation is explicitly allowed;
- the request matches the proof release and namespace;
- the agent authorization is recorded;
- the actual mutation produced a replayable run ledger.

That is not just sync. It is production change control.

The wedge is stateful work: database migration, backfill, cutover, contract,
cleanup, incident recurrence, and rollback evidence. These changes do not fit
cleanly into "desired state equals live state". They are programs with risk,
sequence, checkpoints, approval, and audit requirements.

Torque should become the system that governs those programs.

## The Product Shape

The tested loop is the product thesis in one sentence:

> Agents can ask to change production, but Torque only lets them proceed when
> proof, policy, and authorization all line up.

That gives Torque a sharper category than "another GitOps tool":

- proof-gated change control;
- replayable production evidence;
- agent authorization before mutation;
- stateful workload graphs;
- audit artifacts that survive the terminal session.

The next step is to make this flow first-class in `stack reconcile`: a resident
control loop that can observe desired state, explain drift, require proof for
mutation, record decisions, and replay history. Argo CD can still sync ordinary
apps. Crossplane can still provision infrastructure. Torque should own the
high-assurance path where an agent wants to mutate something risky and the
answer must be more precise than "it was allowed by RBAC."
