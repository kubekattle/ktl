Output showcase.gif

Set FontSize 32
Set Width 1800
Set Height 1200
Set Padding 60
Set FontFamily "SF Mono"
Set LineHeight 1.4
Set Theme "Dracula"

# Setup environment
Hide
Type "export KUBECONFIG=~/.kube/config"
Enter
Type "clear"
Enter
Show

# 1. Simple Stack Apply
Type "# 1. Applying a basic stack chain"
Sleep 1s
Enter
Type "bat testdata/stack/e2e/01-basic-chain/stack.yaml"
Sleep 500ms
Enter
Sleep 5s
Type "# Notice the 'needs' dependency..."
Sleep 2s
Enter
Type "./bin/ktl stack apply --config testdata/stack/e2e/01-basic-chain"
Sleep 500ms
Enter
Sleep 15s

# Clear screen for next demo
Type "clear"
Enter

# 2. Large Graph Apply (Parallelism)
Type "# 2. Applying a large graph with dependencies"
Sleep 1s
Enter
Type "bat testdata/stack/e2e/10-large-graph/stack.yaml"
Sleep 500ms
Enter
Sleep 5s
Type "# Releases without dependencies run in PARALLEL"
Sleep 2s
Enter
Type "./bin/ktl stack apply --config testdata/stack/e2e/10-large-graph"
Sleep 500ms
Enter
Sleep 25s

# Clear screen for next demo
Type "clear"
Enter

# 3. Log Tailing Feature
Type "# 3. High-performance log tailing (multi-pod)"
Sleep 1s
Enter
Type "./bin/ktl logs -n default --tail 5"
Sleep 500ms
Enter
Sleep 10s

# Gracefully exit
Ctrl+C
Sleep 2s
