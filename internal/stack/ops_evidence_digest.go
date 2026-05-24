package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ComputeOpsFactEvidenceDigest(source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("fact evidence source is required")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		raw, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		return opsApplyPreflightDigest(raw), nil
	}

	h := sha256.New()
	found := false
	for _, name := range []string{"manifest.json", "facts.json", "freshness.json"} {
		path := filepath.Join(source, name)
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		found = true
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(raw)
		_, _ = h.Write([]byte{0})
	}
	if !found {
		return "", fmt.Errorf("fact evidence directory has no manifest.json, facts.json, or freshness.json")
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
