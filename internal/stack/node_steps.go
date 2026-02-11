package stack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

type NodeStepCheckpoint struct {
	Step          string
	Attempt       int
	StartedAtNS   int64
	CompletedAtNS int64
	Status        string
	Message       string
	ErrorClass    string
	ErrorMessage  string
	ErrorDigest   string
	CursorJSON    string
}

// LoadRunNodeSteps loads per-node step checkpoints for a run from the sqlite state store.
func LoadRunNodeSteps(root string, runID string) (map[string]map[int]map[string]NodeStepCheckpoint, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, nil
	}

	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err != nil {
		return nil, err
	}

	s, err := openStackStateStore(root, true)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.GetNodeSteps(context.Background(), runID)
}
