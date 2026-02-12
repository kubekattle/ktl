package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubekattle/ktl/internal/stack"
)

func TestStackPlan_UsesStackYAMLCliDefaults(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := writeStackWithCliDefaults(t)
	t.Setenv("KTL_STACK_ROOT", root)

	cmd := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"stack", "plan"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v (stderr=%q)", err, errOut.String())
	}

	var p stack.Plan
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatalf("expected json plan output, parse err: %v (stdout=%q)", err, out.String())
	}
	if len(p.Nodes) != 1 || p.Nodes[0].Name != "a" {
		t.Fatalf("expected only release a from stack.yaml cli.selector.tags, got: %#v", p.Nodes)
	}
}

func TestStackPlan_EnvOverridesStackYAMLSelectors(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := writeStackWithCliDefaults(t)
	t.Setenv("KTL_STACK_ROOT", root)
	t.Setenv("KTL_STACK_TAG", "team-b")

	cmd := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"stack", "plan"})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v (stderr=%q)", err, errOut.String())
	}

	var p stack.Plan
	if err := json.Unmarshal(out.Bytes(), &p); err != nil {
		t.Fatalf("expected json plan output, parse err: %v (stdout=%q)", err, out.String())
	}
	if len(p.Nodes) != 1 || p.Nodes[0].Name != "b" {
		t.Fatalf("expected only release b from KTL_STACK_TAG override, got: %#v", p.Nodes)
	}
}

func writeStackWithCliDefaults(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
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
	stackYAML := `
name: demo
cli:
  output: json
  inferDeps: false
  selector:
    tags: ["team-a"]
releases:
  - name: a
    chart: ./chart
    tags: ["team-a"]
    cluster: {name: c1}
    namespace: default
  - name: b
    chart: ./chart
    tags: ["team-b"]
    cluster: {name: c1}
    namespace: default
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}
	return root
}
