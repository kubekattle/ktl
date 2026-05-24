# torque

<p align="center">
  <a href="https://ingresslabs.github.io/torque/"><strong>Docs and live demos</strong></a> |
  <a href="https://github.com/ingresslabs/torque/actions/workflows/ci.yml">CI</a> |
  <a href="https://github.com/ingresslabs/torque/releases">Releases</a> |
  <a href="./LICENSE">License</a>
</p>

`torque` is an agent-first delivery CLI for Kubernetes and production platform
operations. It keeps builds, plans, applies, verification, logs, stack
execution, and release evidence file-first so humans, CI, and AI agents can
review the same artifacts.

The project is built around one idea: automation should be powerful, but every
mutation should leave a readable proof trail. Torque can build images, verify
charts, render apply plans, capture rollout evidence, inspect runtime state,
package stack graphs, and gate releases with signed proof bundles instead of
hidden service state.

Use it when a delivery path crosses more than one boundary: source, image,
chart, cluster, host, data plane, public endpoint, and release review. Torque
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
```

## Core Loop

```bash
torque build . --tag ghcr.io/acme/api:dev --capture build.sqlite
verifier --chart ./chart --release api -n prod --format json --report verify.json
torque apply plan --chart ./chart --release api -n prod --verify-report verify.json
torque apply --chart ./chart --release api -n prod --capture apply.sqlite --yes
torque proof graph apply-proof.json --out proof.graph.json --html proof.html
torque proof gate proof.graph.json --out proof.gate.json
```

## Ops Foundations

```bash
torque ops inventory show --targets ./targetgraph.yaml --selector role=web
torque ops inventory snapshot --source ./targetgraph.yaml --type file --format json
torque ops facts collect --targets ./targetgraph.yaml --selector role=web --out-dir ./ops-facts-evidence --format json
torque ops lock acquire --scope target/host-01 --holder operator --operation host.command.run
torque ops policy check --mode guarded --operation host.command.run --mutating --approved
```

## What It Covers

- Proof-backed Kubernetes apply, rollback, repair, drift, and incident replay.
- Stack packaging for multi-tool deployments that span hosts, clusters, apps,
  data services, public checks, and verification.
- Ops target graphs, inventory views, host/Kubernetes fact snapshots, target
  locks, and mutation policy checks.
- Agent policy checks so mutating operations can require proof and release gates.
- Release scoring, autopilot, canary, blue/green, and flight recorder workflows.

## Learn More

Start with the published site: <https://ingresslabs.github.io/torque/>.

Useful docs live under [`docs/`](docs/), examples under [`testdata/`](testdata/),
and real stack packaging examples under [`stacks/`](stacks/).
