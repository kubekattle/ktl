package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildProfileCiDefaultsToPush(t *testing.T) {
	disableSandboxForTests(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	rec := &recordingBuildService{}
	root := newRootCommandWithBuildService(rec)
	root.SetIn(newFakeTTY())
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--profile", "ci", "build", t.TempDir()})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !rec.lastOpts.Push {
		t.Fatalf("expected ci profile to default to push")
	}
}

func TestBuildProfileDoesNotOverrideExplicitPushFlag(t *testing.T) {
	disableSandboxForTests(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	rec := &recordingBuildService{}
	root := newRootCommandWithBuildService(rec)
	root.SetIn(newFakeTTY())
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--profile", "ci", "build", t.TempDir(), "--push=false"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.lastOpts.Push {
		t.Fatalf("expected explicit --push=false to win over profile defaults")
	}
}

func TestBuildConfigProfileDefaultsAppliedWhenFlagUnset(t *testing.T) {
	disableSandboxForTests(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ktl"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(home, ".ktl", "config.yaml"), "build:\n  profile: ci\n")

	rec := &recordingBuildService{}
	root := newRootCommandWithBuildService(rec)
	root.SetIn(newFakeTTY())
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"build", t.TempDir()})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !rec.lastOpts.Push {
		t.Fatalf("expected config profile to default to push")
	}
}
