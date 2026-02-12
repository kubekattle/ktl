package main

import (
	"path/filepath"
	"testing"

	"github.com/kubekattle/ktl/internal/appconfig"
)

func TestParseSandboxPolicyDefaultConfig(t *testing.T) {
	wd := t.TempDir()
	_ = wd
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	repoRoot := appconfig.FindRepoRoot(cwd)
	if repoRoot == "" {
		t.Fatalf("repo root not found from %s", cwd)
	}
	cfg := filepath.Join(repoRoot, "internal", "workflows", "buildsvc", "sandbox", "ktl-default.cfg")
	summary, err := parseSandboxPolicy(cfg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if summary.NetworkMode != "host" {
		t.Fatalf("expected host network mode, got %q", summary.NetworkMode)
	}
	if summary.RlimitAS != "8388608" || summary.RlimitCPU != "900" || summary.RlimitFsize != "1073741824" {
		t.Fatalf("unexpected limits: %#v", summary)
	}
	if summary.BindMountCount == 0 {
		t.Fatalf("expected bind mounts, got %#v", summary)
	}
	foundTmp := false
	for _, m := range summary.TmpfsMounts {
		if m == "/tmp (size=2G)" {
			foundTmp = true
		}
	}
	if !foundTmp {
		t.Fatalf("expected /tmp tmpfs with size, got %#v", summary.TmpfsMounts)
	}
}
