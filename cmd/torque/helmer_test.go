package main

import (
	"strings"
	"testing"
)

func TestNewRootCommandHelmerMode(t *testing.T) {
	prev := buildMode
	buildMode = buildModeHelmerOnly
	defer func() { buildMode = prev }()

	root := newRootCommand()
	if got := root.Name(); got != "helmer" {
		t.Fatalf("root.Name()=%q, want helmer", got)
	}
	for _, name := range []string{"plan", "report", "archive", "verify-archive", "unpack"} {
		if cmd, _, err := root.Find([]string{name}); err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("expected helmer subcommand %q, err=%v", name, err)
		}
	}
}

func TestBuildInstallCommandUsesCurrentToolProfile(t *testing.T) {
	prev := buildMode
	defer func() { buildMode = prev }()

	buildMode = buildModeFull
	torqueCmd := buildInstallCommand(deployPlanOptions{
		Chart:     "./chart",
		Release:   "api",
		Namespace: "prod",
	})
	if !strings.HasPrefix(torqueCmd, "torque apply plan ") {
		t.Fatalf("torque install command=%q", torqueCmd)
	}

	buildMode = buildModeHelmerOnly
	helmerCmd := buildInstallCommand(deployPlanOptions{
		Chart:     "./chart",
		Release:   "api",
		Namespace: "prod",
	})
	if !strings.HasPrefix(helmerCmd, "helmer plan ") {
		t.Fatalf("helmer install command=%q", helmerCmd)
	}
}
