package stack

import (
	"context"
	"testing"
	"time"
)

func TestStackStateStore_AppendEvent_WritesNodeSteps(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	s, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer s.Close()

	runID := "2026-01-01T00-00-00Z"
	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Profile:   "dev",
		Nodes: []*ResolvedRelease{
			{ID: "c1/ns/app", Name: "app", Cluster: ClusterTarget{Name: "c1"}, Namespace: "ns"},
		},
	}
	r := &runState{RunID: runID, Plan: p, Command: "apply", Nodes: wrapRunNodes(p.Nodes), Concurrency: 1, FailMode: "fail-fast"}
	if err := s.CreateRun(ctx, r, p); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	evStart := RunEvent{
		Seq:     1,
		TS:      t0.Format(time.RFC3339Nano),
		RunID:   runID,
		NodeID:  "c1/ns/app",
		Type:    string(PhaseStarted),
		Attempt: 1,
		Message: "upgrade",
		Fields:  map[string]any{"phase": "upgrade"},
	}
	if err := s.AppendEvent(ctx, runID, evStart); err != nil {
		t.Fatalf("AppendEvent start: %v", err)
	}

	evDone := RunEvent{
		Seq:     2,
		TS:      t0.Add(150 * time.Millisecond).Format(time.RFC3339Nano),
		RunID:   runID,
		NodeID:  "c1/ns/app",
		Type:    string(PhaseCompleted),
		Attempt: 1,
		Message: "upgrade success",
		Fields:  map[string]any{"phase": "upgrade", "status": "success", "message": "ok"},
	}
	if err := s.AppendEvent(ctx, runID, evDone); err != nil {
		t.Fatalf("AppendEvent done: %v", err)
	}

	var gotStep, gotStatus string
	var startedAtNS, completedAtNS int64
	if err := s.db.QueryRowContext(ctx, `
SELECT step, status, started_at_ns, completed_at_ns
FROM ktl_stack_node_steps
WHERE run_id = ? AND node_id = ? AND attempt = ? AND step = ?
`, runID, "c1/ns/app", 1, "upgrade").Scan(&gotStep, &gotStatus, &startedAtNS, &completedAtNS); err != nil {
		t.Fatalf("query step: %v", err)
	}
	if gotStep != "upgrade" {
		t.Fatalf("step=%q", gotStep)
	}
	if gotStatus != "success" {
		t.Fatalf("status=%q", gotStatus)
	}
	if startedAtNS == 0 || completedAtNS == 0 || completedAtNS <= startedAtNS {
		t.Fatalf("bad timestamps started=%d completed=%d", startedAtNS, completedAtNS)
	}
}
