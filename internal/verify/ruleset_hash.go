package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func RulesetDigest(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".rego") || strings.HasSuffix(name, ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel := filepath.ToSlash(strings.TrimPrefix(path, dir))
		rel = strings.TrimPrefix(rel, "/")
		if _, err := io.WriteString(h, rel+"\n"); err != nil {
			return "", err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if _, err := h.Write(raw); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\n"); err != nil {
			return "", err
		}
	}
	sum := h.Sum(nil)
	if len(sum) == 0 {
		return "", fmt.Errorf("empty ruleset digest")
	}
	return hex.EncodeToString(sum), nil
}
