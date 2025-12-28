package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStackHelpListsSubcommands(t *testing.T) {
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
	root.SetArgs([]string{"stack", "--help"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	help := out.String()
	for _, needle := range []string{
		"\nSubcommands:\n",
		"  plan",
		"  graph",
		"  explain",
		"  runs",
		"  status",
		"  audit",
		"  export",
		"  keygen",
		"  sign",
		"  verify",
		"  apply",
		"  delete",
		"  rerun-failed",
		"\nGlobal Flags:\n",
	} {
		if !strings.Contains(help, needle) {
			t.Fatalf("expected help to contain %q, got:\n%s", needle, help)
		}
	}

	if strings.Contains(errOut.String(), "Error:") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}
