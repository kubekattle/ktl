package stack

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSealedPlan_ArgValidation(t *testing.T) {
	t.Parallel()

	_, _, err := LoadSealedPlan(context.Background(), LoadSealedPlanOptions{
		SealedDir:  "x",
		BundlePath: "y",
	})
	if err == nil {
		t.Fatalf("expected error for both sealedDir and bundlePath set")
	}

	_, _, err = LoadSealedPlan(context.Background(), LoadSealedPlanOptions{})
	if err == nil {
		t.Fatalf("expected error for neither sealedDir nor bundlePath set")
	}
}

func TestLoadSealedPlan_FromSealedDir_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sealedDir := t.TempDir()

	chartDir := t.TempDir()
	valuesDir := t.TempDir()

	chartYAML := []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n")
	valuesYAML := []byte("replicas: 1\n")

	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), chartYAML, 0o644); err != nil {
		t.Fatalf("write chart.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "deployment.yaml"), []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	valuesPath := filepath.Join(valuesDir, "values.yaml")
	if err := os.WriteFile(valuesPath, valuesYAML, 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}

	node := &ResolvedRelease{
		ID:        "demo",
		Name:      "demo",
		Dir:       ".",
		Cluster:   ClusterTarget{Name: "c1"},
		Namespace: "ns",
		Chart:     chartDir,
		Values:    []string{valuesPath},
		Set:       map[string]string{},
	}

	gid := &GitIdentity{Commit: "deadbeef", Dirty: true}
	wantHash, _, err := ComputeEffectiveInputHashWithOptions(node, EffectiveInputHashOptions{
		StackGitIdentity:      gid,
		IncludeValuesContents: true,
	})
	if err != nil {
		t.Fatalf("compute effective input hash: %v", err)
	}
	node.EffectiveInputHash = wantHash

	rp := &RunPlan{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      "",
		StackRoot:  ".",
		StackName:  "demo-stack",
		Command:    "apply",
		Profile:    "",
		FailMode:   "fail-fast",
		Nodes:      []*ResolvedRelease{node},
		Runner: RunnerResolved{
			Concurrency:            1,
			ProgressiveConcurrency: false,
			Limits: RunnerLimitsResolved{
				ParallelismGroupLimit: 1,
			},
			Adaptive: RunnerAdaptiveResolved{
				Min:                1,
				Window:             20,
				RampAfterSuccesses: 2,
				RampMaxFailureRate: 0.3,
				CooldownSevere:     4,
			},
		},
		StackGitCommit: gid.Commit,
		StackGitDirty:  gid.Dirty,
	}

	planHash, err := ComputeRunPlanHash(rp)
	if err != nil {
		t.Fatalf("compute run plan hash: %v", err)
	}
	rp.PlanHash = planHash

	rawPlan, err := json.MarshalIndent(rp, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sealedDir, "plan.json"), rawPlan, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	manifest := &InputBundleManifest{
		APIVersion: "ktl.dev/stack-input-bundle/v1",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		PlanHash:   planHash,
		Nodes: []InputBundleNode{
			{
				ID:       "demo",
				ChartDir: "nodes/demo/chart",
				Values: []InputBundleValue{
					{
						OriginalPath: "values.yaml",
						BundlePath:   "nodes/demo/values/00_values.yaml",
					},
				},
			},
		},
	}
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	var bundle bytes.Buffer
	gw := gzip.NewWriter(&bundle)
	tw := tar.NewWriter(gw)
	writeTarFile := func(name string, data []byte) {
		t.Helper()
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(data)),
			ModTime: time.Unix(0, 0).UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body %s: %v", name, err)
		}
	}

	writeTarFile("manifest.json", rawManifest)
	writeTarFile("nodes/demo/chart/Chart.yaml", chartYAML)
	writeTarFile("nodes/demo/chart/templates/deployment.yaml", []byte("# empty\n"))
	writeTarFile("nodes/demo/values/00_values.yaml", valuesYAML)

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sealedDir, "inputs.tar.gz"), bundle.Bytes(), 0o644); err != nil {
		t.Fatalf("write inputs.tar.gz: %v", err)
	}

	p, cleanup, err := LoadSealedPlan(ctx, LoadSealedPlanOptions{
		StateStoreRoot: "state-root",
		SealedDir:      sealedDir,
	})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("LoadSealedPlan: %v", err)
	}
	if p.StackRoot != "state-root" {
		t.Fatalf("StackRoot: want %q got %q", "state-root", p.StackRoot)
	}
	if len(p.Nodes) != 1 || p.Nodes[0].ID != "demo" {
		t.Fatalf("nodes: unexpected: %#v", p.Nodes)
	}
}
