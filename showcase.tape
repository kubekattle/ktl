Output showcase.gif

Set FontSize 32
Set Width 1800
Set Height 1200
Set Padding 60
Set FontFamily "JetBrains Mono"
Set LineHeight 1.4
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
Type "bat testdata/stack/e2e/10-large-graph/stack.yaml"
Sleep 500ms
Enter
Sleep 4s
Type "# Notice the 'needs' array? ktl calculates the dependency graph automatically."
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

# 2. Unified Logs (The "Better Stern")
Type "# 2. Unified Logs: Multi-pod tailing with zero configuration."
Sleep 1s
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

# 3. Tunnel (The "Better Port-Forward")
Type "# 3. Smart Tunnel: Access services with env injection and fault injection."
Sleep 1s
Enter
Type "# Imagine running your local app against a remote DB..."
Sleep 1s
Enter
Type "./bin/ktl tunnel service/kubernetes 6443:443 --env-from deployment/my-app"
Sleep 500ms
Enter
Sleep 5s
Ctrl+C
Sleep 1s

# Clear for next demo
Type "clear"
Enter

# 4. Build (The "Better Docker Build")
Type "# 4. Built-in BuildKit: Fast, cached builds without a local Docker daemon."
Sleep 1s
Enter
Type "./bin/ktl build testdata/build/dockerfiles/basic -t ktl.local/demo:latest"
Sleep 500ms
Enter
Sleep 10s

# Conclusion
Type "# ktl: Build, Deploy, Debug. All in one."
Sleep 2s
Enter

# Gracefully exit
Ctrl+C
Sleep 2s
