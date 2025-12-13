//go:build !linux

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/example/ktl/pkg/buildkit"
)

func TestBuildCommandSandboxLogsErrorsOnUnsupportedPlatform(t *testing.T) {
	origBuildFn := buildDockerfileFn
	defer func() { buildDockerfileFn = origBuildFn }()

	var invoked bool
	buildDockerfileFn = func(_ context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
		invoked = true
		return &buildkit.BuildResult{Digest: "sha256:abc"}, nil
	}

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
	if invoked {
		t.Fatalf("build should not start when sandbox logs are unsupported")
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}
