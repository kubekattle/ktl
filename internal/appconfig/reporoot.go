package appconfig

import (
	"os"
	"path/filepath"
	"strings"
)

func FindRepoRoot(start string) string {
	start = strings.TrimSpace(start)
	if start == "" {
		return ""
	}
	info, err := os.Stat(start)
	if err == nil && !info.IsDir() {
		start = filepath.Dir(start)
	}
	current := start
	for {
		if isRepoRoot(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func isRepoRoot(dir string) bool {
	if dir == "" {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi.IsDir() {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, ".ktl.yaml")); err == nil && !fi.IsDir() {
		return true
	}
	return false
}
