package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRelPath_ExpandsHomeAndEnv(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", home)

	got := resolveRelPath(base, "~/kubeconfig.yaml")
	want := filepath.Join(home, "kubeconfig.yaml")
	if got != want {
		t.Fatalf("~ expansion mismatch: got %q, want %q", got, want)
	}

	got = resolveRelPath(base, "$HOME/kubeconfig.yaml")
	if got != want {
		t.Fatalf("$HOME expansion mismatch: got %q, want %q", got, want)
	}
}
