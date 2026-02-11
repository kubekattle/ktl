//go:build !linux

// File: cmd/ktl/build_sandbox_logs_stub_test.go
// Brief: CLI command wiring and implementation for 'build sandbox logs stub'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildCommandSandboxLogsErrorsOnUnsupportedPlatform(t *testing.T) {
	cmd := newBuildCommand()
	cmd.SetArgs([]string{"--sandbox-logs", t.TempDir()})
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when requesting sandbox logs on unsupported platform")
	}
	if !strings.Contains(err.Error(), "--sandbox-logs") {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}
