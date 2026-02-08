Output showcase-build.gif

Set FontSize 32
Set Width 1800
Set Height 1200
Set Padding 60
Set FontFamily "JetBrains Mono"
Set LineHeight 1.4
Set Theme "Dracula"

# Setup
Hide
Type "ssh lab"
Enter
Type "cd ~/ktl-showcase"
Enter
Type "clear"
Enter
Show

# 1. Standard BuildKit Build
Type "# 1. Building a container image with embedded BuildKit"
Sleep 1s
Enter
Type "./bin/ktl build testdata/build/dockerfiles/basic -t ktl.local/basic:dev"
Sleep 500ms
Enter
Sleep 10s

# Clear for next demo
Type "clear"
Enter

# 2. Sandboxed Build (Linux)
Type "# 2. Secure Sandboxed Build (nsjail isolation)"
Sleep 1s
Enter
Type "export KTL_SANDBOX_CONFIG=$(pwd)/sandbox/linux-strict.cfg"
Sleep 500ms
Enter
Type "./bin/ktl build testdata/build/dockerfiles/sandbox-strict --no-cache -t ktl.local/sandbox:dev"
Sleep 500ms
Enter
Sleep 15s

# Gracefully exit
Ctrl+C
Sleep 1s
Type "exit"
Enter
Sleep 2s
