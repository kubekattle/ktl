package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVerifyShortcut_Manifest(t *testing.T) {
	repoRoot := chdirRepoRoot(t)
	cmd := newRootCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--manifest", filepath.Join(repoRoot, "testdata", "verify", "showcase", "resources.yaml"),
		"--mode", "warn",
		"--format", "table",
		"--report", "-",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify shortcut manifest: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "k8s/container_is_privileged") {
		t.Fatalf("expected a builtin finding, got:\n%s", stdout.String())
	}
}

func TestVerifyShortcut_Chart_ClientOnly(t *testing.T) {
	repoRoot := chdirRepoRoot(t)
	tmp := t.TempDir()

	// Helm action config init requires a kubeconfig file, but ClientOnly rendering should not reach the cluster.
	kcfg := filepath.Join(tmp, "kubeconfig.yaml")
	if err := os.WriteFile(kcfg, []byte(minimalKubeconfigYAML()), 0o644); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	kubeContext := "ktl"
	cmd := newRootCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--chart", filepath.Join(repoRoot, "testdata", "charts", "verify-smoke"),
		"--release", "demo",
		"-n", "default",
		"--kubeconfig", kcfg,
		"--context", kubeContext,
		"--mode", "warn",
		"--format", "table",
		"--report", "-",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify shortcut chart: %v\nstderr:\n%s", err, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "k8s/pod_or_container_without_security_context") && !strings.Contains(got, "k8s/memory_limits_not_defined") {
		t.Fatalf("expected workload findings, got:\n%s", got)
	}
}

func minimalKubeconfigYAML() string {
	// A minimal kubeconfig that is syntactically valid. Nothing should connect in ClientOnly mode.
	return strings.TrimSpace(`
apiVersion: v1
kind: Config
clusters:
  - name: ktl
    cluster:
      server: https://127.0.0.1:1
contexts:
  - name: ktl
    context:
      cluster: ktl
      user: ktl
current-context: ktl
users:
  - name: ktl
    user:
      token: dummy
`) + "\n"
}

func TestVerifyShortcut_ConfigAndFlagsReject(t *testing.T) {
	repoRoot := chdirRepoRoot(t)
	cmd := newRootCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		filepath.Join(repoRoot, "testdata", "verify", "showcase", "verify.yaml"),
		"--manifest", filepath.Join(repoRoot, "testdata", "verify", "showcase", "resources.yaml"),
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when mixing config path + shortcut flags")
	}
}

func TestMinimalKubeconfigYAML_IsDeterministic(t *testing.T) {
	// Guardrail: if this helper changes, chart shortcut tests may start flaking.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve caller")
	}
	_ = file
	if !strings.Contains(minimalKubeconfigYAML(), "current-context: ktl") {
		t.Fatalf("unexpected kubeconfig template")
	}
}
