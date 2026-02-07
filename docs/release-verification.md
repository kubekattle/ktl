# Release Verification

This project publishes release assets with:

- `*.sha256` files and `checksums-*.txt` (SHA256)
- `*.sigstore.json` (cosign keyless bundles)
- GitHub artifact attestations (SLSA provenance + SBOM)

## Verify Checksums

Download a release asset plus its corresponding checksums file:

- Per-platform tarballs + SBOMs: `checksums-<os>-<arch>-<tag>.txt`
- Linux packages: `checksums-linux-packages-<tag>.txt`

Then run:

```bash
# Linux
sha256sum -c checksums-*.txt

# macOS
shasum -a 256 -c checksums-*.txt
```

## Verify Cosign Bundles

Each signed asset has a matching bundle: `<asset>.sigstore.json`.

```bash
cosign verify-blob \
  --bundle <asset>.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/<OWNER>/<REPO>/.github/workflows/release\\.yml@refs/tags/<TAG>$' \
  <asset>
```

## Verify GitHub Attestations

Provenance attestation verification:

```bash
gh attestation verify <asset> \
  --repo <OWNER>/<REPO> \
  --signer-workflow <OWNER>/<REPO>/.github/workflows/release.yml
```

Notes:

- `gh attestation verify` defaults to the SLSA provenance predicate type; for SBOM attestations, pass the appropriate `--predicate-type`.
- Tighten verification further with `--cert-identity-regex`, `--source-ref`, and `--signer-digest` if you want to lock to a specific tag/ref and workflow digest.
