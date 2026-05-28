# torque

<p align="center">
  <a href="https://ingresslabs.github.io/torque/"><strong>Docs and live demos</strong></a> |
  <a href="https://github.com/ingresslabs/torque/actions/workflows/ci.yml">CI</a> |
  <a href="https://github.com/ingresslabs/torque/releases">Releases</a> |
  <a href="./LICENSE">License</a>
</p>

`torque` is an agent-first delivery CLI for Kubernetes platform releases. It
keeps builds, plans, applies, verification, logs, stack execution, and release
evidence file-first so humans, CI, and AI agents can review the same artifacts.

The project is built around one idea: automation should be powerful, but every
mutation should leave a readable proof trail. Torque can build images, compile
platform stack plans, capture rollout evidence, inspect runtime state, package
release graphs, and gate releases with signed proof bundles instead of hidden
service state.

Use it when a delivery path crosses more than one boundary: source, image,
Helm release, cluster, data plane, public endpoint, and release review. Torque
does not replace Helm, Kubernetes, CI, or observability tools; it gives the
workflow around them a graph, a capture format, and a verification gate.

## Install

```bash
curl -fsSL https://ingresslabs.github.io/torque/install.sh | sh
```

From source:

```bash
go install github.com/ingresslabs/torque/cmd/torque@latest
go install github.com/ingresslabs/torque/cmd/torque-agent@latest
go install github.com/ingresslabs/torque/cmd/verifier@latest

# From a repo checkout:
make install-helmer
```

## Platform Loop

```bash
torque build ./app --tag ghcr.io/acme/payments-api:dev --capture build.sqlite
torque stack plan --config ./stacks/fraud-platform --bundle dist/platform-plan.tgz
torque stack seal --config ./stacks/fraud-platform --out dist/platform-release --concurrency 2
torque stack apply --sealed-dir dist/platform-release --capture dist/platform.sqlite --yes
torque stack audit --output html > dist/platform-audit.html
torque stack export --out dist/platform-run.tgz
torque stack audit --from-bundle dist/platform-run.tgz --output json > dist/platform-audit.json
```

## Core Premise

Torque deploys platforms, not isolated charts. A Helm chart can be one node in
the graph, but the reviewable unit is the stack package: images, releases,
values, scripts, public checks, data services, verification, capture files, and
the final exportable run bundle. That keeps complex delivery paths repeatable
without hiding the important decisions in CI logs or a control-plane database.

## What It Covers

- Proof-backed Kubernetes apply, rollback, repair, drift, and incident replay.
- Stack packaging for multi-tool deployments that span hosts, clusters, apps,
  data services, public checks, and verification.
- Ops target inventory, adapter capability discovery, and evidence-backed host
  command/file/package/service/user/cron/systemd plus Kubernetes manifest and
  readiness/log/event automation.
- Terraform/OpenTofu provider resources as typed stack modules with saved-plan
  apply/delete, state digests, redacted receipts, and audit artifacts.
- Provider ecosystem hardening path for generated module packs, Ops cloud
  targets, Fleet execution, and deterministic 100-node provider tests.
- Agent-backed NATS command execution, targeted fleet fan-out, durable
  JetStream assignments/receipts, worker idempotency ledgers, capability
  reporting, worker-side capability and slot-lease enforcement, renewable
  target slot ledgers, worker-owned slot lease renewal/release, resume-safe slot
  lease ownership, and identity receipts for stack nodes that need outbound
  worker fan-out without changing stack semantics.
- Optional fleet control-plane design for Kubernetes, dedicated etcd inventory,
  NATS JetStream assignment/receipt streams, readiness/capability gates, and
  10,000-host durable agents.
- Agent policy checks so mutating operations can require proof and release gates.
- Agent appliance evidence bundles for repo intelligence, browser captures, API
  probes, and command checks that Codex, Claude, OpenCode, CI, and humans can
  inspect from the same files.
- Release scoring, autopilot, canary, blue/green, and flight recorder workflows.

## Tools

- `helmer`: standalone chart archive and Helm plan/report tool. It reuses the Torque plan engine while keeping `torque apply plan` available for the full delivery workflow.
- `verifier`: standalone Kubernetes policy verifier for Helm charts, rendered manifests, and live namespaces.
- `torque-package`: compatibility archive-only helper; prefer `helmer archive`, `helmer verify-archive`, and `helmer unpack` for new workflows.

```bash
torque agent appliance run . \
  --actor codex \
  --task "review checkout regression" \
  --api-url http://localhost:3000/api/health \
  --browser-url http://localhost:3000/checkout \
  --check "go test ./internal/checkout"
```

## Learn More

Start with the published site: <https://ingresslabs.github.io/torque/>.

Useful docs live under [`docs/`](docs/), examples under [`testdata/`](testdata/),
and real stack packaging examples under [`stacks/`](stacks/).
