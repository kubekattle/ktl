package stack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VerifyBundleIntegrity checks the hashes in manifest.json (if present) without requiring a signature.
func VerifyBundleIntegrity(bundlePath string) error {
	bundlePath = strings.TrimSpace(bundlePath)
	if bundlePath == "" {
		return fmt.Errorf("bundle path is required")
	}
	tmp, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	manifestRaw, err := os.ReadFile(filepath.Join(tmp, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read manifest.json: %w", err)
	}
	canon, err := canonicalizeJSON(manifestRaw)
	if err != nil {
		return fmt.Errorf("canonicalize manifest.json: %w", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(canon, &manifest); err != nil {
		return fmt.Errorf("parse manifest.json: %w", err)
	}

	if v, ok := manifest["stateSha256"].(string); ok && strings.TrimSpace(v) != "" {
		got, err := sha256File(filepath.Join(tmp, "state.sqlite"))
		if err != nil {
			return fmt.Errorf("hash state.sqlite: %w", err)
		}
		if strings.TrimSpace(v) != strings.TrimSpace(got) {
			return fmt.Errorf("state.sqlite sha256 mismatch (want %s got %s)", strings.TrimSpace(v), strings.TrimSpace(got))
		}
	}
	if v, ok := manifest["inputsBundleSha256"].(string); ok && strings.TrimSpace(v) != "" {
		got, err := sha256File(filepath.Join(tmp, "inputs.tar.gz"))
		if err != nil {
			return fmt.Errorf("hash inputs.tar.gz: %w", err)
		}
		if strings.TrimSpace(v) != strings.TrimSpace(got) {
			return fmt.Errorf("inputs.tar.gz sha256 mismatch (want %s got %s)", strings.TrimSpace(v), strings.TrimSpace(got))
		}
	}
	return nil
}
