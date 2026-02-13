Output "ktl-build-01-sandbox.gif"
Set FontSize 18
Set Width 1600
Set Height 900
Set Theme "Dracula"
Set TypingSpeed 35ms

Type "cd /root/ktl-showcase/work"
Enter
Type "clear"
Enter
Type "echo '# Tape 1: Sandbox doctor + sandboxed ktl build'"
Enter
Type "/root/ktl-showcase/bin/ktl build sandbox doctor --sandbox-bin /usr/local/bin/nsjail-oldmnt --sandbox-config sandbox/linux-ci.cfg"
Enter
Sleep 8s
Type "/root/ktl-showcase/bin/ktl build testdata/build/dockerfiles/basic -t local/ktl-sandbox-demo:latest --sandbox --sandbox-bin /usr/local/bin/nsjail-oldmnt --sandbox-config sandbox/linux-ci.cfg --sandbox-bind-home --sandbox-logs --output logs"
Enter
Sleep 45s
Type "echo 'Sandbox showcase done.'"
Enter
Sleep 2s
