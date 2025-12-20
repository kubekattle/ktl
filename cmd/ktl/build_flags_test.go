// File: cmd/ktl/build_flags_test.go
// Brief: CLI command wiring and implementation for 'build flags'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/example/ktl/internal/workflows/buildsvc"
)

type recordingBuildService struct {
	lastOpts buildsvc.Options
}

func (r *recordingBuildService) Run(_ context.Context, opts buildsvc.Options) (*buildsvc.Result, error) {
	r.lastOpts = opts
	opts.Streams.OutWriter().Write([]byte("Built " + strings.Join(opts.Tags, ", ") + "\n"))
	return &buildsvc.Result{Tags: opts.Tags}, nil
}

func TestBuildCommandFlagPropagation(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()
	cacheDir := t.TempDir()
	t.Setenv("MY_SECRET", "super-secret")

	rec := &recordingBuildService{}
	cmd := newBuildCommandWithService(rec)
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

	opts := rec.lastOpts
	if opts.ContextDir != ctxDir {
		t.Fatalf("context dir mismatch: %s", opts.ContextDir)
	}
	if opts.Dockerfile != "Altfile" {
		t.Fatalf("dockerfile mismatch: %s", opts.Dockerfile)
	}
	if opts.Builder != "tcp://buildkitd:1234" {
		t.Fatalf("builder not propagated: %s", opts.Builder)
	}
	if opts.CacheDir != cacheDir {
		t.Fatalf("cache dir mismatch")
	}
	if len(opts.Platforms) != 2 || opts.Platforms[0] != "linux/amd64" || opts.Platforms[1] != "linux/arm64" {
		t.Fatalf("platforms not parsed: %#v", opts.Platforms)
	}
	if len(opts.Tags) != 1 || opts.Tags[0] != "example.com/app:dev" {
		t.Fatalf("tags missing: %#v", opts.Tags)
	}
	if !opts.Push || !opts.Load {
		t.Fatalf("push/load flags not propagated")
	}
	if !containsSubstr(opts.BuildArgs, "FOO=bar") {
		t.Fatalf("build args missing: %#v", opts.BuildArgs)
	}
	if len(opts.Secrets) != 1 || opts.Secrets[0] != "MY_SECRET" {
		t.Fatalf("secret missing: %#v", opts.Secrets)
	}
	if !containsSubstr(opts.CacheFrom, "ref=cache/main") {
		t.Fatalf("cache-from missing: %#v", opts.CacheFrom)
	}
	if !containsSubstr(opts.CacheTo, "ref=cache/export") {
		t.Fatalf("cache-to missing: %#v", opts.CacheTo)
	}
	if !opts.Interactive || opts.InteractiveShell != "/bin/bash -l" {
		t.Fatalf("interactive flags missing")
	}
	if out := stdout.String(); !strings.Contains(out, "Built example.com/app:dev") {
		t.Fatalf("expected build output, got %q", out)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}

func TestBuildCommandNoCacheDisablesCacheImports(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()
	rec := &recordingBuildService{}
	cmd := newBuildCommandWithService(rec)
	cmd.SetArgs([]string{
		"--cache-from", "type=registry,ref=cache/main",
		"--no-cache",
		ctxDir,
	})
	cmd.SetIn(newFakeTTY())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if !rec.lastOpts.NoCache {
		t.Fatalf("no-cache flag not propagated")
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
			t.Fatalf("help output missing %s", flag)
		}
	}
}

func containsSubstr(list []string, target string) bool {
	for _, v := range list {
		if strings.Contains(v, target) {
			return true
		}
	}
	return false
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
