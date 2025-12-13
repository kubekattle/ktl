package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var _, testFile, _, _ = runtime.Caller(0)
var repoRoot = filepath.Clean(filepath.Join(filepath.Dir(testFile), "..", ".."))

func repoTestdata(parts ...string) string {
	base := append([]string{repoRoot, "testdata"}, parts...)
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

func TestResolveComposeFilesUsesTestdata(t *testing.T) {
	dir := repoTestdata("build", "compose")
	files, err := resolveComposeFiles([]string{filepath.Join(dir, "docker-compose.yml")})
	if err != nil {
		t.Fatalf("resolveComposeFiles returned error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one compose file, got %d", len(files))
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Fatalf("expected compose file to exist: %v", err)
	}
}

func TestRequireBuildContextArg(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		err := requireBuildContextArg(nil, nil)
		if err == nil {
			t.Fatal("expected error when context argument missing")
		}
		want := "'ktl build' requires 1 argument (CONTEXT). Try '.' for the current directory"
		if err.Error() != want {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		err := requireBuildContextArg(nil, []string{"one", "two"})
		if err == nil {
			t.Fatal("expected error for extra args")
		}
		if !strings.Contains(err.Error(), "exactly one context") {
			t.Fatalf("expected error to mention single context, got: %v", err)
		}
	})

	t.Run("single", func(t *testing.T) {
		if err := requireBuildContextArg(nil, []string{"."}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}

func TestSelectBuildModeAutoPrefersDockerfile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")
	writeFile(t, filepath.Join(dir, "docker-compose.yml"), "services: {}\n")

	opts := buildCLIOptions{
		contextDir: dir,
		dockerfile: "Dockerfile",
		buildMode:  string(modeAuto),
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

	opts := buildCLIOptions{
		contextDir: dir,
		dockerfile: "Dockerfile",
		buildMode:  string(modeAuto),
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
	opts := buildCLIOptions{
		contextDir: dir,
		dockerfile: "Dockerfile",
		buildMode:  string(modeCompose),
	}
	if _, _, err := selectBuildMode(dir, opts); err == nil {
		t.Fatal("expected error when compose mode requested without compose files")
	}
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}
