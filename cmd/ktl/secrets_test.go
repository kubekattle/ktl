package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretsTestCommandResolvesReference(t *testing.T) {
	configPath := writeSecretsTestConfig(t)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"secrets", "test", "--secret-config", configPath, "--ref", "secret://file/api/token"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Resolved secret://file/api/token") {
		t.Fatalf("expected resolved output, got:\n%s", got)
	}
}

func TestSecretsListCommandJSON(t *testing.T) {
	configPath := writeSecretsTestConfig(t)

	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"secrets", "list", "--secret-config", configPath, "--secret-provider", "file", "--path", "api", "--format", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var items []string
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !containsString(items, "token") || !containsString(items, "user") {
		t.Fatalf("expected list to contain token and user, got: %v", items)
	}
}

func writeSecretsTestConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	secretsPath := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("api:\n  token: s3cr3t\n  user: admin\n"), 0o600); err != nil {
		t.Fatalf("write secrets: %v", err)
	}
	configPath := filepath.Join(dir, "secrets-config.yaml")
	config := "defaultProvider: file\nproviders:\n  file:\n    type: file\n    path: secrets.yaml\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
