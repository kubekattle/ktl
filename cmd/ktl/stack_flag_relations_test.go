package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStackApplyMutuallyExclusiveSealedAndBundle(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	rootDir := writeMinimalStackRoot(t)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{
		"stack", "apply",
		"--config", rootDir,
		"--infer-deps=false",
		"--plan-only",
		"--sealed-dir", "x",
		"--from-bundle", "y",
	})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := strings.ToLower(err.Error() + " " + errOut.String())
	if !strings.Contains(msg, "flags in the group") || !strings.Contains(msg, "sealed-dir") || !strings.Contains(msg, "from-bundle") {
		t.Fatalf("expected mutually-exclusive error, got: %v (stderr=%q)", err, errOut.String())
	}
}

func TestStackApplyMutuallyExclusiveFailFastAndContinue(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	rootDir := writeMinimalStackRoot(t)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{
		"stack", "apply",
		"--config", rootDir,
		"--infer-deps=false",
		"--plan-only",
		"--fail-fast=true",
		"--continue-on-error=true",
	})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := strings.ToLower(err.Error() + " " + errOut.String())
	if !strings.Contains(msg, "flags in the group") || !strings.Contains(msg, "fail-fast") || !strings.Contains(msg, "continue-on-error") {
		t.Fatalf("expected mutually-exclusive error, got: %v (stderr=%q)", err, errOut.String())
	}
}

func TestStackDeleteRejectsDryRunFlag(t *testing.T) {
	root := newRootCommand()
	cmd, _, err := root.Find([]string{"stack", "delete"})
	if err != nil {
		t.Fatalf("find command: %v", err)
	}
	if cmd.Flags().Lookup("dry-run") != nil {
		t.Fatalf("expected ktl stack delete to not define --dry-run")
	}
}

func TestStackConfigFlagExistsAndRootIsDeprecated(t *testing.T) {
	root := newRootCommand()
	cmd, _, err := root.Find([]string{"stack"})
	if err != nil {
		t.Fatalf("find command: %v", err)
	}
	if cmd.PersistentFlags().Lookup("config") == nil {
		t.Fatalf("expected ktl stack to define --config")
	}
	rootFlag := cmd.PersistentFlags().Lookup("root")
	if rootFlag == nil {
		t.Fatalf("expected ktl stack to still accept --root")
	}
	if rootFlag.Deprecated == "" {
		t.Fatalf("expected --root to be deprecated")
	}
}

func writeMinimalStackRoot(t *testing.T) string {
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
releases:
  - name: r1
    chart: ./chart
    cluster:
      name: c1
    namespace: default
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(strings.TrimSpace(stackYAML)+"\n"), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}
	return root
}
