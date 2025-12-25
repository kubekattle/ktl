package stack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listRunsLegacy(root string, limit int) ([]RunIndexEntry, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	base := filepath.Join(root, ".ktl", "stack", "runs")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}

	var runIDs []string
	for _, e := range entries {
		if e.IsDir() {
			runIDs = append(runIDs, e.Name())
		}
	}
	sort.Strings(runIDs)
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
