// File: cmd/ktl/main_test.go
// Brief: Main ktl CLI entrypoint and root command wiring.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRootShowsHelpOnUnknownPlainWord(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"desfs"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := errOut.String(); !bytes.Contains([]byte(got), []byte(`unknown command "desfs"`)) {
		t.Fatalf("expected unknown command message in stderr, got: %q", got)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("Usage:")) {
		t.Fatalf("expected help output in stdout, got: %q", got)
	}
}

func TestDeleteCommandHasDestroyAlias(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var deleteCmdAliases []string
	for _, cmd := range root.Commands() {
		if cmd.Name() == "delete" {
			deleteCmdAliases = cmd.Aliases
			break
		}
	}
	if deleteCmdAliases == nil {
		t.Fatalf("expected root to include delete command")
	}
	if !slices.Contains(deleteCmdAliases, "destroy") {
		t.Fatalf("expected delete aliases to include destroy, got: %v", deleteCmdAliases)
	}
}
