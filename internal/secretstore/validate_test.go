package secretstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRefsMissingProvider(t *testing.T) {
	dir := t.TempDir()
	secretsPath := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("db:\n  password: s3cr3t\n"), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	resolver, err := NewResolver(Config{
		Providers: map[string]ProviderConfig{
			"local": {Type: "file", Path: secretsPath},
		},
		DefaultProvider: "local",
	}, ResolverOptions{})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	values := map[string]interface{}{
		"db": map[string]interface{}{
			"password": "secret://vault/app/db#password",
		},
	}

	err = ValidateRefs(context.Background(), resolver, values, ValidationOptions{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if !strings.Contains(err.Error(), "vault") {
		t.Fatalf("expected error to mention missing provider, got: %s", err.Error())
	}
}

func TestValidateRefsMissingFileKey(t *testing.T) {
	dir := t.TempDir()
	secretsPath := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("db:\n  user: admin\n"), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	resolver, err := NewResolver(Config{
		Providers: map[string]ProviderConfig{
			"local": {Type: "file", Path: secretsPath},
		},
		DefaultProvider: "local",
	}, ResolverOptions{})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	values := map[string]interface{}{
		"db": map[string]interface{}{
			"password": "secret://local/db/password",
		},
	}

	err = ValidateRefs(context.Background(), resolver, values, ValidationOptions{})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "available keys") {
		t.Fatalf("expected suggestions in error, got: %s", err.Error())
	}
}
