package stack

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestInputBundle_RoundTripPreservesEffectiveInputHash(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: ktl.dev/v1
kind: Stack
name: demo
defaults:
  cluster: { name: c1 }
  namespace: ns1
releases:
  - name: app
    chart: ./chart
    values: [values.yaml]
`)
	writeFile(t, filepath.Join(root, "chart", "Chart.yaml"), `
apiVersion: v2
name: demo
version: 0.1.0
`)
	writeFile(t, filepath.Join(root, "chart", "templates", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
data:
  k: v1
`)
	writeFile(t, filepath.Join(root, "values.yaml"), "foo: bar\n")

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(p.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(p.Nodes))
	}
	n := p.Nodes[0]
	wantHash, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	n.EffectiveInputHash = wantHash

	bundlePath := filepath.Join(t.TempDir(), "inputs.tar.gz")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manifest, _, err := WriteInputBundle(ctx, bundlePath, "", p.Nodes)
	if err != nil {
		t.Fatalf("WriteInputBundle: %v", err)
	}

	extractDir := t.TempDir()
	manifest2, err := ExtractInputBundle(ctx, bundlePath, extractDir)
	if err != nil {
		t.Fatalf("ExtractInputBundle: %v", err)
	}
	if len(manifest2.Nodes) != len(manifest.Nodes) {
		t.Fatalf("nodes=%d want=%d", len(manifest2.Nodes), len(manifest.Nodes))
	}

	pp := &Plan{
		StackRoot: root,
		StackName: p.StackName,
		Profile:   p.Profile,
		Nodes:     []*ResolvedRelease{n},
		ByID:      map[string]*ResolvedRelease{n.ID: n},
		ByCluster: map[string][]*ResolvedRelease{n.Cluster.Name: []*ResolvedRelease{n}},
	}
	if err := ApplyInputBundleToPlan(pp, extractDir, manifest2); err != nil {
		t.Fatalf("ApplyInputBundleToPlan: %v", err)
	}

	gotHash, _, err := ComputeEffectiveInputHashWithOptions(n, EffectiveInputHashOptions{
		StackRoot:             extractDir,
		IncludeValuesContents: true,
		StackGitIdentity:      &GitIdentity{},
	})
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if gotHash != wantHash {
		t.Fatalf("hash mismatch after bundling (%s != %s)", gotHash, wantHash)
	}
}
