package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildSandboxDoctorCommandWired(t *testing.T) {
	root := newRootCommand()
	var build *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "build" {
			build = cmd
			break
		}
	}
	if build == nil || build.Name() != "build" {
		t.Fatalf("expected build command")
	}
	var sandbox *cobra.Command
	for _, cmd := range build.Commands() {
		if cmd.Name() == "sandbox" {
			sandbox = cmd
			break
		}
	}
	if sandbox == nil {
		t.Fatalf("expected build sandbox command")
	}
	for _, cmd := range sandbox.Commands() {
		if cmd.Name() == "doctor" {
			return
		}
	}
	t.Fatalf("expected build sandbox doctor command")
}
