package stack

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestComputeEffectiveInputHash_ChartChangesAffectHash(t *testing.T) {
	root := t.TempDir()

	chartDir := filepath.Join(root, "chart")
	writeFile(t, filepath.Join(chartDir, "Chart.yaml"), `
apiVersion: v2
name: demo
version: 0.1.0
`)
	writeFile(t, filepath.Join(chartDir, "templates", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
data:
  k: v1
`)

	valuesPath := filepath.Join(root, "values.yaml")
	writeFile(t, valuesPath, "replicaCount: 1\n")

	u, err := Discover(root)
	if err == nil {
		t.Fatalf("expected missing stack.yaml error")
	}
	_ = u

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

	u, err = Discover(root)
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

	hash1, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	// Mutate the chart content in-place.
	writeFile(t, filepath.Join(chartDir, "templates", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
data:
  k: v2
`)

	hash2, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if hash1 == hash2 {
		t.Fatalf("expected different hashes after chart change, got %s", hash1)
	}
}

func TestComputeEffectiveInputHash_OptionsAffectHash(t *testing.T) {
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
`)
	writeFile(t, filepath.Join(root, "chart", "Chart.yaml"), `
apiVersion: v2
name: demo
version: 0.1.0
`)

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	n := p.Nodes[0]

	hash1, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	timeout := 12 * time.Minute
	n.Apply.Timeout = &timeout
	hash2, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash2: %v", err)
	}
	if hash1 == hash2 {
		t.Fatalf("expected different hashes after apply timeout change, got %s", hash1)
	}

	// Restore and change delete timeout instead.
	n.Apply.Timeout = nil
	hash3, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash3: %v", err)
	}
	if hash3 != hash1 {
		t.Fatalf("expected hash to return to original after reverting apply timeout (%s != %s)", hash3, hash1)
	}

	dt := 2 * time.Minute
	n.Delete.Timeout = &dt
	hash4, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash4: %v", err)
	}
	if hash4 == hash1 {
		t.Fatalf("expected different hashes after delete timeout change, got %s", hash4)
	}

	// Ensure values-only changes are detected too.
	valuesPath := filepath.Join(root, "values.yaml")
	if err := os.WriteFile(valuesPath, []byte("foo: bar\n"), 0o644); err != nil {
		t.Fatalf("write values: %v", err)
	}
	n.Values = []string{valuesPath}
	hash5, _, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash5: %v", err)
	}
	if hash5 == hash4 {
		t.Fatalf("expected different hashes after adding values, got %s", hash5)
	}
}
