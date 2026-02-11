package stack

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDriftReport_Explainable(t *testing.T) {
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
	valuesPath := filepath.Join(root, "values.yaml")
	writeFile(t, valuesPath, "foo: bar\n")

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	n := p.Nodes[0]

	hash, input, err := ComputeEffectiveInputHash(root, n, true)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	n.EffectiveInputHash = hash
	n.EffectiveInput = input

	// Introduce drift by changing a values file and a chart template.
	writeFile(t, valuesPath, "foo: baz\n")
	writeFile(t, filepath.Join(root, "chart", "templates", "cm.yaml"), `
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
data:
  k: v2
`)

	lines, err := DriftReport(p)
	if err != nil {
		t.Fatalf("DriftReport: %v", err)
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "inputs changed") {
		t.Fatalf("expected drift header, got:\n%s", out)
	}
	if !strings.Contains(out, "chart digest:") {
		t.Fatalf("expected chart digest diff, got:\n%s", out)
	}
	if !strings.Contains(out, "values ") {
		t.Fatalf("expected values digest diff, got:\n%s", out)
	}
}
