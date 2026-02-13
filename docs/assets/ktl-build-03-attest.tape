Output "ktl-build-03-attest.gif"
Set FontSize 18
Set Width 1600
Set Height 900
Set Theme "Dracula"
Set TypingSpeed 35ms

Type "cd /root/ktl-showcase/work"
Enter
Type "clear"
Enter
Type "echo '# Tape 3: SBOM + provenance attestations'"
Enter
Type "rm -rf /root/ktl-showcase/work/out-attest-vhs-tape"
Enter
Type "KTL_SANDBOX_DISABLE=1 /root/ktl-showcase/bin/ktl build testdata/build/dockerfiles/basic -t local/ktl-attest-demo:latest --output logs --oci --attest-dir /root/ktl-showcase/work/out-attest-vhs-tape --sbom --provenance"
Enter
Sleep 75s
Type "ls -lh /root/ktl-showcase/work/out-attest-vhs-tape"
Enter
Sleep 3s
Type "sed -n '1,25p' /root/ktl-showcase/work/out-attest-vhs-tape/*provenance*.json"
Enter
Sleep 4s
Type "echo 'Attestation showcase done.'"
Enter
Sleep 2s
