// File: cmd/ktl/main_test.go
// Brief: Main ktl CLI entrypoint and root command wiring.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

	err := root.ExecuteContext(context.Background())
	if err != nil && !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("execute: %v", err)
	}
	if got := errOut.String(); !bytes.Contains([]byte(got), []byte(`unknown command "desfs"`)) {
		t.Fatalf("expected unknown command message in stderr, got: %q", got)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("Usage:")) {
		t.Fatalf("expected help output in stdout, got: %q", got)
	}
}

func TestApplyShowsHelpWhenLogLevelValueLooksLikeHelpFlag(t *testing.T) {
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
	root.SetArgs([]string{
		"apply",
		"--chart", "testdata/charts/tempo",
		"--release", "monitoring",
		"--log-level", "-h",
	})

	err := root.ExecuteContext(context.Background())
	if err != nil && !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("expected help response, got %v", err)
	}
	if got := out.String(); !bytes.Contains([]byte(got), []byte("Usage:")) {
		t.Fatalf("expected help output in stdout, got: %q", got)
	}
	if got := errOut.String(); bytes.Contains([]byte(got), []byte("Only 'yes' will be accepted")) {
		t.Fatalf("expected no confirmation prompt, got stderr: %q", got)
	}
}

func TestApplyShowsHelpOnMissingLogLevelValue(t *testing.T) {
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
	root.SetArgs([]string{
		"apply",
		"--chart", "testdata/charts/tempo",
		"--release", "monitoring",
		"--log-level",
	})

	err := root.ExecuteContext(context.Background())
	if err != nil && !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("expected help response, got %v", err)
	}
	if got := errOut.String(); !bytes.Contains([]byte(got), []byte("flag needs an argument: --log-level")) {
		t.Fatalf("expected missing-arg error in stderr, got: %q", got)
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

func TestRootHasRevertCommand(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var revertCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "revert" {
			revertCmd = cmd
			break
		}
	}
	if revertCmd == nil {
		t.Fatalf("expected root to include revert command")
	}
	if f := revertCmd.Flags().Lookup("release"); f == nil {
		t.Fatalf("expected revert to have --release flag")
	}
}

func TestRootHelpSubcommandOrder(t *testing.T) {
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
	root.SetArgs([]string{"--help"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	help := out.String()
	wantOrder := []string{
		"\nSubcommands:\n",
		"  init",
		"  build",
		"  apply",
		"  delete",
		"  revert",
		"  list",
		"  lint",
		"  logs",
		"  env",
	}
	last := -1
	for _, needle := range wantOrder {
		idx := strings.Index(help, needle)
		if idx == -1 {
			t.Fatalf("expected help to contain %q, got:\n%s", needle, help)
		}
		if idx < last {
			t.Fatalf("expected %q to appear after previous entries, got help:\n%s", needle, help)
		}
		last = idx
	}
	if strings.Contains(errOut.String(), "Error:") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestListCommandHasLsAlias(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var listCmdAliases []string
	for _, cmd := range root.Commands() {
		if cmd.Name() == "list" {
			listCmdAliases = cmd.Aliases
			break
		}
	}
	if listCmdAliases == nil {
		t.Fatalf("expected root to include list command")
	}
	if !slices.Contains(listCmdAliases, "ls") {
		t.Fatalf("expected list aliases to include ls, got: %v", listCmdAliases)
	}
}

func TestApplyHasPlanSubcommand(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var applyCmd *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "apply" {
			applyCmd = cmd
			break
		}
	}
	if applyCmd == nil {
		t.Fatalf("expected root to include apply command")
	}
	if _, _, err := applyCmd.Find([]string{"plan"}); err != nil {
		t.Fatalf("expected apply to include plan subcommand: %v", err)
	}
}

func TestRootIncludesLintCommand(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	for _, cmd := range root.Commands() {
		if cmd.Name() == "lint" {
			return
		}
	}
	t.Fatalf("expected root to include lint command")
}

func TestRootIncludesPackageCommand(t *testing.T) {
}
