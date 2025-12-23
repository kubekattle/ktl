package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/example/ktl/internal/workflows/buildsvc"
	"github.com/example/ktl/pkg/buildkit"
)

type noopBuildkitRunner struct{}

func (noopBuildkitRunner) BuildDockerfile(ctx context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
	_ = ctx
	_ = opts
	return &buildkit.BuildResult{Digest: "sha256:deadbeef"}, nil
}

type captureBuildkitRunner struct {
	last buildkit.DockerfileBuildOptions
}

func (r *captureBuildkitRunner) BuildDockerfile(ctx context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
	_ = ctx
	r.last = opts
	return &buildkit.BuildResult{Digest: "sha256:deadbeef"}, nil
}

func TestBuildInteractiveRequiresTTY(t *testing.T) {
	disableSandboxForTests(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nRUN echo ok\n")
	authFile := filepath.Join(dir, "config.json")
	writeFile(t, authFile, "{}\n")

	profile := "dev"
	svc := buildsvc.New(buildsvc.Dependencies{BuildRunner: noopBuildkitRunner{}})
	logLevel := "info"
	cmd := newBuildCommandWithService(svc, &profile, &logLevel)
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--authfile", authFile, "--interactive", dir})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got == "" || !bytes.Contains([]byte(got), []byte("--interactive requires a TTY")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildInteractiveDefaultsShellAndPropagates(t *testing.T) {
	disableSandboxForTests(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nRUN echo ok\n")
	authFile := filepath.Join(dir, "config.json")
	writeFile(t, authFile, "{}\n")

	runner := &captureBuildkitRunner{}
	profile := "dev"
	svc := buildsvc.New(buildsvc.Dependencies{BuildRunner: runner})
	logLevel := "info"
	cmd := newBuildCommandWithService(svc, &profile, &logLevel)
	cmd.SetIn(newFakeTTY())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--authfile", authFile, "--interactive", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if runner.last.Interactive == nil {
		t.Fatalf("expected Interactive config to be set")
	}
	if got := runner.last.Interactive.Shell; len(got) != 1 || got[0] != "/bin/sh" {
		t.Fatalf("unexpected default interactive shell: %#v", got)
	}
}
