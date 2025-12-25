package stack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ListRuns lists recent runs. It prefers the sqlite state store when present, and
// falls back to legacy per-run directories for backwards compatibility.
func ListRuns(root string, limit int) ([]RunIndexEntry, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err == nil {
		s, err := openStackStateStore(root, true)
		if err != nil {
			return nil, err
		}
		defer s.Close()
		return s.ListRuns(context.Background(), limit)
	}

	// Legacy fallback: scan .ktl/stack/runs/*/summary.json
	entries, err := listRunsLegacy(root, limit)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no runs found (expected %s or %s)", filepath.Join(root, stackStateSQLiteRelPath), filepath.Join(root, ".ktl", "stack", "runs"))
	}
	return entries, nil
}
