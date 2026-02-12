package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubekattle/ktl/internal/stack"
)

func TestStackPlan_RejectsGitIncludeDepsWithoutGitRange(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := writeStackWithInvalidSelectorDefaults(t)
	t.Setenv("KTL_STACK_ROOT", root)

	cmd := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"stack", "plan"})

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected error, got success (stdout=%q stderr=%q)", out.String(), errOut.String())
	}
	msg := strings.ToLower(err.Error() + " " + errOut.String())
	if !strings.Contains(msg, "gitincludedeps") || !strings.Contains(msg, "requires") || !strings.Contains(msg, "gitrange") {
		t.Fatalf("expected selector validation error, got: %v (stderr=%q)", err, errOut.String())
	}
}

func TestStackPlan_WarnsWhenEnvGitRangeOverridesStackYAML(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := writeGitRangedStack(t)
	t.Setenv("KTL_STACK_ROOT", root)
	t.Setenv("KTL_STACK_GIT_RANGE", "HEAD~1..HEAD")

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
		t.Fatalf("expected only release a from env git-range override, got: %#v", p.Nodes)
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "both stack.yaml cli.selector.gitrange and ktl_stack_git_range are set") {
		t.Fatalf("expected override warning, got stderr=%q", errOut.String())
	}
}

func writeStackWithInvalidSelectorDefaults(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeDemoChart(t, filepath.Join(root, "chart"))
	stackYAML := `
name: demo
cli:
  output: json
  inferDeps: false
  selector:
    gitIncludeDeps: true
releases:
  - name: a
    chart: ./chart
    cluster: {name: c1}
    namespace: default
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}
	return root
}

func writeGitRangedStack(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeDemoChart(t, filepath.Join(root, "chart"))

	if err := run(t, root, "git", "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_ = run(t, root, "git", "config", "user.email", "ktl@example.invalid")
	_ = run(t, root, "git", "config", "user.name", "ktl")

	stackYAML := `
name: demo
cli:
  output: json
  inferDeps: false
  selector:
    gitRange: "stack.yaml-range"
releases:
  - name: a
    chart: ./chart
    cluster: {name: c1}
    namespace: default
    values: ["a/values.yaml"]
  - name: b
    chart: ./chart
    cluster: {name: c1}
    namespace: default
    values: ["b/values.yaml"]
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "values.yaml"), []byte("fixture: a\n"), 0o644); err != nil {
		t.Fatalf("write values-a.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b", "values.yaml"), []byte("fixture: b\n"), 0o644); err != nil {
		t.Fatalf("write values-b.yaml: %v", err)
	}

	if err := run(t, root, "git", "add", "."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := run(t, root, "git", "commit", "-m", "init"); err != nil {
		t.Fatalf("git commit init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "a", "values.yaml"), []byte("fixture: a2\n"), 0o644); err != nil {
		t.Fatalf("rewrite values-a.yaml: %v", err)
	}
	if err := run(t, root, "git", "add", "a/values.yaml"); err != nil {
		t.Fatalf("git add values-a.yaml: %v", err)
	}
	if err := run(t, root, "git", "commit", "-m", "touch a"); err != nil {
		t.Fatalf("git commit touch: %v", err)
	}
	return root
}

func writeDemoChart(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "x.yaml"), []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
}

func run(t *testing.T, dir string, name string, args ...string) error {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &execError{err: err, out: string(out)}
	}
	return nil
}

type execError struct {
	err error
	out string
}

func (e *execError) Error() string { return e.err.Error() + ": " + e.out }
func (e *execError) Unwrap() error { return e.err }
