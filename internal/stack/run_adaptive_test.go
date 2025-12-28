package stack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptedExecutor struct{}

func (scriptedExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	// Force a deterministic RATE_LIMIT failure on the 3rd node.
	if strings.HasSuffix(node.ID, "/c") && node.Attempt == 1 {
		return errors.New("429 too many requests")
	}
	return nil
}

func TestRun_AdaptiveConcurrency_EmitsEvents(t *testing.T) {
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
			{ID: "c/ns/a", Name: "a", Dir: root, Chart: chartDir, Namespace: "ns", Cluster: ClusterTarget{Name: "c"}},
			{ID: "c/ns/b", Name: "b", Dir: root, Chart: chartDir, Namespace: "ns", Cluster: ClusterTarget{Name: "c"}},
			{ID: "c/ns/c", Name: "c", Dir: root, Chart: chartDir, Namespace: "ns", Cluster: ClusterTarget{Name: "c"}},
		},
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range p.Nodes {
		p.ByID[n.ID] = n
		p.ByCluster[n.Cluster.Name] = append(p.ByCluster[n.Cluster.Name], n)
	}
	if err := RecomputeExecutionGroups(p); err != nil {
		t.Fatalf("assign groups: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := Run(ctx, RunOptions{
		Command:                "apply",
		Plan:                   p,
		Concurrency:            4,
		ProgressiveConcurrency: true,
		FailFast:               false,
		MaxAttempts:            1,
		Executor:               scriptedExecutor{},
		Adaptive: &AdaptiveConcurrencyOptions{
			Min:                1,
			WindowSize:         10,
			RampAfterSuccesses: 2,
			RampMaxFailureRate: 0.50,
		},
	}, ioDiscard{}, ioDiscard{})
	if err == nil {
		t.Fatalf("expected run to return an error")
	}

	runID, rerr := LoadMostRecentRun(root)
	if rerr != nil {
		t.Fatalf("load most recent run: %v", rerr)
	}
	store, serr := openStackStateStore(root, false)
	if serr != nil {
		t.Fatalf("open store: %v", serr)
	}
	defer store.Close()
	events, eerr := store.ListEvents(context.Background(), runID, 0)
	if eerr != nil {
		t.Fatalf("list events: %v", eerr)
	}
	var sawRamp bool
	var sawShrink bool
	for _, ev := range events {
		if ev.Type != "RUN_CONCURRENCY" {
			continue
		}
		if strings.Contains(ev.Message, "reason=ramp-up") {
			sawRamp = true
		}
		if strings.Contains(ev.Message, "reason=RATE_LIMIT") {
			sawShrink = true
			continue
		}
		if fields, ok := ev.Fields.(map[string]any); ok {
			if v, ok := fields["class"].(string); ok && v == "RATE_LIMIT" {
				if r, ok := fields["reason"].(string); ok && r == "RATE_LIMIT" {
					sawShrink = true
				}
			}
		}
	}
	if !sawRamp {
		t.Fatalf("expected a ramp-up RUN_CONCURRENCY event")
	}
	if !sawShrink {
		t.Fatalf("expected a shrink RUN_CONCURRENCY event for RATE_LIMIT")
	}
}
