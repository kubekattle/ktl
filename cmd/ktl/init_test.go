package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/ktl/internal/appconfig"
	"gopkg.in/yaml.v3"
)

func TestInitWritesConfig(t *testing.T) {
	dir := t.TempDir()

	kubeconfigPath := filepath.Join(dir, "kubeconfig.yaml")
	kubeconfig := `apiVersion: v1
kind: Config
current-context: dev
contexts:
- name: dev
  context:
    cluster: dev
    user: dev
    namespace: dev
clusters:
- name: dev
  cluster:
    server: https://example.com
users:
- name: dev
  user:
    token: dummy
`
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfigPath)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", dir, "--secrets-file", "./secrets.local.yaml", "--profile", "ci"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(strings.ToLower(errOut.String()), "warning:") {
		t.Fatalf("expected no warnings, got: %q", errOut.String())
	}

	written := filepath.Join(dir, ".ktl.yaml")
	raw, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read .ktl.yaml: %v", err)
	}

	var cfg appconfig.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal .ktl.yaml: %v", err)
	}
	if cfg.Build.Profile != "ci" {
		t.Fatalf("expected build.profile to be ci, got %q", cfg.Build.Profile)
	}
	if cfg.Secrets.DefaultProvider != "local" {
		t.Fatalf("expected secrets.defaultProvider local, got %q", cfg.Secrets.DefaultProvider)
	}
	provider, ok := cfg.Secrets.Providers["local"]
	if !ok {
		t.Fatalf("expected local provider in secrets config")
	}
	if provider.Type != "file" {
		t.Fatalf("expected local provider type file, got %q", provider.Type)
	}
	if provider.Path != "./secrets.local.yaml" {
		t.Fatalf("expected local provider path ./secrets.local.yaml, got %q", provider.Path)
	}
}

func TestInitDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"init", dir, "--dry-run"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".ktl.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no .ktl.yaml to be written, got err=%v", err)
	}
	if !strings.Contains(out.String(), "build:") {
		t.Fatalf("expected dry-run output to include config YAML, got:\n%s", out.String())
	}
}

func TestInitMergePreservesExisting(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	existing := `build:
  profile: secure
secrets:
  defaultProvider: vault
  providers:
    vault:
      type: vault
      address: https://vault.example.com
`
	if err := os.WriteFile(filepath.Join(dir, ".ktl.yaml"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write .ktl.yaml: %v", err)
	}

	root := newRootCommand()
	root.SetArgs([]string{"init", dir, "--merge"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".ktl.yaml"))
	if err != nil {
		t.Fatalf("read .ktl.yaml: %v", err)
	}
	var cfg appconfig.Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal .ktl.yaml: %v", err)
	}
	if cfg.Build.Profile != "secure" {
		t.Fatalf("expected build.profile secure, got %q", cfg.Build.Profile)
	}
	if cfg.Secrets.DefaultProvider != "vault" {
		t.Fatalf("expected secrets.defaultProvider vault, got %q", cfg.Secrets.DefaultProvider)
	}
	if _, ok := cfg.Secrets.Providers["vault"]; !ok {
		t.Fatalf("expected vault provider to remain")
	}
	if _, ok := cfg.Secrets.Providers["local"]; ok {
		t.Fatalf("did not expect local provider to be injected on merge")
	}
}

func TestInitOutputJSONPreset(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--dry-run", "--output", "json", "--preset", "prod"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		ConfigYAML string `json:"configYaml"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !strings.Contains(payload.ConfigYAML, "profile: secure") {
		t.Fatalf("expected secure profile in config, got:\n%s", payload.ConfigYAML)
	}
	if !strings.Contains(payload.ConfigYAML, "vault") {
		t.Fatalf("expected vault provider in config, got:\n%s", payload.ConfigYAML)
	}
}

func TestInitShowDiff(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KTL_CONFIG", cfgPath)

	existing := "build:\n  profile: dev\n"
	if err := os.WriteFile(filepath.Join(dir, ".ktl.yaml"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write .ktl.yaml: %v", err)
	}

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"init", dir, "--merge", "--show-diff", "--output", "json", "--dry-run"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !strings.Contains(payload.Diff, "@@") {
		t.Fatalf("expected unified diff output, got:\n%s", payload.Diff)
	}
}
