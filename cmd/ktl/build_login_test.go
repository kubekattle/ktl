package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/cli/cli/config"
	"github.com/spf13/cobra"
)

func TestRunBuildLoginStoresCredentials(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	pingRegistryFn = func(server, user, pass string) error { return nil }
	defer func() { pingRegistryFn = pingRegistry }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(""))

	opts := loginOptions{Server: "ghcr.io", Username: "octocat", Password: "secret"}
	if err := runBuildLogin(cmd, opts); err != nil {
		t.Fatalf("runBuildLogin returned error: %v", err)
	}

	cfg := config.LoadDefaultConfigFile(io.Discard)
	store := cfg.GetCredentialsStore("ghcr.io")
	auth, err := store.Get("ghcr.io")
	if err != nil {
		t.Fatalf("get stored credentials: %v", err)
	}
	if auth.Username != "octocat" || auth.Password != "secret" {
		t.Fatalf("unexpected auth config: %#v", auth)
	}
}

func TestRunBuildLogoutRemovesCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	pingRegistryFn = func(server, user, pass string) error { return nil }
	defer func() { pingRegistryFn = pingRegistry }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(""))

	if err := runBuildLogin(cmd, loginOptions{Server: "ghcr.io", Username: "octocat", Password: "secret"}); err != nil {
		t.Fatalf("login failed: %v", err)
	}

	logoutCmd := &cobra.Command{}
	logoutCmd.SetOut(io.Discard)
	logoutCmd.SetErr(io.Discard)
	if err := runBuildLogout(logoutCmd, "ghcr.io", ""); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	cfg := config.LoadDefaultConfigFile(io.Discard)
	store := cfg.GetCredentialsStore("ghcr.io")
	if auth, err := store.Get("ghcr.io"); err == nil && auth.Username != "" {
		t.Fatalf("expected credentials removed, still found %#v", auth)
	}
}

func TestNormalizeRegistryServer(t *testing.T) {
	cases := map[string]string{
		"":                            defaultRegistryServer,
		"docker.io":                   defaultRegistryServer,
		"index.docker.io":             defaultRegistryServer,
		"registry-1.docker.io":        defaultRegistryServer,
		"https://index.docker.io/v1/": defaultRegistryServer,
		"ghcr.io":                     "ghcr.io",
	}
	for input, expect := range cases {
		if got := normalizeRegistryServer(input); got != expect {
			t.Fatalf("normalizeRegistryServer(%q)=%q, want %q", input, got, expect)
		}
	}
}

func TestReadPasswordFromStdinTrims(t *testing.T) {
	cmd := &cobra.Command{}
	buf := bytes.NewBufferString("token\n")
	cmd.SetIn(buf)
	pass, err := readPasswordFromStdin(cmd)
	if err != nil {
		t.Fatalf("readPasswordFromStdin error: %v", err)
	}
	if pass != "token" {
		t.Fatalf("expected token, got %q", pass)
	}
}

func TestPromptForInputFromReader(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("value\n"))
	cmd.SetErr(io.Discard)
	val, err := promptForInput(cmd, "Label")
	if err != nil {
		t.Fatalf("promptForInput error: %v", err)
	}
	if val != "value" {
		t.Fatalf("expected value, got %q", val)
	}
}

func TestRunBuildLoginRejectsPasswordFlagCombination(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(""))
	err := runBuildLogin(cmd, loginOptions{Server: "ghcr.io", Username: "u", Password: "one", PasswordStdin: true})
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("expected error about password flag, got %v", err)
	}
}

func TestRegistryPingEndpointDefault(t *testing.T) {
	if got := registryPingEndpoint(""); got != "https://registry-1.docker.io" {
		t.Fatalf("unexpected endpoint: %s", got)
	}
	if got := registryPingEndpoint("ghcr.io"); got != "https://ghcr.io" {
		t.Fatalf("unexpected endpoint: %s", got)
	}
}

func TestDockerConfigDirectoryCreated(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "config")
	os.MkdirAll(nested, 0o755)
	t.Setenv("DOCKER_CONFIG", nested)
	pingRegistryFn = func(server, user, pass string) error { return nil }
	defer func() { pingRegistryFn = pingRegistry }()

	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(""))
	if err := runBuildLogin(cmd, loginOptions{Server: "registry.example.com", Username: "user", Password: "pass"}); err != nil {
		t.Fatalf("login failed: %v", err)
	}
}
