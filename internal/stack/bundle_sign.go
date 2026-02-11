package stack

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func SignBundle(bundlePath string, priv ed25519.PrivateKey) (*BundleSignature, error) {
	bundlePath = strings.TrimSpace(bundlePath)
	if bundlePath == "" {
		return nil, fmt.Errorf("bundle path is required")
	}
	tmp, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	manifestRaw, err := os.ReadFile(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	canon, err := canonicalizeJSON(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize manifest.json: %w", err)
	}
	sig, err := signManifest(priv, canon)
	if err != nil {
		return nil, err
	}
	if err := writeJSONAtomicFile(filepath.Join(tmp, "signature.json"), sig); err != nil {
		return nil, err
	}
	if err := repackBundle(bundlePath, tmp); err != nil {
		return nil, err
	}
	return sig, nil
}

func VerifyBundle(bundlePath string, trustedPub ed25519.PublicKey) (*BundleSignature, error) {
	bundlePath = strings.TrimSpace(bundlePath)
	if bundlePath == "" {
		return nil, fmt.Errorf("bundle path is required")
	}
	tmp, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	sigRaw, err := os.ReadFile(filepath.Join(tmp, "signature.json"))
	if err != nil {
		return nil, fmt.Errorf("read signature.json: %w", err)
	}
	var sig BundleSignature
	if err := json.Unmarshal(sigRaw, &sig); err != nil {
		return nil, fmt.Errorf("parse signature.json: %w", err)
	}
	if strings.TrimSpace(sig.Algorithm) != "" && strings.TrimSpace(sig.Algorithm) != "ed25519" {
		return nil, fmt.Errorf("unsupported signature algorithm %q", sig.Algorithm)
	}

	pubB64 := strings.TrimSpace(sig.PublicKey)
	if len(trustedPub) > 0 {
		pubB64 = base64.StdEncoding.EncodeToString(trustedPub)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return nil, fmt.Errorf("decode publicKey: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("publicKey has length %d (want %d)", len(pubBytes), ed25519.PublicKeySize)
	}
	pub := ed25519.PublicKey(pubBytes)

	manifestRaw, err := os.ReadFile(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}
	canon, err := canonicalizeJSON(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize manifest.json: %w", err)
	}
	sum := sha256.Sum256(canon)
	got := hex.EncodeToString(sum[:])
	if want := strings.TrimSpace(sig.ManifestSHA256); want != "" && want != got {
		return nil, fmt.Errorf("manifest sha256 mismatch (want %s got %s)", want, got)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.Signature))
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(pub, sum[:], sigBytes) {
		return nil, fmt.Errorf("invalid signature")
	}

	// Best-effort integrity checks described by the manifest.
	var manifest map[string]any
	if err := json.Unmarshal(canon, &manifest); err == nil {
		if v, ok := manifest["stateSha256"].(string); ok && strings.TrimSpace(v) != "" {
			got, err := sha256File(filepath.Join(tmp, "state.sqlite"))
			if err != nil {
				return nil, fmt.Errorf("hash state.sqlite: %w", err)
			}
			if strings.TrimSpace(v) != strings.TrimSpace(got) {
				return nil, fmt.Errorf("state.sqlite sha256 mismatch (want %s got %s)", strings.TrimSpace(v), strings.TrimSpace(got))
			}
		}
		if v, ok := manifest["inputsBundleSha256"].(string); ok && strings.TrimSpace(v) != "" {
			got, err := sha256File(filepath.Join(tmp, "inputs.tar.gz"))
			if err != nil {
				return nil, fmt.Errorf("hash inputs.tar.gz: %w", err)
			}
			if strings.TrimSpace(v) != strings.TrimSpace(got) {
				return nil, fmt.Errorf("inputs.tar.gz sha256 mismatch (want %s got %s)", strings.TrimSpace(v), strings.TrimSpace(got))
			}
		}
	}
	return &sig, nil
}

func repackBundle(dstPath string, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []tarFile
	for _, e := range entries {
		if e == nil || e.IsDir() {
			continue
		}
		name := strings.TrimSpace(e.Name())
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		files = append(files, tarFile{Name: name, Path: filepath.Join(dir, name), Mode: 0o644})
	}
	if len(files) == 0 {
		return fmt.Errorf("no files to pack from %s", dir)
	}
	return writeDeterministicTarGz(dstPath, files)
}
