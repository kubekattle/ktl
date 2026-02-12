// File: cmd/ktl/build_flags_test.go
// Brief: CLI command wiring and implementation for 'build flags'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kubekattle/ktl/internal/workflows/buildsvc"
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
	authFile := t.TempDir() + "/config.json"
	t.Setenv("KTL_DOCKER_CONFIG", "")
	t.Setenv("DOCKER_CONFIG", "")
	t.Setenv("MY_SECRET", "super-secret")
	t.Setenv("KTL_AUTHFILE", "")
	t.Setenv("KTL_REGISTRY_AUTH_FILE", "")
	t.Setenv("REGISTRY_AUTH_FILE", "")
	writeFile(t, authFile, "{}\n")
	t.Setenv("MY_SECRET", "super-secret")

	rec := &recordingBuildService{}
	profile := "dev"
	logLevel := "info"
	cmd := newBuildCommandWithService(rec, &profile, &logLevel, nil, nil)
	cmd.SetIn(newFakeTTY())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--build-arg=FOO=bar",
		"--builder=tcp://buildkitd:1234",
		"--cache-dir=" + cacheDir,
		"--cache-from=type=registry,ref=cache/main,mode=max",
		"--cache-to=type=registry,ref=cache/export,mode=max",
		"--file=Altfile",
		"--sbom",
		"--provenance",
		"--attest-dir=" + t.TempDir(),
		"--capture=" + t.TempDir() + "/capture.sqlite",
		"--capture-tag=team=platform",
		"--load",
		"--platform=linux/amd64,linux/arm64",
		"--push",
		"--sign",
		"--sign-key=awskms://example/key",
		"--rekor-url=https://rekor.example.invalid",
		"--tlog-upload=false",
		"--secret=MY_SECRET",
		"-i",
		"--interactive-shell=/bin/bash -l",
		"--mode=compose",
		"--compose-file=docker-compose.yml",
		"--compose-profile=dev",
		"--compose-service=api",
		"--compose-project=example",
		"--compose-parallelism=5",
		"--output=logs",
		"--authfile=" + authFile,
		"--sandbox-config=sandbox/linux-ci.cfg",
		"--sandbox-bin=/usr/local/bin/nsjail",
		"--sandbox-bind=/tmp:/tmp",
		"--sandbox-workdir=/work",
		"--sandbox-probe-path=/tmp",
		"--ws-listen=:9085",
		"--sandbox",
		"--tag=example.com/app:dev",
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
	if !opts.AttestSBOM || !opts.AttestProvenance {
		t.Fatalf("sbom/provenance flags not propagated")
	}
	if strings.TrimSpace(opts.AttestationDir) == "" {
		t.Fatalf("attest-dir not propagated")
	}
	if strings.TrimSpace(opts.CapturePath) == "" {
		t.Fatalf("capture not propagated")
	}
	if len(opts.CaptureTags) != 1 || opts.CaptureTags[0] != "team=platform" {
		t.Fatalf("capture tags not propagated: %#v", opts.CaptureTags)
	}
	if !opts.Sign {
		t.Fatalf("sign flag not propagated")
	}
	if opts.SignKey != "awskms://example/key" {
		t.Fatalf("sign-key not propagated: %q", opts.SignKey)
	}
	if opts.RekorURL != "https://rekor.example.invalid" {
		t.Fatalf("rekor-url not propagated: %q", opts.RekorURL)
	}
	if opts.TLogUpload != "false" {
		t.Fatalf("tlog-upload not propagated: %q", opts.TLogUpload)
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
	if opts.BuildMode != "compose" {
		t.Fatalf("mode not propagated: %q", opts.BuildMode)
	}
	if opts.Output != "logs" {
		t.Fatalf("output mode not propagated: %q", opts.Output)
	}
	if len(opts.ComposeFiles) != 1 || opts.ComposeFiles[0] != "docker-compose.yml" {
		t.Fatalf("compose-file not propagated: %#v", opts.ComposeFiles)
	}
	if len(opts.ComposeProfiles) != 1 || opts.ComposeProfiles[0] != "dev" {
		t.Fatalf("compose-profile not propagated: %#v", opts.ComposeProfiles)
	}
	if len(opts.ComposeServices) != 1 || opts.ComposeServices[0] != "api" {
		t.Fatalf("compose-service not propagated: %#v", opts.ComposeServices)
	}
	if opts.ComposeProject != "example" {
		t.Fatalf("compose-project not propagated: %q", opts.ComposeProject)
	}
	if opts.ComposeParallelism != 5 {
		t.Fatalf("compose parallelism missing: %d", opts.ComposeParallelism)
	}
	if opts.AuthFile != authFile {
		t.Fatalf("authfile not propagated: %q", opts.AuthFile)
	}
	if opts.SandboxConfig != "sandbox/linux-ci.cfg" {
		t.Fatalf("sandbox-config not propagated: %q", opts.SandboxConfig)
	}
	if opts.SandboxBin != "/usr/local/bin/nsjail" {
		t.Fatalf("sandbox-bin not propagated: %q", opts.SandboxBin)
	}
	if len(opts.SandboxBinds) != 1 || opts.SandboxBinds[0] != "/tmp:/tmp" {
		t.Fatalf("sandbox-bind not propagated: %#v", opts.SandboxBinds)
	}
	if opts.SandboxWorkdir != "/work" {
		t.Fatalf("sandbox-workdir not propagated: %q", opts.SandboxWorkdir)
	}
	if opts.SandboxProbePath != "/tmp" {
		t.Fatalf("sandbox-probe-path not propagated: %q", opts.SandboxProbePath)
	}
	if opts.WSListenAddr != ":9085" {
		t.Fatalf("ws-listen not propagated: %q", opts.WSListenAddr)
	}
	if !opts.RequireSandbox {
		t.Fatalf("expected sandbox to be required")
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
	profile := "dev"
	logLevel := "info"
	cmd := newBuildCommandWithService(rec, &profile, &logLevel, nil, nil)
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
		"--attest-dir",
		"--authfile",
		"--build-arg",
		"--builder",
		"--cache-dir",
		"--cache-from",
		"--cache-to",
		"--capture",
		"--capture-tag",
		"--compose-parallelism",
		"--compose-file",
		"--compose-profile",
		"--compose-project",
		"--compose-service",
		"-f, --file",
		"-h, --help",
		"-i, --interactive",
		"--interactive-shell",
		"--logfile",
		"--load",
		"--mode",
		"--no-cache",
		"--platform",
		"--provenance",
		"-q, --quiet",
		"--rekor-url",
		"--push",
		"--rm",
		"--sandbox-bind",
		"--sandbox-bin",
		"--sandbox-config",
		"--sandbox-logs",
		"--sandbox-probe-path",
		"--sandbox-bind-home",
		"--sandbox",
		"--sandbox-workdir",
		"--sbom",
		"--secret",
		"--sign-key",
		"--sign",
		"-t, --tag",
		"--tlog-upload",
		"--ws-listen",
		"--remote-build",
	}
	for _, flag := range flags {
		if !strings.Contains(help, flag) {
			t.Fatalf("help output missing %s", flag)
		}
	}
}

func TestBuildCommandMirrorFlagsRejectInvalidCombinations(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()

	cases := []struct {
		name       string
		args       []string
		wantSubstr string
	}{
		{
			name:       "ws-listen with quiet",
			args:       []string{"--ws-listen", ":9085", "--quiet", ctxDir},
			wantSubstr: "--ws-listen cannot be combined with --quiet",
		},
		{
			name:       "ws-listen with logfile",
			args:       []string{"--ws-listen", ":9085", "--logfile", "out.log", ctxDir},
			wantSubstr: "--ws-listen cannot be combined with --logfile",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := "dev"
			logLevel := "info"
			cmd := newBuildCommandWithService(&recordingBuildService{}, &profile, &logLevel, nil, nil)
			cmd.SetIn(newFakeTTY())
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q missing %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestBuildCommandDefaultBuilderIsNotForced(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()

	rec := &recordingBuildService{}
	profile := "dev"
	logLevel := "info"
	cmd := newBuildCommandWithService(rec, &profile, &logLevel, nil, nil)
	cmd.SetIn(newFakeTTY())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--tag", "example.com/app:dev", ctxDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("command returned error: %v", err)
	}
	if rec.lastOpts.Builder != "" {
		t.Fatalf("expected builder to be empty unless explicitly set, got %q", rec.lastOpts.Builder)
	}
}

func TestBuildCommandRejectsInvalidFlagValuesAtParseTime(t *testing.T) {
	disableSandboxForTests(t)
	ctxDir := t.TempDir()

	cases := []struct {
		name       string
		args       []string
		wantSubstr string
	}{
		{
			name:       "invalid mode",
			args:       []string{"--mode", "nope", ctxDir},
			wantSubstr: "must be one of",
		},
		{
			name:       "invalid build-arg",
			args:       []string{"--build-arg", "INVALID", ctxDir},
			wantSubstr: "expected KEY=VALUE",
		},
		{
			name:       "invalid cache-from missing type",
			args:       []string{"--cache-from", "ref=cache/main,mode=max", ctxDir},
			wantSubstr: "missing type",
		},
		{
			name:       "invalid tag",
			args:       []string{"--tag", "not a tag", ctxDir},
			wantSubstr: "invalid tag",
		},
		{
			name:       "invalid platform",
			args:       []string{"--platform", "linux", ctxDir},
			wantSubstr: "invalid platform",
		},
		{
			name:       "invalid compose parallelism",
			args:       []string{"--compose-parallelism", "-1", ctxDir},
			wantSubstr: "must be",
		},
		{
			name:       "invalid sandbox bind",
			args:       []string{"--sandbox-bind", "/tmp", ctxDir},
			wantSubstr: "expected host:guest",
		},
		{
			name:       "invalid remote build address",
			args:       []string{"--remote-build", "localhost", ctxDir},
			wantSubstr: "expected host:port",
		},
		{
			name:       "invalid tlog-upload",
			args:       []string{"--tlog-upload", "maybe", ctxDir},
			wantSubstr: "must be true or false",
		},
		{
			name:       "empty cache-dir",
			args:       []string{"--cache-dir", "", ctxDir},
			wantSubstr: "cannot be empty",
		},
		{
			name:       "invalid builder address",
			args:       []string{"--builder", "buildkitd.sock", ctxDir},
			wantSubstr: "expected a scheme",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := "dev"
			logLevel := "info"
			cmd := newBuildCommandWithService(&recordingBuildService{}, &profile, &logLevel, nil, nil)
			cmd.SetIn(newFakeTTY())
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q missing %q", err.Error(), tc.wantSubstr)
			}
		})
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

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeFile mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
