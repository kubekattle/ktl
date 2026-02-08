Output showcase.gif

Set FontSize 15
Set Width 1400
Set Height 900
Set Padding 30
Set FontFamily "JetBrains Mono"
Set Theme "Catppuccin Mocha"

# Setup environment
Hide
Type "export KUBECONFIG=~/.kube/config"
Enter
Type "clear"
Enter
Show

# Type the command (using --config to avoid deprecation warning)
Type "./bin/ktl stack apply --config testdata/stack/e2e/10-large-graph"
Sleep 500ms
Enter

# Allow the tool to run and show its TUI/Progress
Sleep 20s

# Gracefully exit
Ctrl+C
Sleep 2s
