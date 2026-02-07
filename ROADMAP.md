# 🚀 ktl Roadmap: The Path to "Best-in-Class"

To make `ktl` the irreplaceable Kubernetes companion, we propose these 10 outstanding features:

## 1. `ktl debug` (The Ultimate Troubleshooter)
**Concept**: A wrapper around `kubectl debug` that automatically injects a "Swiss Army Knife" sidecar (netshoot, curl, vim, htop) into crashing pods.
**Why**: Debugging `CrashLoopBackOff` is painful because you can't `exec`. Ephemeral containers are the answer, but the syntax is verbose. `ktl debug <pod>` should just work.

## 2. `ktl tunnel` (Smart Port Forwarding)
**Concept**: Auto-detect ports from Service/Pod specs and forward them. Support multi-service forwarding (e.g., `ktl tunnel app db redis`).
**Why**: Developers hate looking up port numbers. They just want to access the service. "It just works" local access.

## 3. `ktl events` (Live Cluster Pulse)
**Concept**: A real-time, deduplicated, color-coded stream of cluster events. Like `tail -f` but for the cluster state.
**Why**: `kubectl get events` is sorted weirdly and hard to read. A streaming timeline helps catch issues *as they happen*.

## 4. `ktl cost` (Namespace FinOps)
**Concept**: Estimate the monthly cost of the current namespace based on CPU/Memory requests and Cloud Provider pricing APIs (AWS/GCP/Azure).
**Why**: Developers have no visibility into the cost impact of their deployments. "You are spending $50/mo" is powerful feedback.

## 5. `ktl access` (RBAC Visualizer)
**Concept**: "Who can do what?" Visualizer. Check your own permissions (`can-i`) or audit a ServiceAccount.
**Why**: RBAC is opaque. A clear matrix of "Read/Write/Delete" permissions for the current user or a specific ServiceAccount is invaluable.

## 6. `ktl snapshot` (Manifest Export)
**Concept**: Export the current state of a namespace to clean, apply-ready YAML (stripping `status`, `managedFields`, `uid`).
**Why**: "I want to copy this env to another cluster." `kubectl get -o yaml` produces messy output that fails when re-applied.

## 7. `ktl diff` (Local vs Cluster)
**Concept**: Compare local Helm charts or manifests against the live cluster state. Show drift.
**Why**: "Did someone edit the deployment manually?" GitOps is great, but sometimes you need to check ad-hoc drift.

## 8. `ktl secret` (Safe Editor)
**Concept**: Interactive editor that automatically decodes Base64, opens `$EDITOR`, and re-encodes on save.
**Why**: `kubectl edit secret` shows Base64. Humans cannot read Base64. This removes the "decode -> paste -> edit -> encode -> paste" loop.

## 9. `ktl top` (Efficiency Dashboard)
**Concept**: TUI dashboard showing Request vs Limit vs Usage. Highlight "Waste" (high request, low usage) and "Risk" (usage near limit).
**Why**: `kubectl top` is too basic. We need to see *efficiency* to optimize resources.

## 10. `ktl invite` (Kubeconfig Generator)
**Concept**: Generate a time-bound, namespace-restricted `kubeconfig` for a colleague.
**Why**: Onboarding is hard. "Here, run this command to get access" is a killer feature for platform teams.
