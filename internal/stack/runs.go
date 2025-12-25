// File: internal/stack/runs.go
// Brief: Listing recent stack runs from on-disk artifacts.

package stack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type RunIndexEntry struct {
	RunID      string    `json:"runId"`
	RunRoot    string    `json:"runRoot"`
	StackName  string    `json:"stackName,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Status     string    `json:"status,omitempty"`
	StartedAt  string    `json:"startedAt,omitempty"`
	UpdatedAt  string    `json:"updatedAt,omitempty"`
	Totals     RunTotals `json:"totals,omitempty"`
	HasSummary bool      `json:"hasSummary"`
}

func ListRuns(root string, limit int) ([]RunIndexEntry, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	base := filepath.Join(root, ".ktl", "stack", "runs")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no runs found under %s", base)
		}
		return nil, err
	}

	var runIDs []string
	for _, e := range entries {
		if e.IsDir() {
			runIDs = append(runIDs, e.Name())
		}
	}
	sort.Strings(runIDs)
	// Newest first.
	for i, j := 0, len(runIDs)-1; i < j; i, j = i+1, j-1 {
		runIDs[i], runIDs[j] = runIDs[j], runIDs[i]
	}
	if limit > 0 && len(runIDs) > limit {
		runIDs = runIDs[:limit]
	}

	out := make([]RunIndexEntry, 0, len(runIDs))
	for _, id := range runIDs {
		runRoot := filepath.Join(base, id)
		summaryPath := filepath.Join(runRoot, "summary.json")
		raw, err := os.ReadFile(summaryPath)
		if err != nil {
			out = append(out, RunIndexEntry{RunID: id, RunRoot: runRoot, HasSummary: false})
			continue
		}
		var s RunSummary
		if err := json.Unmarshal(raw, &s); err != nil {
			out = append(out, RunIndexEntry{RunID: id, RunRoot: runRoot, HasSummary: false})
			continue
		}
		out = append(out, RunIndexEntry{
			RunID:      id,
			RunRoot:    runRoot,
			Status:     s.Status,
			StartedAt:  s.StartedAt,
			UpdatedAt:  s.UpdatedAt,
			Totals:     s.Totals,
			HasSummary: true,
		})
	}
	return out, nil
}
