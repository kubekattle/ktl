package secretstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		name            string
		value           string
		defaultProvider string
		wantProvider    string
		wantPath        string
		wantErr         bool
	}{
		{
			name:         "explicit provider",
			value:        "secret://vault/app/db",
			wantProvider: "vault",
			wantPath:     "app/db",
		},
		{
			name:            "default provider",
			value:           "secret:///app/db",
			defaultProvider: "local",
			wantProvider:    "local",
			wantPath:        "app/db",
		},
		{
			name:            "default provider without slash",
			value:           "secret://password",
			defaultProvider: "local",
			wantProvider:    "local",
			wantPath:        "password",
		},
		{
			name:    "missing provider",
			value:   "secret://password",
			wantErr: true,
		},
		{
			name:    "missing path",
			value:   "secret://vault/",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, ok, err := ParseRef(tc.value, tc.defaultProvider)
			if !ok {
				t.Fatalf("expected reference to be detected")
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Provider != tc.wantProvider {
				t.Fatalf("provider=%q, want %q", ref.Provider, tc.wantProvider)
			}
			if ref.Path != tc.wantPath {
				t.Fatalf("path=%q, want %q", ref.Path, tc.wantPath)
			}
		})
	}
}

func TestResolverResolveValues(t *testing.T) {
	tempDir := t.TempDir()
	secretsPath := filepath.Join(tempDir, "secrets.yaml")
	payload := "db:\n  password: s3cr3t\napi:\n  token: t0k3n\n"
	if err := os.WriteFile(secretsPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}

	resolver, err := NewResolver(Config{
		Providers: map[string]ProviderConfig{
			"local": {Type: "file", Path: secretsPath},
		},
	}, ResolverOptions{Mode: ResolveModeValue})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	values := map[string]interface{}{
		"db": map[string]interface{}{
			"password": "secret://local/db/password",
		},
		"token": "secret://local/api/token",
	}
	if err := resolver.ResolveValues(context.Background(), values); err != nil {
		t.Fatalf("resolve values: %v", err)
	}
	db := values["db"].(map[string]interface{})
	if got := db["password"]; got != "s3cr3t" {
		t.Fatalf("password=%v, want s3cr3t", got)
	}
	if got := values["token"]; got != "t0k3n" {
		t.Fatalf("token=%v, want t0k3n", got)
	}
	report := resolver.Audit()
	if report.Empty() {
		t.Fatalf("expected audit entries")
	}
}

func TestResolverMaskMode(t *testing.T) {
	tempDir := t.TempDir()
	secretsPath := filepath.Join(tempDir, "secrets.yaml")
	payload := "db:\n  password: s3cr3t\n"
	if err := os.WriteFile(secretsPath, []byte(payload), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}

	resolver, err := NewResolver(Config{
		Providers: map[string]ProviderConfig{
			"local": {Type: "file", Path: secretsPath},
		},
	}, ResolverOptions{Mode: ResolveModeMask})
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}
	values := map[string]interface{}{
		"password": "secret://local/db/password",
	}
	if err := resolver.ResolveValues(context.Background(), values); err != nil {
		t.Fatalf("resolve values: %v", err)
	}
	if got := values["password"]; got == "s3cr3t" {
		t.Fatalf("expected masked value, got real secret")
	}
	report := resolver.Audit()
	if report.Empty() {
		t.Fatalf("expected audit entries")
	}
	if !report.Entries[0].Masked {
		t.Fatalf("expected masked audit entry")
	}
}
