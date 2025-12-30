package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/example/ktl/internal/appconfig"
)

func TestVerifyInlineManifest_Shortcut(t *testing.T) {
	repoRoot := chdirRepoRoot(t)
	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	noColor := false
	cmd := newVerifyCommand(&kubeconfig, &kubeContext, &logLevel, &noColor)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		filepath.Join(repoRoot, "testdata", "verify", "showcase", "verify.yaml"),
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success, got %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "k8s/host_namespace_isolated") {
		t.Fatalf("expected host namespace finding, got:\n%s", stdout.String())
	}
}

func chdirRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve caller")
	}
	repoRoot := appconfig.FindRepoRoot(file)
	if repoRoot == "" {
		t.Fatalf("failed to locate repo root from %s", file)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	return repoRoot
}
