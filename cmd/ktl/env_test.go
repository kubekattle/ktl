package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvCommandPrintsCatalogAndValues(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)
	t.Setenv("KTL_SANDBOX_DISABLE", "1")
	t.Setenv("NO_COLOR", "1")

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"env"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got: %q", errOut.String())
	}

	got := out.String()
	for _, want := range []string{"CATEGORY", "VARIABLE", "VALUE", "DESCRIPTION"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("expected header to mention %q, got:\n%s", want, got)
		}
	}
	for _, want := range []string{"KTL_CONFIG", "KTL_SANDBOX_DISABLE", "NO_COLOR", "KTL_FEATURE_<FLAG>"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	if !bytes.Contains([]byte(got), []byte("KTL_SANDBOX_DISABLE")) || !bytes.Contains([]byte(got), []byte(" 1 ")) {
		t.Fatalf("expected KTL_SANDBOX_DISABLE value to be shown, got:\n%s", got)
	}
}

func TestEnvCommandHidesInternalByDefault(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)
	t.Setenv("KTL_SANDBOX_ACTIVE", "1")
	t.Setenv("KTL_NSJAIL_ACTIVE", "1")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"env"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if bytes.Contains([]byte(got), []byte("KTL_SANDBOX_ACTIVE")) || bytes.Contains([]byte(got), []byte("KTL_NSJAIL_ACTIVE")) {
		t.Fatalf("expected internal variables to be hidden by default, got:\n%s", got)
	}
}

func TestEnvCommandShowsInternalWithAll(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)
	t.Setenv("KTL_SANDBOX_ACTIVE", "1")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"env", "--all"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte("KTL_SANDBOX_ACTIVE")) {
		t.Fatalf("expected internal variables to be shown with --all, got:\n%s", got)
	}
}

func TestEnvCommandOnlySetAndFiltering(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)
	t.Setenv("KTL_BUILDKIT_HOST", "unix:///tmp/buildkit.sock")

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"env", "--set", "--category", "build", "--match", "buildkit"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte("KTL_BUILDKIT_HOST")) {
		t.Fatalf("expected KTL_BUILDKIT_HOST to be included, got:\n%s", got)
	}
	if bytes.Contains([]byte(got), []byte("KTL_CONFIG")) {
		t.Fatalf("expected non-build variables to be filtered out, got:\n%s", got)
	}
}

func TestEnvCommandJSONFormat(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"env", "--format", "json", "--set"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if !bytes.Contains([]byte(got), []byte(`"variable": "KTL_CONFIG"`)) {
		t.Fatalf("expected JSON output to include KTL_CONFIG, got:\n%s", got)
	}
}
