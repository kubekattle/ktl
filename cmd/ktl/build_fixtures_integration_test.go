//go:build integration && linux

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildDockerfileFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	requireCommand(t, "docker")

	fixtures := []string{
		repoTestdata("build", "dockerfiles", "basic"),
		repoTestdata("build", "dockerfiles", "tools"),
		repoTestdata("build", "dockerfiles", "multistage"),
		repoTestdata("build", "dockerfiles", "metadata"),
		repoTestdata("build", "dockerfiles", "scripts"),
	}
	for _, contextDir := range fixtures {
		contextDir := contextDir
		t.Run(filepath.Base(contextDir), func(t *testing.T) {
			runBuildFixture(t, contextDir, []string{"--no-cache"}, nil)
		})
	}
}

func TestBuildComposeFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	requireCommand(t, "docker")

	type composeFixture struct {
		name string
		file string
		args []string
	}
	fixtures := []composeFixture{
		{name: "apps", file: "docker-compose.apps.yml"},
		{name: "profiles", file: "docker-compose.profiles.yml", args: []string{"--compose-profile", "jobs"}},
		{name: "args", file: "docker-compose.args.yml", args: []string{"--compose-service", "proxy"}},
		{name: "metrics", file: "docker-compose.metrics.yml"},
		{name: "ingest", file: "docker-compose.ingest.yml", args: []string{"--compose-service", "ingest"}},
	}

	contextDir := repoTestdata("build", "compose")
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			composePath := repoTestdata("build", "compose", fixture.file)
			args := append([]string{
				"--mode", "compose",
				"--compose-file", composePath,
				"--compose-project", "fixtures",
				"--no-cache",
			}, fixture.args...)
			runBuildFixture(t, contextDir, args, nil)
		})
	}
}

func runBuildFixture(t *testing.T, contextDir string, extraArgs, extraEnv []string) {
	t.Helper()
	ktlBin := buildIntegrationBinary(t)
	tag := fmt.Sprintf("ktl.local/e2e/%s:%d", filepath.Base(contextDir), time.Now().UnixNano())

	args := []string{"build", contextDir, "--tag", tag}
	args = append(args, extraArgs...)

	cmd := exec.Command(ktlBin, args...)
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	env := append(os.Environ(), extraEnv...)
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		t.Fatalf("ktl build %s failed: %v\n%s", contextDir, err, buf.String())
	}
	output := buf.Bytes()
	if containsComposeMode(extraArgs) {
		if !bytes.Contains(output, []byte(": ktl")) {
			t.Fatalf("expected compose service output for %s:\n%s", contextDir, buf.String())
		}
		return
	}
	if !bytes.Contains(output, []byte("Built "+tag)) {
		t.Fatalf("expected successful build output for %s:\n%s", tag, buf.String())
	}
}

func containsComposeMode(args []string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] == "--mode" && i+1 < len(args) && strings.EqualFold(args[i+1], "compose") {
			return true
		}
	}
	return false
}
