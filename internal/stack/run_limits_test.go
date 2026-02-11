package stack

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type blockingExecutor struct {
	mu         sync.Mutex
	running    int
	maxRunning int
	block      chan struct{}
}

func (e *blockingExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	e.mu.Lock()
	e.running++
	if e.running > e.maxRunning {
		e.maxRunning = e.running
	}
	e.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-e.block:
	}
	e.mu.Lock()
	e.running--
	e.mu.Unlock()
	return nil
}

func TestRun_RespectsMaxParallelKind(t *testing.T) {
	root := t.TempDir()
	chartDir := filepath.Join(root, "chart")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: x\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "cm.yaml"), []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\ndata:\n  a: b\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	p := &Plan{
		StackRoot: root,
		StackName: "test",
		Nodes: []*ResolvedRelease{
			{ID: "c/ns/a", Name: "a", Dir: root, Chart: chartDir, Namespace: "ns", Cluster: ClusterTarget{Name: "c"}, InferredPrimaryKind: "Deployment"},
			{ID: "c/ns/b", Name: "b", Dir: root, Chart: chartDir, Namespace: "ns", Cluster: ClusterTarget{Name: "c"}, InferredPrimaryKind: "Deployment"},
		},
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range p.Nodes {
		p.ByID[n.ID] = n
		p.ByCluster[n.Cluster.Name] = append(p.ByCluster[n.Cluster.Name], n)
	}

	exec := &blockingExecutor{block: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{
			Command:              "apply",
			Plan:                 p,
			Concurrency:          2,
			Executor:             exec,
			MaxConcurrencyByKind: map[string]int{"Deployment": 1},
		}, ioDiscard{}, ioDiscard{})
	}()

	time.Sleep(200 * time.Millisecond)
	close(exec.block)
	if err := <-done; err != nil {
		t.Fatalf("run failed: %v", err)
	}
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if exec.maxRunning > 1 {
		t.Fatalf("expected maxRunning<=1, got %d", exec.maxRunning)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
