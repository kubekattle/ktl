package stack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ListRuns lists recent runs. It prefers the sqlite state store when present, and
// requires it (legacy on-disk runs have been removed).
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
	return nil, fmt.Errorf("no runs found (expected %s)", filepath.Join(root, stackStateSQLiteRelPath))
}
