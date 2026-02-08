# 🚀 ktl Roadmap: The Path to "Best-in-Class"

To make `ktl` the irreplaceable Kubernetes companion, we propose these 10 outstanding features:

## 1. `ktl debug` (The Ultimate Troubleshooter)
**Concept**: A wrapper around `kubectl debug` that automatically injects a "Swiss Army Knife" sidecar (netshoot, curl, vim, htop) into crashing pods.
**Why**: Debugging `CrashLoopBackOff` is painful because you can't `exec`. Ephemeral containers are the answer, but the syntax is verbose. `ktl debug <pod>` should just work.

## 2. `ktl tunnel` 2.0 (The Connectivity Hub)
*Implemented v1 with multi-tunneling & auto-fix.*
**Next Steps (The "Perfect 10" Improvements):**
1.  **Profiles**: `ktl tunnel save backend` / `ktl tunnel load backend` to restore complex sets of tunnels.
2.  **Traffic Stats**: Live TUI columns for TX/RX bytes and Request/sec.
3.  **Local DNS**: Map `http://my-app.local` to the tunnel (bypassing localhost port chaos).
4.  **Reverse Tunnel**: Expose a local port *to* the cluster (great for testing webhooks).
5.  **Smart Connect**: Detect Redis/Postgres and offer to launch `redis-cli` or `psql` connected to the tunnel.
6.  **Auto-Open**: `--open` flag to launch the default browser when the tunnel is ready.
7.  **LAN Share**: `--share` (bind 0.0.0.0) to let colleagues on the WiFi access your tunnel.
8.  **Multi-Cluster**: Tunnel to `prod/db` and `dev/app` simultaneously (mixed contexts).
9.  **Dependency Walking**: "Tunnel to X and all its dependencies" (using `ktl stack` graph).
10. **Hooks**: Run a local script (e.g., `npm start`) only after tunnels are healthy.

## 3. `ktl cost` (Namespace FinOps)
**Concept**: Estimate the monthly cost of the current namespace based on CPU/Memory requests and Cloud Provider pricing APIs (AWS/GCP/Azure).
**Why**: Developers have no visibility into the cost impact of their deployments. "You are spending $50/mo" is powerful feedback.

## 4. `ktl access` (RBAC Visualizer)
**Concept**: "Who can do what?" Visualizer. Check your own permissions (`can-i`) or audit a ServiceAccount.
**Why**: RBAC is opaque. A clear matrix of "Read/Write/Delete" permissions for the current user or a specific ServiceAccount is invaluable.

## 5. `ktl snapshot` (Manifest Export)
**Concept**: Export the current state of a namespace to clean, apply-ready YAML (stripping `status`, `managedFields`, `uid`).
**Why**: "I want to copy this env to another cluster." `kubectl get -o yaml` produces messy output that fails when re-applied.

## 6. `ktl diff` (Local vs Cluster)
**Concept**: Compare local Helm charts or manifests against the live cluster state. Show drift.
**Why**: "Did someone edit the deployment manually?" GitOps is great, but sometimes you need to check ad-hoc drift.

## 7. `ktl secret` (Safe Editor)
**Concept**: Interactive editor that automatically decodes Base64, opens `$EDITOR`, and re-encodes on save.
**Why**: `kubectl edit secret` shows Base64. Humans cannot read Base64. This removes the "decode -> paste -> edit -> encode -> paste" loop.

## 8. `ktl top` (Efficiency Dashboard)
**Concept**: TUI dashboard showing Request vs Limit vs Usage. Highlight "Waste" (high request, low usage) and "Risk" (usage near limit).
**Why**: `kubectl top` is too basic. We need to see *efficiency* to optimize resources.

## 9. `ktl invite` (Kubeconfig Generator)
**Concept**: Generate a time-bound, namespace-restricted `kubeconfig` for a colleague.
**Why**: Onboarding is hard. "Here, run this command to get access" is a killer feature for platform teams.
