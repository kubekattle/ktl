package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/ktl/internal/secretstore"
	"helm.sh/helm/v3/pkg/cli"
)

func TestBuildValuesResolvesSecretRefs(t *testing.T) {
	tempDir := t.TempDir()
	secretsPath := filepath.Join(tempDir, "secrets.yaml")
	payload := "db:\n  password: s3cr3t\n"
	if err := os.WriteFile(secretsPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	resolver, err := secretstore.NewResolver(secretstore.Config{
		Providers: map[string]secretstore.ProviderConfig{
			"local": {Type: "file", Path: secretsPath},
		},
	}, secretstore.ResolverOptions{})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	var audit secretstore.AuditReport
	values, err := buildValues(context.Background(), cli.New(), nil, []string{"db.password=secret://local/db/password"}, nil, nil, &SecretOptions{
		Resolver: resolver,
		AuditSink: func(report secretstore.AuditReport) {
			audit = report
		},
	})
	if err != nil {
		t.Fatalf("build values: %v", err)
	}
	section, ok := values["db"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected db map, got %T", values["db"])
	}
	if got := section["password"]; got != "s3cr3t" {
		t.Fatalf("password=%v, want s3cr3t", got)
	}
	if audit.Empty() {
		t.Fatalf("expected audit entries")
	}
}

func TestBuildValuesErrorsWithoutResolver(t *testing.T) {
	_, err := buildValues(context.Background(), cli.New(), nil, []string{"db.password=secret://local/db/password"}, nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "secret reference") {
		t.Fatalf("unexpected error: %v", err)
	}
}
