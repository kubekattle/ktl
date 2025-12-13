package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/ktl/pkg/buildkit"
)

func TestBuildCommandFlagPropagation(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	t.Setenv("MY_SECRET", "super-secret")

	origBuildFn := buildDockerfileFn
	defer func() { buildDockerfileFn = origBuildFn }()

	var captured buildkit.DockerfileBuildOptions
	buildDockerfileFn = func(_ context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
		captured = opts
		return &buildkit.BuildResult{Digest: "sha256:123"}, nil
	}

	cmd := newBuildCommand()
	cmd.SetIn(newFakeTTY())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--build-arg", "FOO=bar",
		"--builder", "tcp://buildkitd:1234",
		"--cache-dir", cacheDir,
		"--cache-from", "type=registry,ref=cache/main,mode=max",
		"--cache-to", "type=registry,ref=cache/export,mode=max",
		"--file", "Altfile",
		"--load",
		"--platform", "linux/amd64,linux/arm64",
		"--push",
		"--secret", "MY_SECRET",
		"-i",
		"--interactive-shell", "/bin/bash -l",
		"--tag", "example.com/app:dev",
		ctxDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v", err)
	}

	if captured.ContextDir != ctxDir {
		t.Fatalf("context dir mismatch: want %s got %s", ctxDir, captured.ContextDir)
	}
	if captured.DockerfilePath != "Altfile" {
		t.Fatalf("dockerfile path mismatch: want Altfile got %s", captured.DockerfilePath)
	}
	if captured.BuilderAddr != "tcp://buildkitd:1234" {
		t.Fatalf("builder address not propagated: %#v", captured.BuilderAddr)
	}
	if captured.CacheDir != cacheDir {
		t.Fatalf("cache dir mismatch: want %s got %s", cacheDir, captured.CacheDir)
	}
	if len(captured.Platforms) != 2 || captured.Platforms[0] != "linux/amd64" || captured.Platforms[1] != "linux/arm64" {
		t.Fatalf("platforms not normalized: %#v", captured.Platforms)
	}
	if len(captured.Tags) != 1 || captured.Tags[0] != "example.com/app:dev" {
		t.Fatalf("tags not propagated: %#v", captured.Tags)
	}
	if !captured.Push {
		t.Fatalf("push flag not set")
	}
	if !captured.LoadToContainerd {
		t.Fatalf("load flag not set")
	}
	if captured.BuildArgs["FOO"] != "bar" {
		t.Fatalf("build-arg not parsed: %#v", captured.BuildArgs)
	}
	if len(captured.Secrets) != 1 || captured.Secrets[0].ID != "MY_SECRET" {
		t.Fatalf("secret not added: %#v", captured.Secrets)
	}
	if len(captured.CacheImports) != 1 || captured.CacheImports[0].Type != "registry" || captured.CacheImports[0].Attrs["ref"] != "cache/main" {
		t.Fatalf("cache-from not parsed: %#v", captured.CacheImports)
	}
	if len(captured.CacheExports) != 1 || captured.CacheExports[0].Type != "registry" || captured.CacheExports[0].Attrs["ref"] != "cache/export" {
		t.Fatalf("cache-to not parsed: %#v", captured.CacheExports)
	}
	if captured.Interactive == nil {
		t.Fatalf("interactive config missing")
	}
	if got := strings.Join(captured.Interactive.Shell, " "); got != "/bin/bash -l" {
		t.Fatalf("interactive shell mismatch: %s", got)
	}
	if captured.Interactive.Stdin == nil || captured.Interactive.Stdout == nil || captured.Interactive.Stderr == nil {
		t.Fatalf("interactive stdio handles not set")
	}

	if out := stdout.String(); !strings.Contains(out, "Built example.com/app:dev") {
		t.Fatalf("expected build output, got %q", out)
	}
	if errText := stderr.String(); errText != "" {
		t.Fatalf("expected empty stderr, got %q", errText)
	}
}

func TestBuildCommandNoCacheDisablesCacheImports(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()
	origBuildFn := buildDockerfileFn
	defer func() { buildDockerfileFn = origBuildFn }()

	var captured buildkit.DockerfileBuildOptions
	buildDockerfileFn = func(_ context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
		captured = opts
		return &buildkit.BuildResult{Digest: "sha256:abc"}, nil
	}

	cmd := newBuildCommand()
	cmd.SetArgs([]string{
		"--cache-from", "type=registry,ref=cache/main",
		"--no-cache",
		ctxDir,
	})
	cmd.SetIn(newFakeTTY())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if !captured.NoCache {
		t.Fatalf("no-cache flag not propagated")
	}
	if len(captured.CacheImports) != 0 {
		t.Fatalf("cache imports should be empty when --no-cache is set: %#v", captured.CacheImports)
	}
	if out := stdout.String(); !strings.Contains(out, "Built ") {
		t.Fatalf("expected build output, got %q", out)
	}
	if errText := stderr.String(); !strings.Contains(errText, "ignoring --cache-from entries because --no-cache is set") {
		t.Fatalf("expected warning about ignored cache imports, got %q", errText)
	}
}

func TestBuildCommandHelpListsAllFlags(t *testing.T) {
	disableSandboxForTests(t)
	cmd := newBuildCommand()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help command returned error: %v", err)
	}

	help := buf.String()
	flags := []string{
		"--build-arg",
		"--builder",
		"--cache-dir",
		"--cache-from",
		"--cache-to",
		"-f, --file",
		"-h, --help",
		"-i, --interactive",
		"--interactive-shell",
		"--logfile",
		"--load",
		"--no-cache",
		"--platform",
		"-q, --quiet",
		"--push",
		"--rm",
		"--sandbox-logs",
		"--secret",
		"-t, --tag",
	}
	for _, flag := range flags {
		if !strings.Contains(help, flag) {
			t.Fatalf("help output missing %s: %s", flag, help)
		}
	}
}

func TestBuildCommandLogfileRedirectsOutput(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "build.log")

	origBuildFn := buildDockerfileFn
	defer func() { buildDockerfileFn = origBuildFn }()

	buildDockerfileFn = func(_ context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
		return &buildkit.BuildResult{Digest: "sha256:logfile"}, nil
	}

	cmd := newBuildCommand()
	cmd.SetArgs([]string{"--logfile", logFile, ctxDir})
	cmd.SetIn(newFakeTTY())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v", err)
	}

	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("expected no stdout/stderr output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read logfile: %v", err)
	}
	if !strings.Contains(string(data), "Built ") {
		t.Fatalf("expected build output in logfile, got %q", string(data))
	}
}

func disableSandboxForTests(t *testing.T) {
	t.Helper()
	t.Setenv("KTL_SANDBOX_DISABLE", "1")
	t.Setenv("KTL_NSJAIL_DISABLE", "1")
}

type fakeTTY struct {
	*bytes.Buffer
}

func newFakeTTY() *fakeTTY {
	return &fakeTTY{Buffer: bytes.NewBuffer(nil)}
}

func (f *fakeTTY) Close() error { return nil }
func (f *fakeTTY) Fd() uintptr  { return 0 }
func (f *fakeTTY) Name() string { return "/dev/ttyFAKE" }
