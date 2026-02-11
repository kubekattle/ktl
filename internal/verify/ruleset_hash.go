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

func RulesetDigest(dir string) (string, error) { return RulesetDigestMulti([]string{dir}) }

func RulesetDigestMulti(dirs []string) (string, error) {
	dirs = dedupeStrings(dirs)
	if len(dirs) == 0 {
		return "", nil
	}
	var files []string
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
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
	}
	if len(files) == 0 {
		return "", fmt.Errorf("empty ruleset digest")
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		if _, err := io.WriteString(h, path+"\n"); err != nil {
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
