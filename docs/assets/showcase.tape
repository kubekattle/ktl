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

# 1. Stack Apply (The "Better Helmfile")
Type "# 1. Stack Apply: A DAG-aware, parallel release orchestrator."
Sleep 1s
Enter
Type "# It manages complex dependencies so you don't have to write bash scripts."
Sleep 2s
Enter
Type "./bin/ktl stack apply --config testdata/stack/e2e/09-from-path-selection
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
Type "./bin/ktl stack apply --config testdata/stack/e2e/10-large-graph"
Sleep 500ms
Enter
Sleep 70s
Type "./bin/ktl stack --config testdata/stack/e2e/10-large-graph runs"
Sleep 500ms
Enter
Sleep 2s
Type "./bin/ktl stack apply --config testdata/stack/e2e/10-large-graph --resume --run-id 2026-02-09T11-14-46.491775000Z"
Sleep 500ms
Enter
Sleep 5s

# Clear for next demo
Type "clear"
Enter

# 3. Helm Apply with Logs
Type "# 3. Helm Apply + Logs: See what's happening immediately."
Sleep 1s
Enter
Type "# Standard helm upgrade is silent. ktl apply streams logs instantly."
Sleep 2s
Enter
Type "./bin/ktl apply --chart testdata/charts/promtail/ --release my-base -n default --yes --watch 10s"
Sleep 500ms
Enter
Sleep 15s

# Clear for next demo
Type "clear"
Enter

# 4. Unified Logs
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

# Gracefully exit
Ctrl+C
Sleep 2s
