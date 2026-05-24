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
```

## Platform Loop

```bash
torque build ./app --tag ghcr.io/acme/payments-api:dev --capture build.sqlite
torque stack plan --config ./stacks/fraud-platform --bundle dist/platform-plan.tgz
torque stack seal --config ./stacks/fraud-platform --out dist/platform-release --concurrency 2
torque stack apply --sealed-dir dist/platform-release --capture dist/platform.sqlite --yes
torque stack audit --output html > dist/platform-audit.html
torque stack export --out dist/platform-run.tgz
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
- Agent policy checks so mutating operations can require proof and release gates.
- Release scoring, autopilot, canary, blue/green, and flight recorder workflows.

## Learn More

Start with the published site: <https://ingresslabs.github.io/torque/>.

Useful docs live under [`docs/`](docs/), examples under [`testdata/`](testdata/),
and real stack packaging examples under [`stacks/`](stacks/).
