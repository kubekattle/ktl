% The TORQUE Handbook: Modern Kubernetes Development
% Anton Krylov
% February 2026

# Introduction

**torque** (Kubernetes Tool) is a deploy workflow toolkit designed to bridge the gap between interactive local rollouts, reviewable PR artifacts, and headless CI pipelines. It focuses on planning, applying, capturing, and explaining Kubernetes changes with enough evidence for review and incident response.

## The Problem

Kubernetes development often involves "tool sprawl":
- **kubectl** for imperative commands.
- **Helm** for package management.
- **Stern/kail** for log tailing.
- **Docker/BuildKit** for image building.
- **Skaffold/Tilt** for dev loops.
- **Bash scripts** to glue it all together.

This fragmentation leads to context switching, inconsistent environments between dev and CI, and a steep learning curve for new team members.

## The Solution

**torque** provides a unified interface for the entire lifecycle:
1.  **Build**: Integrated BuildKit support (no local Docker daemon required).
2.  **Deploy**: DAG-aware stack orchestration (replaces Helmfile).
3.  **Observe**: Zero-config multi-pod log tailing and rollout-aware status.
4.  **Capture**: Portable SQLite evidence files that can be inspected after the run.

---

# Installation & Setup

## Prerequisites

- Go 1.23+ (if building from source)
- Access to a Kubernetes cluster (local or remote)

## Installation

```bash
go install github.com/ingresslabs/torque/cmd/torque@latest
```

## Quick Start

1.  **Verify access**:
    ```bash
    torque logs -n default
    ```
    This command will automatically tail all pods in the `default` namespace.

2.  **Preview a deploy**:
    ```bash
    torque apply plan --chart ./chart --release my-app -n default --visualize
    ```

3.  **Simulate live API behavior**:
    ```bash
    torque apply simulate --chart ./chart --release my-app -n default --out ./torque-sim-proof
    torque replay ./torque-sim-proof --lab k3s
    ```

4.  **Prove runtime drift**:
    ```bash
    torque guardian diff --source ./torque-sim-proof --live --out drift-proof.json
    torque guardian pr --from drift-proof.json --branch fix/runtime-drift
    ```

5.  **Replay incident evidence**:
    ```bash
    torque incident capture --release api -n prod --since 1h --out incident.torque
    torque incident replay incident.torque --lab k3s --out incident-replay-proof
    torque incident explain --from incident-replay-proof --out root-cause.json
    ```

---

# Core Concepts

## 1. The Stack (`torque stack`)

A **Stack** is a collection of Kubernetes resources (Helm charts, raw manifests, Kustomizations) that need to be deployed together. Unlike simple scripts, `torque` treats a stack as a **Directed Acyclic Graph (DAG)**.

### Key Features
- **Dependency Management**: Define `needs: [backend]` in your frontend component, and `torque` ensures they deploy in the correct order.
- **Parallel Execution**: Independent components are deployed concurrently, significantly speeding up cold starts.
- **State Tracking**: `torque` tracks the state of each release. If a deployment fails, you can fix the issue and resume exactly where you left off.

### Example `stack.yaml`

```yaml
version: v1
releases:
  - name: postgres
    chart: bitnami/postgresql
    values:
      postgresqlPassword: secret

  - name: backend
    chart: ./charts/backend
    needs: [postgres]
    wait: true

  - name: frontend
    chart: ./charts/frontend
    needs: [backend]
```

## 2. The Build System (`torque build`)

`torque` includes an embedded BuildKit client. This means you can build container images efficiently without relying on a local Docker daemon.

### Key Features
- **Hermetic Builds**: Enforce reproducible builds by disabling network access during the build phase (except for pinned base images).
- **Sandboxing**: (Linux only) Run builds inside an `nsjail` sandbox for extreme security.
- **Cache Intelligence**: Get detailed reports on cache hits/misses to optimize your Dockerfiles.

# Workflow Scenarios

## Scenario 1: The "Fix & Resume" Loop

Imagine deploying a complex stack of 10 microservices. Service #5 fails due to a config error.

**Without torque**:
You fix the config, then either re-run the whole script (slow) or manually helm upgrade that one service (error-prone).

**With torque**:
1.  `torque stack apply` fails at node #5.
2.  You fix the code/config.
3.  Run:
    ```bash
    torque stack apply --only service-5
    ```
    Or simply re-run the original command; `torque` sees that services 1-4 are already "Succeeded" and skips them (idempotency).

## Scenario 2: Reviewing And Debugging A Failed Rollout

A release fails and you need to understand what changed, which resources became unhealthy, and what evidence is available for a follow-up review.

**Without torque**:
1.  `helm upgrade --install ...`
2.  `kubectl get pods` and `kubectl describe` across several resources.
3.  `kubectl logs` by hand.
4.  Copy terminal output into an issue after the context has already drifted.

**With torque**:
```bash
torque apply plan --chart ./chart --release api -n prod --visualize
torque apply --chart ./chart --release api -n prod \
  --predict --proof-bundle ./apply-proof.json \
  --capture ./apply.sqlite --ui
torque proof graph ./apply-proof.json \
  --out proof.graph.json \
  --html proof.html
torque proof verify proof.graph.json
torque proof gate proof.graph.json --out proof.gate.json
torque release score proof.graph.json --out release-score.json
torque release autopilot proof.graph.json \
  --key .torque/keys/proof-ed25519.json \
  --out-dir release-autopilot
torque release promote proof.graph.json \
  --strategy canary --steps 5,25,50,100 \
  --slo slo.yaml --rollback-on-fail \
  --out-dir release-promote-canary
torque release promote proof.graph.json \
  --strategy blue-green --preview --smoke smoke.json \
  --switch-traffic --out-dir release-promote-blue-green
torque flight record proof.graph.json --out release.flight.torque
torque agent policy check agent-request.json \
  --proof proof.graph.json --allow apply --require-gate
tar -czf torque-evidence.tgz ./apply.sqlite ./apply-proof.json ./proof.graph.json ./proof.html
```
The workflow keeps the plan artifact, predictive risk score, rollback confidence, rollout timeline, resource readiness updates, logs, Helm release summary, rendered manifest, command inputs, proof graph, release gate, readiness score, flight recorder, and agent authorization record together as durable evidence.

# Advanced Features

## Capture Evidence

Command-level `torque ... --capture` flags record deploy, destroy, build, and log sessions into a portable SQLite file. Store that file as a CI artifact or incident attachment so later diagnostics can explain the run without re-running against the cluster.

## Security & Governance

`verifier` allows platform engineers to enforce policies:
- **RBAC**: Ensure no ClusterRoles use wildcards.
- **PSS**: Enforce Pod Security Standards (Restricted/Baseline).
- **Custom Rules**: Write your own Rego policies.
- **Secret flow evidence**: block secret-like values rendered into non-Secret
  resources and write a redaction proof.

---

# Command Reference

## torque apply
Apply a manifest or helm chart with instant log streaming.

**Usage**: `torque apply [flags]`
- `--chart`: Path to helm chart.
- `--watch`: Stream logs after apply.
- `--predict --proof-bundle ./apply-proof.json`: Score rollout risk before apply and write a JSON bundle with plan, prediction, history, resource status, and final outcome.
- `--auto-rollback --slo ./slo.yaml`: Roll back failed applies or violated rollout SLO gates and write rollback proof.

## torque repair
Turn a failed apply proof bundle into a repair plan, optional chart patch, and PR body.

**Usage**: `torque repair --from ./apply-proof.json --chart ./chart [flags]`
- `--apply`: Write safe generated repair templates into the chart.
- `--branch fix/api-rollout`: Create/switch to a repair branch before writing files when the chart is in a clean git worktree.
- `--pr-body ./repair-pr.md`: Write a Markdown PR body with root cause, evidence, patch plan, and validation commands.

## torque proof
Build, verify, diff, gate, and attest release proof graphs.

**Usage**: `torque proof graph ./apply-proof.json [flags]`
- `--out proof.graph.json`: Write the graph JSON artifact.
- `--html proof.html`: Write a browser-readable report.
- `--key .torque/keys/proof-ed25519.json`: Sign with an ed25519 key from `torque stack keygen`.
- `--attach drift-proof.json`: Attach extra proof evidence such as Guardian drift, logs, SBOM, provenance, SLO, or repair artifacts.

**Verify**: `torque proof verify proof.graph.json --require-signature`

**Diff**: `torque proof diff previous-proof.graph.json current-proof.graph.json`

**Gate**: `torque proof gate proof.graph.json --out proof.gate.json`

**Attest**: `torque proof attest proof.graph.json --release v1.0.9 --key .torque/keys/proof-ed25519.json --out release.attestation.json`

## torque agent
Authorize AI or automation agent operations with proof-backed permissions.

**Policy check**: `torque agent policy check agent-request.json --proof proof.graph.json --allow apply --require-gate`

**Run record**: `torque agent run agent-request.json --proof proof.graph.json --allow apply --require-gate --out agent-run.json`

`agent run` is intentionally non-mutating. It records authorization for a caller
that will invoke the write operation through its own controlled path.

## torque release autopilot
Run the release autopilot over an existing proof source or proof graph. The
default mode is non-mutating: it builds and signs the graph, evaluates the
release gate, writes the score, records/replays/explains the release flight,
checks agent authorization, and signs a release verdict.

**Usage**: `torque release autopilot proof.graph.json [flags]`
- `--out-dir release-autopilot`: Write all autopilot artifacts into one directory.
- `--key .torque/keys/proof-ed25519.json`: Sign the graph and release attestation.
- `--policy release-policy.yaml`: Evaluate a custom release policy.
- `--fail-below 90`: Block when the readiness score is too low.
- `--execute --yes --chart ./chart --release api -n prod`: Run `torque apply` first, then collect proof.

## torque release promote
Promote a release only after proof gate, score, flight, SLO/smoke evidence, and
agent authorization checks pass.

**Canary**: `torque release promote proof.graph.json --strategy canary --steps 5,25,50,100 --slo slo.yaml --rollback-on-fail`

**Blue/green**: `torque release promote proof.graph.json --strategy blue-green --preview --smoke smoke.json --switch-traffic`

- `--analysis-window 5m`: Record the per-step analysis window.
- `--provider evidence`: Default non-mutating provider that writes promotion proof only.
- `--provider file --execute --yes --state-out traffic-state.json`: Deterministic E2E provider that writes final traffic state after checks pass.
- `--provider kubernetes --execute --yes`: Apply native Kubernetes canary replica shifts or blue/green Service selector switches after proof checks pass.
- `--provider argo-rollouts --execute --yes`: Configure an Argo Rollouts Rollout strategy from the proof-backed promotion plan.
- `--key .torque/keys/proof-ed25519.json`: Sign the promoted graph and promotion attestation.

## torque release score
Score release readiness from a signed proof graph and release gate checks.

**Usage**: `torque release score proof.graph.json [flags]`
- `--policy release-policy.yaml`: Evaluate a custom release policy.
- `--pub .torque/keys/proof-ed25519.json`: Verify with an explicit trusted key.
- `--out release-score.json`: Write machine-readable score JSON.
- `--fail-below 90`: Exit non-zero when the score is too low for promotion.

## torque flight
Record, replay, and explain the release flight timeline from proof evidence.

**Record**: `torque flight record proof.graph.json --out release.flight.torque`

**Replay**: `torque flight replay release.flight.torque`

**Explain**: `torque flight explain release.flight.torque`

## torque secrets scan
Scan source files, rendered manifests, build inputs, or text artifacts for
secret-like values without writing raw values to reports.

**Usage**: `torque secrets scan --scope repo|render|build|artifact [flags]`
- `--report secrets.json`: Write the JSON secret scan report.
- `--mode block --fail-on high`: Fail when high-confidence findings are present.
- `--flow-graph`: Include a redacted source-to-rendered/live-boundary-to-report flow graph.
- `--scope render --manifest ./rendered.yaml`: Scan rendered Kubernetes objects and allow Secret boundaries.

## torque security benchmark
Run the checked-in security corpus and publish evidence-backed detector metrics.

**Usage**: `torque security benchmark --corpus ./testdata/security --report benchmark.json`
- `--corpus`: Directory containing `corpus.yaml` and fixture cases.
- `--report benchmark.json`: Write recall, precision, false-positive, runtime, redaction, flow-graph, provenance-chain, and boundary metrics.
- `--live-k3s-boundary-matrix --live-confirm`: Also probe a temporary live namespace and include the k3s boundary pass/fail status.

## verifier security evidence
Merge secret-flow findings into verifier output and write a review bundle.

**Usage**: `verifier --chart ./chart --release api -n prod --security-profile enterprise --security-boundary-matrix --secret-flow-graph --secrets-report secrets.json --security-evidence ./torque-security-evidence`
- `--security-profile enterprise`: Enable blocking evidence-first secret checks.
- `--security-boundary-matrix`: Add a Secret/ConfigMap/env/log-facing boundary proof to the secrets report and evidence bundle.
- `--secret-flow-graph`: Export `secret.flow.graph.json` with values, template, rendered-object, live-object, boundary, and report nodes.
- `--secrets-report secrets.json`: Write the redacted secret scan report.
- `--security-evidence ./dir`: Export manifest, verifier report, secrets report, boundary matrix, flow graph, redaction proof, and Markdown summary.

## torque stack
Manage complex multi-component releases.

**Usage**: `torque stack [apply|delete|plan]`
- `--config`: Path to `stack.yaml`.
- `--parallel`: Max concurrent deployments (default 4).

## torque logs
Tail logs from multiple pods.

**Usage**: `torque logs [flags]`
- `-n`: Namespace.
- `-l`: Label selector.
- `--tail`: Number of lines.

# Troubleshooting

## Common Issues

### BuildKit Connection Failed
- Ensure `buildkitd` is running locally or configured via `TORQUE_BUILDKIT_HOST`.
- On macOS, `torque` looks for the socket at `~/.colima/default/docker.sock` or standard locations.

---

# Contributing

We welcome contributions! Please see `AGENTS.md` for our internal architectural guidelines and agent protocols.

## Principles
1.  **Focused toolkit**: Keep `torque` focused on the deploy workflow and include companion binaries only when they serve a distinct review job.
2.  **Developer Experience First**: meaningful error messages, colors, and spinners.
3.  **Idempotency**: All operations should be safe to retry.
