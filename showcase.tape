Output showcase.gif

Set FontSize 18
Set Theme "Dracula"

# Setup environment
Hide
Type "export KUBECONFIG=~/.kube/config"
Enter
Type "kubectl create deployment my-app --image=nginx --replicas=1 --dry-run=client -o yaml | kubectl apply -f -"
Enter
Type "kubectl wait --for=condition=available deployment/my-app --timeout=30s"
Enter
Type "clear"
Enter
Show

# Intro
Type "# Problem: K8s workflows are fragmented (kubectl, helm, stern, docker...)"
Sleep 1s
Enter
Type "# Solution: 'ktl' unifies them into a single, developer-centric binary."
Sleep 2s
Enter
Type "clear"
Enter

# 1. Stack Apply (The "Better Helmfile")
Type "# 1. Stack Apply: A DAG-aware, parallel release orchestrator."
Sleep 1s
Enter
Type "cat testdata/stack/e2e/10-large-graph/stack.yaml"
Sleep 500ms
Enter
Sleep 4s
Type "# Notice the 'needs' array? ktl calculates the dependency graph automatically."
Sleep 2s
Enter
Type "# It manages complex dependencies so you don't have to write bash scripts."
Sleep 2s
Enter
Type "./bin/ktl stack apply --config testdata/stack/e2e/10-large-graph"
Sleep 500ms
Enter
# Let it run to show parallel execution and TUI
Sleep 20s

# Clear for next demo
Type "clear"
Enter

# 2. Resuming & Selective Re-run
Type "# 2. Smart Resume: Failed? Fix it and resume where you left off."
Sleep 1s
Enter
Type "# ktl knows which nodes succeeded and which failed."
Sleep 2s
Enter
Type "./bin/ktl stack apply --config testdata/stack/e2e/10-large-graph --only e2e10-05"
Sleep 500ms
Enter
Sleep 8s

# Clear for next demo
Type "clear"
Enter

# 3. Helm Apply with Logs (The "Better Helm Upgrade")
Type "# 3. Helm Apply + Logs: See what's happening immediately."
Sleep 1s
Enter
Type "# Standard helm upgrade is silent. ktl apply streams logs instantly."
Sleep 2s
Enter
Type "./bin/ktl apply --chart testdata/stack/e2e/charts/base --release my-base -n default --watch 10s"
Sleep 500ms
Enter
Sleep 15s

# Clear for next demo
Type "clear"
Enter

# 4. Unified Logs (The "Better Stern")
Type "# 4. Unified Logs: Multi-pod tailing with zero configuration."
Sleep 1s
Enter
Type "# Instantly tail all pods in a deployment, statefulset, or label selector."
Sleep 2s
Enter
Type "./bin/ktl logs -n default --tail 5"
Sleep 500ms
Enter
Sleep 8s
Ctrl+C
Sleep 1s

# Clear for next demo
Type "clear"
Enter

# Conclusion
Type "# ktl: The Unified Toolchain for Kubernetes."
Sleep 2s
Enter

# Gracefully exit
Ctrl+C
Sleep 2s
