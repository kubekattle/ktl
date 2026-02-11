package secretstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type vaultKV2Response struct {
	Data struct {
		Data map[string]interface{} `json:"data"`
	} `json:"data"`
}

type vaultKV1Response struct {
	Data map[string]interface{} `json:"data"`
}

func TestVaultProviderKV2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/app/db" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := vaultKV2Response{}
		payload.Data.Data = map[string]interface{}{"password": "s3cr3t"}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	provider, err := newVaultProvider(ProviderConfig{
		Type:      "vault",
		Address:   server.URL,
		Token:     "token",
		Mount:     "secret",
		KVVersion: 2,
	})
	if err != nil {
		t.Fatalf("newVaultProvider: %v", err)
	}
	val, err := provider.Resolve(context.Background(), "app/db#password")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "s3cr3t" {
		t.Fatalf("value=%q, want s3cr3t", val)
	}
}

func TestVaultProviderKV1DefaultKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/app/db" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := vaultKV1Response{}
		payload.Data = map[string]interface{}{"value": "ok"}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	provider, err := newVaultProvider(ProviderConfig{
		Type:      "vault",
		Address:   server.URL,
		Token:     "token",
		Mount:     "secret",
		KVVersion: 1,
	})
	if err != nil {
		t.Fatalf("newVaultProvider: %v", err)
	}
	val, err := provider.Resolve(context.Background(), "app/db")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if val != "ok" {
		t.Fatalf("value=%q, want ok", val)
	}
}

func TestVaultProviderRequiresKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := vaultKV2Response{}
		payload.Data.Data = map[string]interface{}{"a": "1", "b": "2"}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	provider, err := newVaultProvider(ProviderConfig{
		Type:      "vault",
		Address:   server.URL,
		Token:     "token",
		Mount:     "secret",
		KVVersion: 2,
	})
	if err != nil {
		t.Fatalf("newVaultProvider: %v", err)
	}
	_, err = provider.Resolve(context.Background(), "app/db")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestVaultAuthConfigAppRoleValidation(t *testing.T) {
	_, err := buildVaultAuthConfig(ProviderConfig{
		AuthMethod: "approle",
		RoleID:     "role",
	})
	if err == nil {
		t.Fatalf("expected error for missing secretId")
	}
}

func TestVaultAuthConfigKubernetesDefaultsTokenPath(t *testing.T) {
	cfg, err := buildVaultAuthConfig(ProviderConfig{
		AuthMethod:     "kubernetes",
		KubernetesRole: "default",
	})
	if err != nil {
		t.Fatalf("expected config, got error: %v", err)
	}
	if cfg.kubernetesTokenPath == "" {
		t.Fatalf("expected default kubernetes token path")
	}
}

func TestVaultAuthConfigAWSRequiresRole(t *testing.T) {
	_, err := buildVaultAuthConfig(ProviderConfig{
		AuthMethod: "aws",
	})
	if err == nil {
		t.Fatalf("expected error for missing awsRole")
	}
}
