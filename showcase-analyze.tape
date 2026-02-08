Output showcase-analyze.gif

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
Type "clear"
Enter
Show

# 1. Analyze a crashing pod (Heuristic Mode)
Type "# 1. Analyzing a CrashLoopBackOff pod (Heuristic Mode)"
Sleep 1s
Enter
Type "./bin/ktl analyze pod/ktl-builder-05520457"
Sleep 500ms
Enter
Sleep 10s

# Clear screen for next demo
Type "clear"
Enter

# 2. AI-Powered Analysis (Mocked for demo)
Type "# 2. AI-Powered Analysis with Code Correlation"
Sleep 1s
Enter
Type "./bin/ktl analyze pod/ktl-builder-05520457 --ai --provider mock"
Sleep 500ms
Enter
Sleep 15s

# Clear screen for next demo
Type "clear"
Enter

# 3. Cluster-Wide Health Check
Type "# 3. Cluster-wide health & node diagnostics"
Sleep 1s
Enter
Type "./bin/ktl analyze --cluster"
Sleep 500ms
Enter
Sleep 10s

# Gracefully exit
Ctrl+C
Sleep 2s
