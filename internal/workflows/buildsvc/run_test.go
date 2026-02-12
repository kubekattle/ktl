// File: internal/workflows/buildsvc/run_test.go
// Brief: Internal buildsvc package implementation for 'run'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kubekattle/ktl/pkg/buildkit"
	"github.com/kubekattle/ktl/pkg/registry"
)

var _, pkgTestFile, _, _ = runtime.Caller(0)
var pkgRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(pkgTestFile), "..", "..", ".."))

func testdataPath(parts ...string) string {
	base := append([]string{pkgRepoRoot, "testdata"}, parts...)
	return filepath.Join(base...)
}

func TestParseKeyValueArgs(t *testing.T) {
	args, err := parseKeyValueArgs([]string{"FOO=bar", "VERSION=1"})
	if err != nil {
		t.Fatalf("parseKeyValueArgs returned error: %v", err)
	}
	if args["FOO"] != "bar" || args["VERSION"] != "1" {
		t.Fatalf("unexpected args map: %#v", args)
	}
	if _, err := parseKeyValueArgs([]string{"INVALID"}); err == nil {
		t.Fatalf("expected error for malformed arg")
	}
}

func TestParseCacheSpecs(t *testing.T) {
	specs, err := parseCacheSpecs([]string{"type=registry,ref=example.com/cache:latest"})
	if err != nil {
		t.Fatalf("parseCacheSpecs error: %v", err)
	}
	if len(specs) != 1 || specs[0].Type != "registry" || specs[0].Attrs["ref"] != "example.com/cache:latest" {
		t.Fatalf("unexpected cache spec: %#v", specs)
	}
	if _, err := parseCacheSpecs([]string{"ref=missing-type"}); err == nil {
		t.Fatalf("expected error for missing type")
	}
}

func TestParseKeyValueCSV(t *testing.T) {
	attrs, err := parseKeyValueCSV("type=registry,ref=cache,mode=max")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs["type"] != "registry" || attrs["ref"] != "cache" || attrs["mode"] != "max" {
		t.Fatalf("unexpected attributes: %#v", attrs)
	}
}

func TestExpandPlatforms(t *testing.T) {
	values := expandPlatforms([]string{"linux/amd64,linux/arm64", "linux/amd64"})
	if len(values) != 2 {
		t.Fatalf("expected 2 unique platforms, got %d", len(values))
	}
}

func TestFindComposeFiles(t *testing.T) {
	dir := testdataPath("build", "compose")
	files, err := findComposeFiles(dir)
	if err != nil {
		t.Fatalf("findComposeFiles error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected compose files in testdata")
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("expected compose file to exist: %v", err)
		}
	}
}

func TestSelectBuildModeAutoPrefersDockerfile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services: {}\n")

	opts := Options{
		ContextDir: dir,
		Dockerfile: "Dockerfile",
		BuildMode:  string(ModeAuto),
	}
	mode, files, err := selectBuildMode(dir, opts)
	if err != nil {
		t.Fatalf("selectBuildMode returned error: %v", err)
	}
	if mode != modeDockerfile {
		t.Fatalf("expected dockerfile mode, got %s", mode)
	}
	if len(files) != 0 {
		t.Fatalf("expected no compose files, got %d", len(files))
	}
}

func TestSelectBuildModeAutoFallsBackToCompose(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services: {}\n")

	opts := Options{
		ContextDir: dir,
		Dockerfile: "Dockerfile",
		BuildMode:  string(ModeAuto),
	}
	mode, files, err := selectBuildMode(dir, opts)
	if err != nil {
		t.Fatalf("selectBuildMode returned error: %v", err)
	}
	if mode != modeCompose {
		t.Fatalf("expected compose mode, got %s", mode)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "docker-compose.yml") {
		t.Fatalf("unexpected compose files: %v", files)
	}
}

func TestSelectBuildModeComposeRequiresFiles(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		ContextDir: dir,
		Dockerfile: "Dockerfile",
		BuildMode:  string(ModeCompose),
	}
	if _, _, err := selectBuildMode(dir, opts); err == nil {
		t.Fatal("expected error when compose mode requested without compose files")
	}
}

func TestSelectBuildModeAutoAcceptsComposeFileAsContext(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	writeFile(t, composePath, "services: {}\n")

	opts := Options{
		ContextDir: composePath,
		Dockerfile: "Dockerfile",
		BuildMode:  string(ModeAuto),
	}
	mode, files, err := selectBuildMode(composePath, opts)
	if err != nil {
		t.Fatalf("selectBuildMode returned error: %v", err)
	}
	if mode != modeCompose {
		t.Fatalf("expected compose mode, got %s", mode)
	}
	if len(files) != 1 || files[0] != composePath {
		t.Fatalf("unexpected compose files: %v", files)
	}
}

func TestSelectBuildModeDockerfileRejectsFileContext(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yml")
	writeFile(t, composePath, "services: {}\n")

	opts := Options{
		ContextDir: composePath,
		Dockerfile: "Dockerfile",
		BuildMode:  string(ModeDockerfile),
	}
	if _, _, err := selectBuildMode(composePath, opts); err == nil {
		t.Fatalf("expected error")
	}
}

type captureRunner struct {
	last buildkit.DockerfileBuildOptions
}

func (r *captureRunner) BuildDockerfile(ctx context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
	r.last = opts
	return &buildkit.BuildResult{Digest: "sha256:deadbeef"}, nil
}

type errRunner struct {
	last buildkit.DockerfileBuildOptions
	err  error
}

func (r *errRunner) BuildDockerfile(ctx context.Context, opts buildkit.DockerfileBuildOptions) (*buildkit.BuildResult, error) {
	r.last = opts
	return nil, r.err
}

type noopRegistry struct{}

func (noopRegistry) RecordBuild(tags []string, layoutPath string) error { return nil }

func (noopRegistry) PushReference(ctx context.Context, reference string, opts registry.PushOptions) error {
	return nil
}

func (noopRegistry) PushRepository(ctx context.Context, repository string, opts registry.PushOptions) error {
	return nil
}

func TestRun_UsesSandboxContextEnvForRelativeContextDir(t *testing.T) {
	t.Setenv(sandboxActiveEnvKey, "1")

	root := t.TempDir()
	ctxDir := filepath.Join(root, "ctx")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(ctxDir, "Dockerfile"), "FROM scratch\n")

	dockerCfgPath := filepath.Join(root, "docker", "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })
	if err := os.Chdir(ctxDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	t.Setenv(sandboxContextEnvKey, ctxDir)

	var out, errOut bytes.Buffer
	runner := &captureRunner{}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, runErr := svc.Run(context.Background(), Options{
		ContextDir: "./ctx",
		Dockerfile: "Dockerfile",
		AuthFile:   dockerCfgPath,
		BuildMode:  string(ModeDockerfile),
		Streams: Streams{
			Out: &out,
			Err: &errOut,
		},
	})
	if runErr != nil {
		t.Fatalf("Run returned error: %v\nstderr: %s", runErr, errOut.String())
	}
	if runner.last.ContextDir != ctxDir {
		t.Fatalf("expected build ContextDir %q, got %q", ctxDir, runner.last.ContextDir)
	}
}

func TestRun_InteractiveRequiresTTY(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nRUN echo ok\n")

	dockerCfgPath := filepath.Join(dir, "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	var out, errOut bytes.Buffer
	runner := &captureRunner{}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, runErr := svc.Run(context.Background(), Options{
		ContextDir:       dir,
		Dockerfile:       "Dockerfile",
		AuthFile:         dockerCfgPath,
		BuildMode:        string(ModeDockerfile),
		Interactive:      true,
		InteractiveShell: "/bin/sh",
		Streams: Streams{
			Out: &out,
			Err: &errOut,
		},
	})
	if runErr == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(runErr.Error(), "--interactive requires a TTY") {
		t.Fatalf("unexpected error: %v", runErr)
	}
}

func TestRun_InteractiveShellPropagatesToBuildRunner(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nRUN echo ok\n")

	dockerCfgPath := filepath.Join(dir, "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	var out, errOut bytes.Buffer
	runner := &captureRunner{}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, runErr := svc.Run(context.Background(), Options{
		ContextDir:       dir,
		Dockerfile:       "Dockerfile",
		AuthFile:         dockerCfgPath,
		BuildMode:        string(ModeDockerfile),
		Interactive:      true,
		InteractiveShell: "/bin/bash -lc 'echo hi'",
		Streams: Streams{
			In:        strings.NewReader(""),
			Out:       &out,
			Err:       &errOut,
			Terminals: []any{os.Stdin},
		},
	})
	if runErr != nil {
		t.Fatalf("Run returned error: %v\nstderr: %s", runErr, errOut.String())
	}
	if runner.last.Interactive == nil {
		t.Fatalf("expected Interactive config to be set")
	}
	if got, want := strings.Join(runner.last.Interactive.Shell, " "), "/bin/bash -lc echo hi"; got != want {
		t.Fatalf("unexpected interactive shell: %q (want %q)", got, want)
	}
	if runner.last.Interactive.Stdin == nil || runner.last.Interactive.Stdout == nil || runner.last.Interactive.Stderr == nil {
		t.Fatalf("expected interactive stdio to be set")
	}
}

func TestRun_InteractiveShellRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\nRUN echo ok\n")

	dockerCfgPath := filepath.Join(dir, "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	var out, errOut bytes.Buffer
	runner := &captureRunner{}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, runErr := svc.Run(context.Background(), Options{
		ContextDir:       dir,
		Dockerfile:       "Dockerfile",
		AuthFile:         dockerCfgPath,
		BuildMode:        string(ModeDockerfile),
		Interactive:      true,
		InteractiveShell: "",
		Streams: Streams{
			In:        strings.NewReader(""),
			Out:       &out,
			Err:       &errOut,
			Terminals: []any{os.Stdin},
		},
	})
	if runErr == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(runErr.Error(), "--interactive-shell") {
		t.Fatalf("unexpected error: %v", runErr)
	}
}

func TestRun_AttestDirEnablesAttestations(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")

	dockerCfgPath := filepath.Join(dir, "docker", "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	runner := &errRunner{err: errors.New("stop")}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, err := svc.Run(context.Background(), Options{
		ContextDir:     dir,
		Dockerfile:     "Dockerfile",
		AuthFile:       dockerCfgPath,
		BuildMode:      string(ModeDockerfile),
		AttestationDir: filepath.Join(dir, "attest"),
		Streams: Streams{
			Out: &bytes.Buffer{},
			Err: &bytes.Buffer{},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("expected stop error, got %v", err)
	}
	if !runner.last.AttestProvenance || !runner.last.AttestSBOM {
		t.Fatalf("expected attestations enabled, got provenance=%v sbom=%v", runner.last.AttestProvenance, runner.last.AttestSBOM)
	}
}

func TestRun_AttestDirRequiresOCIOutputOnSuccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")

	dockerCfgPath := filepath.Join(dir, "docker", "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	runner := &captureRunner{}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, err := svc.Run(context.Background(), Options{
		ContextDir:     dir,
		Dockerfile:     "Dockerfile",
		AuthFile:       dockerCfgPath,
		BuildMode:      string(ModeDockerfile),
		AttestationDir: filepath.Join(dir, "attest"),
		Tags:           []string{"example.com/app:test"},
		Streams: Streams{
			Out: &bytes.Buffer{},
			Err: &bytes.Buffer{},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "--attest-dir requires an OCI layout export") {
		t.Fatalf("expected attest-dir error, got %v", err)
	}
}

func TestRun_SignRequiresPush(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")

	dockerCfgPath := filepath.Join(dir, "docker", "config.json")
	writeFile(t, dockerCfgPath, "{}\n")

	runner := &captureRunner{}
	svc := New(Dependencies{
		BuildRunner: runner,
		Registry:    noopRegistry{},
	})

	_, err := svc.Run(context.Background(), Options{
		ContextDir: dir,
		Dockerfile: "Dockerfile",
		AuthFile:   dockerCfgPath,
		BuildMode:  string(ModeDockerfile),
		Tags:       []string{"example.com/app:test"},
		Sign:       true,
		Push:       false,
		Streams: Streams{
			Out: &bytes.Buffer{},
			Err: &bytes.Buffer{},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "--sign requires --push") {
		t.Fatalf("expected sign requires push error, got %v", err)
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeFile mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
