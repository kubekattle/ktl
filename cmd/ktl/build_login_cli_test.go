// File: cmd/ktl/build_login_cli_test.go
// Brief: CLI parse-time validation tests for 'build login/logout'.

package main

import (
	"bytes"
	"testing"
)

func TestBuildLoginRejectsInvalidServerArgAtParseTime(t *testing.T) {
	cmd := newBuildCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(newFakeTTY())
	cmd.SetArgs([]string{"login", "bad host"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildLoginRejectsEmptyUsernameFlagAtParseTime(t *testing.T) {
	cmd := newBuildCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(newFakeTTY())
	cmd.SetArgs([]string{"login", "ghcr.io", "--username", ""})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildLogoutRejectsInvalidServerArgAtParseTime(t *testing.T) {
	cmd := newBuildCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(newFakeTTY())
	cmd.SetArgs([]string{"logout", "http://"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error")
	}
}
