package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func marshalJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestListRuns_NewestFirst(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer s.Close()

	run1ID := "2025-01-01T00-00-00Z"
	p1 := &Plan{
		StackRoot: root,
		StackName: "demo",
		Profile:   "dev",
		Nodes: []*ResolvedRelease{
			{ID: "c1/ns/app", Name: "app", Cluster: ClusterTarget{Name: "c1"}, Namespace: "ns"},
		},
	}
	r1 := &runState{RunID: run1ID, Plan: p1, Command: "apply", Nodes: wrapRunNodes(p1.Nodes), Concurrency: 1, FailMode: "fail-fast"}
	if err := s.CreateRun(ctx, r1, p1); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	sum1 := &RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      run1ID,
		Status:     "succeeded",
		StartedAt:  "2025-01-01T00:00:00Z",
		UpdatedAt:  "2025-01-01T00:00:01Z",
		Totals:     RunTotals{Planned: 1, Succeeded: 1},
		Nodes: map[string]RunNodeSummary{
			"c1/ns/app": {Status: "succeeded", Attempt: 1, Error: ""},
		},
		Order: []string{"c1/ns/app"},
	}
	if err := s.WriteSummary(ctx, run1ID, sum1); err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ktl_stack_runs SET created_at_ns = ?, updated_at_ns = ?, summary_json = ? WHERE run_id = ?`,
		int64(1000), int64(1001), marshalJSON(t, sum1), run1ID); err != nil {
		t.Fatalf("update run1 timestamps: %v", err)
	}

	run2ID := "2025-01-02T00-00-00Z"
	p2 := &Plan{
		StackRoot: root,
		StackName: "demo",
		Profile:   "dev",
		Nodes: []*ResolvedRelease{
			{ID: "c1/ns/app", Name: "app", Cluster: ClusterTarget{Name: "c1"}, Namespace: "ns"},
			{ID: "c1/ns/db", Name: "db", Cluster: ClusterTarget{Name: "c1"}, Namespace: "ns"},
		},
	}
	r2 := &runState{RunID: run2ID, Plan: p2, Command: "apply", Nodes: wrapRunNodes(p2.Nodes), Concurrency: 1, FailMode: "fail-fast"}
	if err := s.CreateRun(ctx, r2, p2); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	sum2 := &RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      run2ID,
		Status:     "failed",
		StartedAt:  "2025-01-02T00:00:00Z",
		UpdatedAt:  "2025-01-02T00:00:01Z",
		Totals:     RunTotals{Planned: 2, Failed: 1, Succeeded: 1},
		Nodes: map[string]RunNodeSummary{
			"c1/ns/app": {Status: "failed", Attempt: 1, Error: "boom"},
			"c1/ns/db":  {Status: "succeeded", Attempt: 1, Error: ""},
		},
		Order: []string{"c1/ns/app", "c1/ns/db"},
	}
	if err := s.WriteSummary(ctx, run2ID, sum2); err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ktl_stack_runs SET created_at_ns = ?, updated_at_ns = ?, summary_json = ? WHERE run_id = ?`,
		int64(2000), int64(2001), marshalJSON(t, sum2), run2ID); err != nil {
		t.Fatalf("update run2 timestamps: %v", err)
	}

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
	ctx := context.Background()
	runID := "2025-01-02T00-00-00Z"
	s, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer s.Close()

	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Profile:   "dev",
		Nodes: []*ResolvedRelease{
			{ID: "c1/ns/app", Name: "app", Cluster: ClusterTarget{Name: "c1"}, Namespace: "ns"},
		},
	}
	r := &runState{RunID: runID, Plan: p, Command: "apply", Nodes: wrapRunNodes(p.Nodes), Concurrency: 2, FailMode: "fail-fast"}
	if err := s.CreateRun(ctx, r, p); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	sum := &RunSummary{
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
	}
	if err := s.WriteSummary(ctx, runID, sum); err != nil {
		t.Fatalf("WriteSummary: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE ktl_stack_runs SET summary_json = ? WHERE run_id = ?`, marshalJSON(t, sum), runID); err != nil {
		t.Fatalf("update summary_json: %v", err)
	}

	var buf bytes.Buffer
	err = RunStatus(t.Context(), StatusOptions{RootDir: root, RunID: runID, Format: "table"}, &buf)
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
