Output showcase.gif

Set FontSize 22
Set Width 1600
Set Height 1000
Set Padding 40
Set FontFamily "JetBrains Mono"
Set LineHeight 1.2
Set Theme "TokyoNight"

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
