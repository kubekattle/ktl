package stack

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	writeFile(t, path, string(raw))
}

func TestListRuns_NewestFirst(t *testing.T) {
	root := t.TempDir()
	runsDir := filepath.Join(root, ".ktl", "stack", "runs")

	writeJSON(t, filepath.Join(runsDir, "2025-01-01T00-00-00Z", "summary.json"), RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      "2025-01-01T00-00-00Z",
		Status:     "succeeded",
		UpdatedAt:  "2025-01-01T00:00:01Z",
		Totals:     RunTotals{Planned: 1, Succeeded: 1},
	})
	writeJSON(t, filepath.Join(runsDir, "2025-01-02T00-00-00Z", "summary.json"), RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      "2025-01-02T00-00-00Z",
		Status:     "failed",
		UpdatedAt:  "2025-01-02T00:00:01Z",
		Totals:     RunTotals{Planned: 2, Failed: 1},
	})

	got, err := ListRuns(root, 10)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("runs=%d", len(got))
	}
	if got[0].RunID != "2025-01-02T00-00-00Z" {
		t.Fatalf("order[0]=%s", got[0].RunID)
	}
	if got[1].RunID != "2025-01-01T00-00-00Z" {
		t.Fatalf("order[1]=%s", got[1].RunID)
	}
}

func TestRunStatus_FormatTable(t *testing.T) {
	root := t.TempDir()
	runID := "2025-01-02T00-00-00Z"
	runRoot := filepath.Join(root, ".ktl", "stack", "runs", runID)

	writeJSON(t, filepath.Join(runRoot, "plan.json"), RunPlan{
		APIVersion:  "ktl.dev/stack-run/v1",
		RunID:       runID,
		StackRoot:   root,
		StackName:   "demo",
		Command:     "apply",
		Profile:     "dev",
		Concurrency: 2,
		FailMode:    "fail-fast",
		Nodes: []*ResolvedRelease{
			{ID: "c1/ns/app", Name: "app"},
		},
	})
	writeJSON(t, filepath.Join(runRoot, "summary.json"), RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      runID,
		Status:     "failed",
		StartedAt:  "2025-01-02T00:00:00Z",
		UpdatedAt:  "2025-01-02T00:00:10Z",
		Totals:     RunTotals{Planned: 1, Failed: 1},
		Nodes: map[string]RunNodeSummary{
			"c1/ns/app": {Status: "failed", Attempt: 2, Error: "boom"},
		},
		Order: []string{"c1/ns/app"},
	})

	var buf bytes.Buffer
	err := RunStatus(t.Context(), StatusOptions{RootDir: root, RunID: runID, Format: "table"}, &buf)
	if err != nil {
		t.Fatalf("RunStatus: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "STATUS") || !strings.Contains(out, "ATTEMPT") || !strings.Contains(out, "ERROR") {
		t.Fatalf("missing header:\n%s", out)
	}
	if !strings.Contains(out, "c1/ns/app") || !strings.Contains(out, "FAILED") || !strings.Contains(out, "boom") {
		t.Fatalf("missing row:\n%s", out)
	}
}
