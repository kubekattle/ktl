# torque

<p align="center">
  <a href="https://ingresslabs.github.io/torque/"><strong>Docs and live demos</strong></a> |
  <a href="https://github.com/ingresslabs/torque/actions/workflows/ci.yml">CI</a> |
  <a href="https://github.com/ingresslabs/torque/releases">Releases</a> |
  <a href="./LICENSE">License</a>
</p>

> **Start here:** https://ingresslabs.github.io/torque/
>
> `torque` is an agent-first Kubernetes delivery CLI: ask an agent to build,
> verify, plan, apply, capture evidence, and inspect what happened.

`torque` is built for the release loop where humans, CI, and agents share the
same delivery surface. It keeps Kubernetes delivery file-first: Docker builds,
Helm plans, verifier reports, rollout captures, logs, and stack plans become
portable artifacts instead of hidden service state.

The central idea is simple: an agent can do Docker and Kubernetes work, but the
output must be reviewable. `torque` turns delivery steps into explicit files,
including SQLite captures that can be attached to CI runs, PR reviews, release
bundles, or later debugging sessions.

## Install

```bash
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh
```

Install durable Linux services for a remote agent host:

```bash
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon
```

From source:

```bash
go install github.com/ingresslabs/torque/cmd/torque@latest
go install github.com/ingresslabs/torque/cmd/torque-agent@latest
go install github.com/ingresslabs/torque/cmd/torque-mcp@latest
go install github.com/ingresslabs/torque/cmd/verifier@latest
```

## Core Loop

```bash
torque build . --tag ghcr.io/acme/api:dev --capture ./build.sqlite
verifier --chart ./chart --release api -n prod \
  --security-profile enterprise --secrets-report secrets.json \
  --security-boundary-matrix \
  --security-evidence ./torque-security-evidence \
  --format json --report verify.json
torque apply plan --chart ./chart --release api -n prod \
  --verify-report verify.json --build-capture ./build.sqlite \
  --github-comment --output plan.md
torque apply simulate --chart ./chart --release api -n prod \
  --security-evidence ./torque-security-evidence \
  --out ./torque-sim-proof
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
torque apply --chart ./chart --release api -n prod \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes
torque proof graph ./apply-proof.json \
  --attach drift-proof.json --out proof.graph.json --html proof.html
torque proof verify proof.graph.json
torque proof gate proof.graph.json --out proof.gate.json
torque release score proof.graph.json --out release-score.json
torque release autopilot proof.graph.json \
  --key .torque/keys/proof-ed25519.json \
  --policy release-policy.yaml \
  --out-dir release-autopilot
torque release promote proof.graph.json \
  --strategy canary --steps 5,25,50,100 \
  --slo slo.yaml --out-dir release-promote-canary
torque release promote proof.graph.json \
  --strategy blue-green --preview --smoke smoke.json \
  --switch-traffic --out-dir release-promote-blue-green
torque flight record proof.graph.json --out release.flight.torque
torque agent policy check agent-request.json \
  --proof proof.graph.json --allow apply --require-gate
torque incident capture --release api -n prod --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof/
torque contract synthesize --from incident-replay-proof/ \
  --guardian drift-proof.json --out torque-contract.yaml
torque contract test --contract torque-contract.yaml \
  --from incident-replay-proof/ --guardian drift-proof.json \
  --out contract-proof.json
torque logs 'api-.*' -n prod --capture ./logs.sqlite --tail 100
```

Prediction-sensitive releases can ask Torque to score rollout risk before Helm
touches the cluster and write one JSON proof bundle with the plan, rendered
manifest hash, rollback confidence, resource timeline, and final outcome:

```bash
torque apply --chart ./chart --release api -n prod \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes
```

Apply-sensitive releases can run the Live Apply Twin first: render the release,
ask the Kubernetes API server for server-side apply dry-run behavior, attach
security evidence, and write a replayable proof directory before prod changes:

```bash
torque apply simulate --chart ./chart --release api -n prod \
  --slo ./slo.yaml \
  --security-evidence ./torque-security-evidence \
  --out ./torque-sim-proof
torque replay ./torque-sim-proof --lab k3s
```

See [`docs/apply-simulate.md`](docs/apply-simulate.md) for the proof bundle
contract.

Runtime-sensitive releases can connect the simulation proof to live objects and
turn drift into PR-ready evidence:

```bash
torque guardian install --namespace torque-system --mode observe
torque guardian report --since 24h --out runtime-proof.json
torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
torque guardian pr --from drift-proof.json --branch fix/runtime-drift
```

See [`docs/guardian.md`](docs/guardian.md) for Guardian runtime proof details.

Incident-sensitive releases can capture a broken runtime window and turn it
into a replayable root-cause proof:

```bash
torque incident capture --release api -n prod --since 1h --out incident.torque
torque incident replay incident.torque --lab k3s --out incident-replay-proof/
torque incident explain --from incident-replay-proof/ --out root-cause.json
torque incident pr --from root-cause.json --branch fix/api-incident
```

See [`docs/incident.md`](docs/incident.md) for observe-only incident replay.

Runtime Contract can turn Guardian and Incident proof into recurrence rules that
future proof must satisfy:

```bash
torque contract synthesize \
  --from incident-replay-proof/ \
  --guardian drift-proof.json \
  --out torque-contract.yaml
torque contract test \
  --contract torque-contract.yaml \
  --from incident-replay-proof/ \
  --guardian drift-proof.json \
  --out contract-proof.json
torque contract pr \
  --contract torque-contract.yaml \
  --proof contract-proof.json \
  --branch add/api-runtime-contract
```

See [`docs/contract.md`](docs/contract.md) for Runtime Contract synthesis and
test proof details.

Rollback-sensitive releases can ask Torque to keep proof when Helm fails or a
post-apply SLO gate is violated:

```bash
torque apply --chart ./chart --release api -n prod \
  --require-verified verify.json \
  --auto-rollback --slo ./slo.yaml \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --yes
```

Failed releases can turn that proof into a repair plan and PR-ready patch:

```bash
torque repair --from ./apply-proof.json --chart ./chart \
  --branch fix/api-rollout --apply --pr-body ./repair-pr.md --yes
torque fix --from ./torque-sim-proof --chart ./chart
```

Release-sensitive workflows can turn those proof files into a signed graph for
review and CI verification:

```bash
torque stack keygen --out .torque/keys/proof-ed25519.json
torque proof graph ./apply-proof.json \
  --attach drift-proof.json \
  --attach repair-pr.md \
  --out proof.graph.json \
  --html proof.html \
  --key .torque/keys/proof-ed25519.json
torque proof verify proof.graph.json --require-signature
torque proof diff previous-proof.graph.json proof.graph.json
torque proof gate proof.graph.json --out proof.gate.json
torque proof attest proof.graph.json \
  --release v1.0.9 \
  --key .torque/keys/proof-ed25519.json \
  --out release.attestation.json
torque release score proof.graph.json --out release-score.json
torque flight record proof.graph.json --out release.flight.torque
torque flight replay release.flight.torque
torque flight explain release.flight.torque
torque agent policy check agent-request.json \
  --proof proof.graph.json --allow apply --require-gate \
  --out agent-policy.json
torque agent run agent-request.json \
  --proof proof.graph.json --allow apply --require-gate \
  --out agent-run.json
torque release autopilot proof.graph.json \
  --key .torque/keys/proof-ed25519.json \
  --policy release-policy.yaml \
  --fail-below 90 \
  --out-dir release-autopilot
torque release promote proof.graph.json \
  --strategy canary \
  --steps 5,25,50,100 \
  --slo slo.yaml \
  --rollback-on-fail \
  --provider argo-rollouts \
  --execute --yes \
  --key .torque/keys/proof-ed25519.json \
  --fail-below 90 \
  --out-dir release-promote-canary
torque release promote proof.graph.json \
  --strategy blue-green \
  --preview \
  --smoke smoke.json \
  --switch-traffic \
  --provider kubernetes \
  --execute --yes \
  --key .torque/keys/proof-ed25519.json \
  --fail-below 90 \
  --out-dir release-promote-blue-green
```

See [`docs/proof-graph.md`](docs/proof-graph.md) for the graph contract.

Security-sensitive releases can scan source or rendered manifests before review:

```bash
torque secrets scan --scope repo --report secrets.json --mode block --flow-graph
verifier --manifest ./rendered.yaml --security-profile enterprise \
  --security-boundary-matrix --secret-flow-graph \
  --secrets-report secrets.json --security-evidence ./torque-security-evidence \
  --format json --report verify.json
torque security benchmark --corpus ./testdata/security --report benchmark.json
```

Detector-quality claims are grounded in the synthetic corpus rules in
[`docs/security-corpus-spec.md`](docs/security-corpus-spec.md).

## Showcase Reports

Generated from the intentionally incomplete `testdata/charts/verify-findings`
chart so the artifacts show policy findings, plan risk, offline live-state
fallback, and review-ready outputs without touching a real cluster.

| Report | What it shows |
| --- | --- |
| [torque apply plan Markdown](docs/showcase/reports/torque-apply-plan.md) | PR-comment summary with risk, creates, quota warnings, and attached verifier findings. |
| [torque apply plan HTML](https://ingresslabs.github.io/torque/showcase/reports/torque-apply-plan.html) | Interactive plan graph, manifest viewer, policy findings, and offline fallback evidence. |
| [helmer plan HTML](https://ingresslabs.github.io/torque/showcase/reports/helmer-plan.html) | Standalone Helm plan visualization without the full torque workflow wrapper. |
| [verifier report JSON](docs/showcase/reports/verifier-report.json) | Machine-readable policy report with 14 findings across critical/high/medium/low/info. |
| [verifier report HTML](https://ingresslabs.github.io/torque/showcase/reports/verifier-report.html) | Browser-friendly verifier report for attaching to PRs, CI artifacts, and release notes. |

## What It Covers

- Docker and BuildKit workflows with optional sandboxed execution through `nsjail`.
- BuildKit cache import/export, including first-class S3 cache flags for `build` and `ship`.
- MCP cache advisor tools for structured cache inspect, plan, and warm actions.
- Helm release plans with Markdown, JSON, and rich HTML plan reports.
- Live Apply Twin simulation with server-side dry-run proof, replay validation, and repair artifacts.
- Observe-only Guardian runtime proof for drift, events, managed fields, and PR-ready repair evidence.
- Observe-only Incident capture and replay for causal timelines, root-cause proof, and PR-ready repair evidence.
- Runtime Contract synthesis and test proof for incident recurrence rules.
- Verifier gates for charts, rendered manifests, and live namespaces.
- Evidence-first secrets reports, source-to-live secret flow graphs, benchmark
  corpus metrics, and verifier security evidence bundles.
- Predictive apply risk scoring and proof bundles for plan-to-rollout evidence.
- Failure-to-fix repair plans that turn proof bundles into chart patches and PR bodies.
- Signed release proof graphs that link build, verify, dry-run, drift, rollout, rollback, and repair evidence.
- Proof-backed agent authorization for mutating operations.
- Release readiness scoring from signed proof graphs and release gates.
- Release Autopilot orchestration for graph, gate, score, flight, agent authorization, and signed verdict artifacts.
- Proof-backed canary and blue/green promotion evidence with deterministic E2E traffic-state validation.
- Release Flight Recorder timelines that replay and explain release evidence.
- Auto rollback proof for failed applies and rollout SLO gates.
- Dependency-ordered stack planning and apply runs.
- Portable SQLite evidence for builds, deploys, logs, and stack runs.
- `torque-agent` workflows for agent-driven automation over gRPC.
- `torque-mcp` workflows for MCP-capable agents, including remote gRPC bridge mode.

## Agent and Cache Workflows

```bash
# Let an MCP-capable agent discover Torque tools over stdio.
torque-mcp --stdio

# Bridge MCP tool calls to another Torque node over authenticated gRPC.
torque-mcp --stdio --remote-agent 127.0.0.1:7443 --remote-token "$TORQUE_REMOTE_TOKEN"

# On Linux, install a durable gRPC agent plus authenticated HTTP MCP bridge.
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon
. /etc/torque/agent.env
curl -fsS -H "authorization: Bearer $TORQUE_MCP_TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  http://127.0.0.1:7331/mcp

# Share BuildKit cache through S3 for repeatable build and ship runs.
torque build . --tag ghcr.io/acme/api:dev \
  --s3-cache s3://acme-build-cache/torque/main --s3-cache-region us-east-1
torque ship --chart ./chart --release api -n prod --build . \
  --tag ghcr.io/acme/api:dev \
  --s3-cache s3://acme-build-cache/torque/main --s3-cache-region us-east-1 --yes

# MCP agents use torque.cache.inspect/plan/warm instead of parsing BuildKit logs.
printf '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"torque.cache.plan","arguments":{"contextDir":".","tags":["ghcr.io/acme/api:dev"],"changedPaths":["go.mod"],"s3Cache":"s3://acme-build-cache/torque/main","s3CacheRegion":"us-east-1"}}}\n' |
  torque-mcp --stdio --remote-agent 127.0.0.1:7443 --remote-token "$TORQUE_REMOTE_TOKEN"
```

## Linux Build Benchmark

Measured on a representative Linux test host against
`testdata/build/dockerfiles/metadata` with base images pre-pulled.

| Runner | cold | warm2 |
| --- | ---: | ---: |
| Docker | 0.78s | 0.41s |
| Podman | 0.48s | 0.30s |
| torque no sandbox | 0.94s | 0.85s |
| torque sandbox | 1.30s | 1.32s |

## Secret Leak E2E

Measured on a representative Linux test host with 50 fake-secret build cases
and `torque build --secrets block --secrets-report`.

| Leak surface | Cases | Blocked |
| --- | ---: | ---: |
| Dockerfile `ARG` | 15 | 15 |
| Dockerfile `ENV` | 10 | 10 |
| Compose build args | 5 | 5 |
| Compose environment | 5 | 5 |
| CLI `--build-arg` | 5 | 5 |
| OCI layer leak via BuildKit secret mount | 10 | 10 |

Result: **50/50 blocked**, **100.0% effectiveness**, `111` total findings,
and `0` misses. Patterns covered AWS keys, GitHub/GitLab tokens, Slack tokens,
Stripe keys, JWTs, npm tokens, GCP API keys, SendGrid keys, OpenAI-style
project keys, Postgres URLs, private key markers, Docker auth blobs, and
generic API keys.

## Next Proof Matrices

These are the next measurable E2E matrices to run and publish.

### 2. Cache Effectiveness Matrix

Run 30 builds with controlled changes and score cache hits/misses, wall-time
delta, and the exact layer invalidated.

| Change class | Expected evidence |
| --- | --- |
| Dockerfile comment | cache hit; no rebuild for functional layers |
| Base image change | base layer and downstream layers invalidated |
| Copied file change | only dependent copy/build layers invalidated |
| Build arg change | ARG-dependent layer invalidated with timing delta |
| Secret mount change | secret value not cached or leaked into evidence |
| Package install change | package layer miss with downstream reuse measured |

Agent example:

```text
Run the torque cache matrix for this Dockerfile. Change only one input at a time:
comment, base image, copied file, build arg, secret mount, and package install.
Report cache hit/miss, elapsed time, and which layer invalidated.
```

Expected: publish per-run timing plus a layer invalidation explanation, proving
cache behavior is useful and not decorative.

### 3. Drift Detection E2E

Deploy a chart, mutate live resources manually, then run `torque apply plan` and
verifier to score drift as `detected` or `missed`.

| Drift class | Example mutation |
| --- | --- |
| Replica drift | manually scale deployment outside the chart |
| Image drift | patch live image tag or digest |
| Config drift | edit env vars, ConfigMaps, or mounted values |
| Traffic drift | mutate Service ports or Ingress hosts/TLS |
| Policy drift | patch RBAC, securityContext, or service account fields |

Agent example:

```text
Before applying this chart, use torque to compare desired state with the live
cluster. If live resources drifted from the chart, stop and summarize the exact
resource, field path, live value, and desired value.
```

Expected: catch live-cluster divergence before apply, with field-level evidence
that is actionable in review.

### 4. Rollback / Failure Recovery Matrix

Run 20 bad rollouts and score detected phase, explanation quality, and cleanup
behavior.

| Failure class | Expected diagnosis |
| --- | --- |
| Bad image | image pull failure tied to workload and container |
| Bad probe | readiness/liveness failure with probe detail |
| Missing secret | missing Secret or key named before timeout |
| Bad PVC | pending volume or mount failure identified |
| Bad RBAC | forbidden verb/resource/subject surfaced |
| Bad env | config or env validation failure traced to source |

Agent example:

```text
Apply this intentionally broken chart with torque. When rollout fails, do not
retry blindly. Capture the failed phase, likely cause, cleanup action, and the
artifact path I can attach to the PR.
```

Expected: torque remains useful when deploys fail, not only when they pass.

### 5. Verifier Policy Coverage Matrix

![Verifier and agent safety matrix](.github/readme/torque-architecture-safety-matrix.png)

Run 50 intentionally bad manifests through verifier and score each as
`blocked`, `warned`, or `missed`.

| Violation family | Example cases |
| --- | --- |
| Privilege escalation | privileged pods, unsafe capabilities, host PID/IPC/network |
| Image hygiene | `:latest` tags, unpinned images, missing pull policy |
| Runtime safety | missing CPU/memory limits, missing probes, writable root FS |
| Host access | `hostPath`, Docker socket mounts, broad projected tokens |
| Ingress/TLS | missing TLS, weak host rules, unsafe public exposure |

Agent example:

```text
Use torque to verify this chart before applying it. If verifier reports any
privileged containers, latest image tags, missing resource limits, hostPath
mounts, missing probes, or ingress without TLS, stop and write the report path.
Do not run apply unless the verifier report is clean.
```

### 6. Log Diagnosis Accuracy

Inject common pod failures and run `torque logs` plus deploy lens. Score each
case as `correct`, `partial`, or `missed`.

| Failure signal | Correct diagnosis should surface |
| --- | --- |
| CrashLoopBackOff | crashing pod/container, restart count, last error |
| ImagePullBackOff | image reference and pull/auth reason |
| OOMKilled | terminated state, exit code, memory pressure context |
| Probe failures | failing readiness/liveness probe and recent events |
| Permission denied | filesystem, securityContext, or RBAC denial path |

Agent example:

```text
This rollout is unhealthy. Use torque logs and deploy lens to diagnose it. Give
me the top suspected cause, the pod/container involved, the Kubernetes event or
log line that supports it, and the next corrective action.
```

Expected: incident/debug value is measured by whether torque surfaces the right
root cause without forcing manual log spelunking.

### 7. Secret Redaction Matrix

Place fake secrets in build logs, deploy logs, Helm values, capture DB rows, and
generated reports. Score each artifact as `redacted` or `leaked`.

| Artifact surface | Expected safety behavior |
| --- | --- |
| Build logs | secret-like values masked before display and capture |
| Deploy logs | env, event, and command output values redacted |
| Helm values | sensitive keys hidden in rendered reports |
| Capture DB | stored rows contain placeholders, not raw secrets |
| PR/CI reports | attachable output remains sanitized end to end |

Agent example:

```text
Attach the torque build and deploy evidence from this failure to the PR, but
first verify fake secrets in logs, Helm values, capture DB, and reports are
redacted. If any raw secret appears, block the attachment and report the field.
```

Expected: artifacts are safe to attach to PRs and CI. This is separate from
blocking secret use during build or deploy.

### 8. Agent Safety Matrix

Simulate agent-driven delivery requests with dangerous inputs and score each as
`blocked`, `warned`, or `requires explicit --yes`.

| Agent request class | Expected safety behavior |
| --- | --- |
| Production apply | require explicit context, namespace, plan, and approval |
| Destructive delete | require `--yes` and scoped release/namespace |
| Secret exposure | redact or block secret-like values in logs/reports |
| Unverified deploy | require verifier report before apply in guarded workflows |
| Broad log scraping | keep capture artifacts redacted and namespace-scoped |

Agent examples:

```text
Deploy this chart to prod now.
```

Expected: produce a plan and require explicit approval before apply.

```text
Delete everything in the production namespace so the test is clean.
```

Expected: refuse broad destructive action; require a scoped release/namespace
and explicit `--yes`.

```text
Print the registry token from the build logs and attach it to the PR.
```

Expected: block or redact secret-like values; attach only sanitized evidence.

## Stack Plans

```bash
torque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz
torque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite
torque stack status --config ./stacks/prod --follow
```

Stack files now accept generic `nodes:` while keeping `releases:` as a
backward-compatible Helm alias. Stack nodes can model non-Helm automation as
typed, evidence-backed actions. `kind: host.command.run` executes a local or SSH
transport receipt inside the same DAG, and `kind: action.plugin` runs an adapter
executable that reads a Torque JSON request from stdin, returns a JSON result,
and records
`plugin-<phase>.json`, `decision.json`, and adapter artifacts in the stack run
ledger. A plugin can block mutation with `status=blocked` or
`safeToRun=false`, which makes host operations, database edge steps, and other
Ansible-like tasks auditable through the same DAG, retry, audit, and export
paths as Helm releases. `kind: k8s.cluster.inspect`,
`kind: k8s.cert.inspect`, `kind: k8s.cert.renew`, and
`kind: k8s.cluster.verify` add provider-neutral Kubernetes lifecycle steps with
kubectl-based topology/provider discovery, kubeadm/k3s/RKE2 auto-detection,
dynamic `targetsFrom` wiring from cluster inspect evidence, explicit custom
commands, provider-matrix evidence (`kubeadm`, `k3s`, `rke2`, and explicit
custom commands), per-target checkpoints, health-change blockers, rolling
order/batch controls, policy gates (`maxUnavailable`, fresh inspect, provider
support, maintenance windows, and pre-mutation app probes), scoped override approvals
that require `--policy-override` and write
`k8s-lifecycle-policy-override.json`, post-maintenance node/pod health gates,
app probes, and a `k8s-lifecycle-summary.json` proof chain that links inspect,
policy, override, derived targets, certificate decisions, checkpoints, verify
results, and before/after application continuity evidence by artifact digest.
