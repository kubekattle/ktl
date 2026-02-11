// File: cmd/ktl/lint_test.go
// Brief: Tests for the 'lint' command UX and defaults.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintCommandErrorsWhenNoChartFound(t *testing.T) {
	tmp := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var kubeconfig, kubeContext string
	cmd := newLintCommand(&kubeconfig, &kubeContext)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(nil)

	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "missing Chart.yaml") {
		t.Fatalf("expected missing Chart.yaml error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ktl lint ./path/to/chart") {
		t.Fatalf("expected usage hint in error, got: %v", err)
	}
}

func TestLintCommandDefaultsToChartSubdir(t *testing.T) {
	tmp := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	writeFile(t, filepath.Join(tmp, "chart", "Chart.yaml"), "apiVersion: v2\nname: example\nversion: 0.1.0\n")
	writeFile(t, filepath.Join(tmp, "chart", "values.yaml"), "{}\n")
	if err := os.MkdirAll(filepath.Join(tmp, "chart", "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	var kubeconfig, kubeContext string
	cmd := newLintCommand(&kubeconfig, &kubeContext)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "==> Linting ./chart") {
		t.Fatalf("expected lint to run on ./chart, got stdout:\n%s", stdout.String())
	}
}
