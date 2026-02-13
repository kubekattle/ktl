Output "ktl-build-02-cache.gif"
Set FontSize 18
Set Width 1600
Set Height 900
Set Theme "Dracula"
Set TypingSpeed 35ms

Type "cd /root/ktl-showcase/work"
Enter
Type "clear"
Enter
Type "echo '# Tape 2: Cache intelligence (cold vs warm)'"
Enter
Type "KTL_SANDBOX_DISABLE=1 /root/ktl-showcase/bin/ktl build testdata/build/dockerfiles/metadata -t local/ktl-cache-demo:latest --output logs --cache-intel --cache-intel-top 5"
Enter
Sleep 55s
Type "echo '# Re-running the same build to highlight cache hits'"
Enter
Type "KTL_SANDBOX_DISABLE=1 /root/ktl-showcase/bin/ktl build testdata/build/dockerfiles/metadata -t local/ktl-cache-demo:latest --output logs --cache-intel --cache-intel-top 5"
Enter
Sleep 45s
Type "echo 'Cache showcase done.'"
Enter
Sleep 2s
