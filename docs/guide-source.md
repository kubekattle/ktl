% The KTL Handbook: Modern Kubernetes Development
% Anton Krylov
% February 2026

# Introduction

**ktl** (Kubernetes Tool) is a single, developer-centric binary designed to bridge the gap between interactive local workflows and headless CI pipelines. It unifies the functionality of multiple fragmented tools (kubectl, helm, stern, docker build) into a coherent, opinionated suite.

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

**ktl** provides a unified interface for the entire lifecycle:
1.  **Build**: Integrated BuildKit support (no local Docker daemon required).
2.  **Deploy**: DAG-aware stack orchestration (replaces Helmfile).
3.  **Debug**: Zero-config multi-pod log tailing and smart tunnels.
4.  **Analyze**: AI-powered diagnostics for crashing pods.

---

# Installation & Setup

## Prerequisites

- Go 1.23+ (if building from source)
- Access to a Kubernetes cluster (local or remote)

## Installation

```bash
go install github.com/kubekattle/ktl@latest
```

## Quick Start

1.  **Verify access**:
    ```bash
    ktl logs -n default
    ```
    This command will automatically tail all pods in the `default` namespace.

2.  **Analyze a failing pod**:
    ```bash
    ktl analyze pod/my-failing-pod --ai
    ```

---

# Core Concepts

## 1. The Stack (`ktl stack`)

A **Stack** is a collection of Kubernetes resources (Helm charts, raw manifests, Kustomizations) that need to be deployed together. Unlike simple scripts, `ktl` treats a stack as a **Directed Acyclic Graph (DAG)**.

### Key Features
- **Dependency Management**: Define `needs: [backend]` in your frontend component, and `ktl` ensures they deploy in the correct order.
- **Parallel Execution**: Independent components are deployed concurrently, significantly speeding up cold starts.
- **State Tracking**: `ktl` tracks the state of each release. If a deployment fails, you can fix the issue and resume exactly where you left off.

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

## 2. The Build System (`ktl build`)

`ktl` includes an embedded BuildKit client. This means you can build container images efficiently without relying on a local Docker daemon.

### Key Features
- **Hermetic Builds**: Enforce reproducible builds by disabling network access during the build phase (except for pinned base images).
- **Sandboxing**: (Linux only) Run builds inside an `nsjail` sandbox for extreme security.
- **Cache Intelligence**: Get detailed reports on cache hits/misses to optimize your Dockerfiles.

## 3. The Tunnel (`ktl tunnel`)

Port-forwarding is essential but often painful. `ktl tunnel` makes it smart.

### Key Features
- **Environment Injection**: Run a local binary, but inject environment variables (like DB credentials) from a remote Kubernetes deployment.
- **Fault Injection**: Intentionally introduce latency or errors to test your application's resilience.
- **Dependency Tunneling**: Automatically set up tunnels for all upstream dependencies defined in your stack.

---

# Workflow Scenarios

## Scenario 1: The "Fix & Resume" Loop

Imagine deploying a complex stack of 10 microservices. Service #5 fails due to a config error.

**Without ktl**:
You fix the config, then either re-run the whole script (slow) or manually helm upgrade that one service (error-prone).

**With ktl**:
1.  `ktl stack apply` fails at node #5.
2.  You fix the code/config.
3.  Run:
    ```bash
    ktl stack apply --only service-5
    ```
    Or simply re-run the original command; `ktl` sees that services 1-4 are already "Succeeded" and skips them (idempotency).

## Scenario 2: Debugging a CrashLoopBackOff

A pod is crashing, and you don't know why.

**Without ktl**:
1.  `kubectl get pods` (Status: CrashLoopBackOff)
2.  `kubectl logs pod-xyz` (Maybe the error is at the end?)
3.  `kubectl logs pod-xyz --previous` (Maybe it crashed immediately?)
4.  `kubectl describe pod` (OOMKilled?)

**With ktl**:
```bash
ktl analyze pod-xyz --ai
```
The tool automatically:
- Checks resource limits vs usage (OOM detection).
- Scans events for scheduling issues.
- Grabs logs (current and previous).
- Correlates stack traces in logs with your *local* source code.
- Uses AI to explain the root cause and suggest a fix.

## Scenario 3: Local Development with Remote Dependencies

You are working on the `frontend` service. It depends on `backend`, `redis`, and `postgres` running in the cluster.

**With ktl**:
```bash
ktl tunnel frontend --deps --env-from deployment/frontend --exec "npm start"
```
This command:
1.  Reads `stack.yaml` to find dependencies.
2.  Opens tunnels to `backend`, `redis`, and `postgres`.
3.  Fetches env vars from the remote `frontend` deployment.
4.  Starts your local `npm start` process with all connectivity and config pre-wired.

---

# Advanced Features

## AI Diagnostics

`ktl analyze` supports multiple AI providers:
- **OpenAI** (`--provider openai`): Uses GPT-4/GPT-5 models.
- **Local/Mock**: For testing or offline usage.

You can override the model used:
```bash
ktl analyze pod/foo --ai --model gpt-4o
```

## Security & Governance

`ktl verify` allows platform engineers to enforce policies:
- **RBAC**: Ensure no ClusterRoles use wildcards.
- **PSS**: Enforce Pod Security Standards (Restricted/Baseline).
- **Custom Rules**: Write your own Rego policies.

---

# Command Reference

## ktl apply
Apply a manifest or helm chart with instant log streaming.

**Usage**: `ktl apply [flags]`
- `--chart`: Path to helm chart.
- `--watch`: Stream logs after apply.

## ktl stack
Manage complex multi-component releases.

**Usage**: `ktl stack [apply|delete|plan]`
- `--config`: Path to `stack.yaml`.
- `--parallel`: Max concurrent deployments (default 4).

## ktl logs
Tail logs from multiple pods.

**Usage**: `ktl logs [flags]`
- `-n`: Namespace.
- `-l`: Label selector.
- `--tail`: Number of lines.

## ktl analyze
Diagnose pod failures.

**Usage**: `ktl analyze [POD] [flags]`
- `--ai`: Enable AI analysis.
- `--fix`: Apply suggested patches.
- `--cluster`: Run cluster-wide checks.

---

# Troubleshooting

## Common Issues

### "Metrics server not found"
If `ktl analyze` complains about missing metrics:
- Ensure `metrics-server` is running in `kube-system`.
- Note: Crashing pods do not report metrics. `ktl` handles this gracefully by warning you instead of failing.

### BuildKit Connection Failed
- Ensure `buildkitd` is running locally or configured via `KTL_BUILDKIT_HOST`.
- On macOS, `ktl` looks for the socket at `~/.colima/default/docker.sock` or standard locations.

---

# Contributing

We welcome contributions! Please see `AGENTS.md` for our internal architectural guidelines and agent protocols.

## Principles
1.  **Single Binary**: No external runtime dependencies if possible.
2.  **Developer Experience First**: meaningful error messages, colors, and spinners.
3.  **Idempotency**: All operations should be safe to retry.
