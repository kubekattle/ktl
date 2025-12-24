package buildsvc

import (
	"bytes"
	"context"
	"testing"

	"github.com/example/ktl/internal/secrets"
)

func TestSecretsGuardPreflight_DockerfileFixtures(t *testing.T) {
	ctx := context.Background()

	guard, err := newSecretsGuard(ctx, "warn", "", "", "")
	if err != nil {
		t.Fatalf("newSecretsGuard: %v", err)
	}

	t.Run("positive", func(t *testing.T) {
		var out bytes.Buffer
		opts := Options{
			ContextDir: testdataPath("build", "dockerfiles", "secrets-positive"),
			BuildMode:  string(ModeDockerfile),
		}
		rep, err := guard.preflight(&out, opts)
		if err != nil {
			t.Fatalf("preflight returned error in warn mode: %v", err)
		}
		if rep == nil || len(rep.Findings) == 0 {
			t.Fatalf("expected findings, got none")
		}
		if !containsFindingRule(rep.Findings, "arg_value_github_token") {
			t.Fatalf("expected arg_value_github_token finding, got: %#v", rep.Findings)
		}
		if !containsFindingRule(rep.Findings, "arg_value_private_key") {
			t.Fatalf("expected arg_value_private_key finding, got: %#v", rep.Findings)
		}
	})

	t.Run("negative", func(t *testing.T) {
		var out bytes.Buffer
		opts := Options{
			ContextDir: testdataPath("build", "dockerfiles", "secrets-negative"),
			BuildMode:  string(ModeDockerfile),
		}
		rep, err := guard.preflight(&out, opts)
		if err != nil {
			t.Fatalf("preflight returned error in warn mode: %v", err)
		}
		if rep != nil && len(rep.Findings) != 0 {
			t.Fatalf("expected no findings, got: %#v", rep.Findings)
		}
	})
}

func TestSecretsGuardPreflight_ComposeFixture(t *testing.T) {
	ctx := context.Background()

	guard, err := newSecretsGuard(ctx, "warn", "", "", "")
	if err != nil {
		t.Fatalf("newSecretsGuard: %v", err)
	}

	var out bytes.Buffer
	composePath := testdataPath("build", "compose", "docker-compose.secrets.yml")
	opts := Options{
		BuildMode:       string(ModeCompose),
		ComposeFiles:    []string{composePath},
		ComposeServices: []string{"app"},
		ComposeProject:  "fixtures",
	}
	rep, err := guard.preflight(&out, opts)
	if err != nil {
		t.Fatalf("preflight returned error in warn mode: %v", err)
	}
	if rep == nil || len(rep.Findings) == 0 {
		t.Fatalf("expected findings, got none")
	}
	if !containsFindingRule(rep.Findings, "arg_value_github_token") {
		t.Fatalf("expected arg_value_github_token finding, got: %#v", rep.Findings)
	}
	if !containsFindingRule(rep.Findings, "arg_value_jwt") {
		t.Fatalf("expected arg_value_jwt finding, got: %#v", rep.Findings)
	}

	for _, f := range rep.Findings {
		if f.Location == "" {
			t.Fatalf("expected location to be set for finding: %#v", f)
		}
		if !bytes.Contains([]byte(f.Location), []byte("docker-compose.secrets.yml")) {
			t.Fatalf("expected location to mention docker-compose.secrets.yml, got %q", f.Location)
		}
	}
}

func containsFindingRule(findings []secrets.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}
