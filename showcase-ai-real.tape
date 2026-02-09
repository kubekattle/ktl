Output showcase-ai-real.gif

Set FontSize 32
Set Width 1800
Set Height 1200
Set Padding 60
Set FontFamily "JetBrains Mono"
Set LineHeight 1.4
Set Theme "Dracula"

# Setup
Hide
Type "export KUBECONFIG=~/.kube/config"
Enter
Type "export OPENAI_API_KEY=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"
Enter
Type "clear"
Enter
Show

# 1. Show the broken pod
Type "# 1. We have a pod in CrashLoopBackOff..."
Sleep 1s
Enter
Type "kubectl get pod payment-processor"
Sleep 500ms
Enter
Sleep 3s

# 2. Run AI Analysis
Type "# 2. Analyzing with AI (OpenAI GPT-4)"
Sleep 1s
Enter
Type "./bin/ktl analyze pod/payment-processor --ai --provider openai"
Sleep 500ms
Enter

# Allow time for AI to think and stream output
Sleep 15s

# 3. View Source Code
Type "# Notice the 'Source Code' section above matching our local file"
Sleep 2s
Enter
Type "cat testdata/analyze/crash/app.py"
Sleep 500ms
Enter
Sleep 5s

# Gracefully exit
Ctrl+C
Sleep 2s
