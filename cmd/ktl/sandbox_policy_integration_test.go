//go:build integration && linux

// File: cmd/ktl/sandbox_policy_integration_test.go
// Brief: CLI command wiring and implementation for 'sandbox policy integration'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxBlocksUnboundHostPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	requireCommand(t, "nsjail")

	configPath, err := ensureDefaultSandboxConfig()
	if err != nil {
		t.Fatalf("resolve sandbox config: %v", err)
	}

	proofDir := t.TempDir()
	proofFile := filepath.Join(proofDir, "secret.txt")
	secret := "sandbox-secret"
	if err := os.WriteFile(proofFile, []byte(secret), 0o600); err != nil {
		t.Fatalf("write proof file: %v", err)
	}

	hostOutput := runShell(t, fmt.Sprintf("cat %s", proofFile))
	if strings.TrimSpace(hostOutput) != secret {
		t.Fatalf("expected host to read proof file, got %q", hostOutput)
	}

	script := fmt.Sprintf("cat %s", proofFile)

	if _, err := runSandboxShell(t, configPath, script, nil); err == nil {
		t.Fatalf("expected sandbox to block access to %s without bind", proofFile)
	}

	output, err := runSandboxShell(t, configPath, script, []string{"--bindmount_ro", fmt.Sprintf("%s:%s", proofDir, proofDir)})
	if err != nil {
		t.Fatalf("expected sandbox to allow bound path: %v\n%s", err, output)
	}
	if strings.TrimSpace(output) != secret {
		t.Fatalf("unexpected sandbox output: %q", output)
	}
}

func runShell(t *testing.T, script string) string {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("shell command failed: %v\n%s", err, buf.String())
	}
	return buf.String()
}

func runSandboxShell(t *testing.T, configPath, script string, extra []string) (string, error) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "sandbox.log")
	args := []string{"--config", configPath, "--log", logPath, "--cwd", "/", "--quiet"}

	baseBinds := append([]string{"/bin"}, systemDirBinds...)
	for _, dir := range baseBinds {
		if pathExists(dir) {
			args = append(args, "--bindmount_ro", fmt.Sprintf("%s:%s", dir, dir))
		}
	}
	args = append(args, extra...)
	args = append(args, "--", "/bin/sh", "-c", script)

	cmd := exec.Command("nsjail", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		if data, readErr := os.ReadFile(logPath); readErr == nil && len(data) > 0 {
			buf.WriteString("\n-- sandbox log --\n")
			buf.Write(data)
		}
	}
	return buf.String(), err
}
