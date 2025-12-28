package stack

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

type countingExecutor struct {
	called int
}

func (e *countingExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	e.called++
	return nil
}

func TestRun_ResumeSeedsSucceededFromSteps(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	chartDir := filepath.Join(root, "chart")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "x.yaml"), []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	run1ID := "2026-01-01T00-00-00Z"
	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Profile:   "dev",
		Nodes: []*ResolvedRelease{
			{
				ID:        "c1/ns/app",
				Name:      "app",
				Chart:     chartDir,
				Cluster:   ClusterTarget{Name: "c1"},
				Namespace: "ns",
				Apply:     ApplyOptions{Wait: ptrBool(true)},
			},
		},
	}
	r1 := &runState{RunID: run1ID, Plan: p, Command: "apply", Nodes: wrapRunNodes(p.Nodes), Concurrency: 1, FailMode: "fail-fast"}
	if err := store.CreateRun(ctx, r1, p); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	// Simulate a run that completed all apply phases, but never wrote NODE_SUCCEEDED.
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO ktl_stack_node_steps (
  run_id, node_id, attempt, step,
  started_at_ns, completed_at_ns, status, message
) VALUES
  (?, ?, 1, 'upgrade', 1, 2, 'succeeded', 'ok'),
  (?, ?, 1, 'wait', 3, 4, 'succeeded', 'ok'),
  (?, ?, 1, 'post-hooks', 5, 6, 'succeeded', 'ok')
	`, run1ID, "c1/ns/app", run1ID, "c1/ns/app", run1ID, "c1/ns/app"); err != nil {
		t.Fatalf("insert node steps: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE ktl_stack_nodes SET attempt = 1 WHERE run_id = ? AND node_id = ?`, run1ID, "c1/ns/app"); err != nil {
		t.Fatalf("update attempt: %v", err)
	}
	_ = store.Close()

	loaded, err := LoadRun(root, run1ID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	stepsByID, err := LoadRunNodeSteps(root, run1ID)
	if err != nil {
		t.Fatalf("LoadRunNodeSteps: %v", err)
	}

	exec := &countingExecutor{}
	var out bytes.Buffer
	var errOut bytes.Buffer
	err = Run(ctx, RunOptions{
		Command:           "apply",
		Plan:              loaded.Plan,
		Concurrency:       1,
		Executor:          exec,
		Lock:              false,
		ResumeStatusByID:  loaded.StatusByID,
		ResumeAttemptByID: loaded.AttemptByID,
		ResumeStepsByID:   stepsByID,
		ResumeFromRunID:   run1ID,
	}, &out, &errOut)
	if err != nil {
		t.Fatalf("Run: %v (stderr=%q)", err, errOut.String())
	}
	if exec.called != 0 {
		t.Fatalf("expected executor to be skipped, called=%d", exec.called)
	}
}

func ptrBool(v bool) *bool { return &v }
