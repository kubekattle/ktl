package secretstore

import (
	"context"
	"fmt"
	"strings"
	"sync"

	vault "github.com/hashicorp/vault/api"
)

type vaultProvider struct {
	client    *vault.Client
	mount     string
	kvVersion int
	key       string
	auth      vaultAuthConfig
	authOnce  sync.Once
	authErr   error
}

func newVaultProvider(cfg ProviderConfig) (*vaultProvider, error) {
	address := strings.TrimSpace(cfg.Address)
	if address == "" {
		return nil, fmt.Errorf("vault address is required")
	}
	authCfg, err := buildVaultAuthConfig(cfg)
	if err != nil {
		return nil, err
	}

	apiCfg := vault.DefaultConfig()
	apiCfg.Address = address
	client, err := vault.NewClient(apiCfg)
	if err != nil {
		return nil, err
	}
	if ns := strings.TrimSpace(cfg.Namespace); ns != "" {
		client.SetNamespace(ns)
	}
	if authCfg.method == vaultAuthToken {
		client.SetToken(authCfg.token)
	}

	mount := strings.Trim(strings.TrimSpace(cfg.Mount), "/")
	if mount == "" {
		mount = "secret"
	}
	kvVersion := cfg.KVVersion
	if kvVersion == 0 {
		kvVersion = 2
	}
	if kvVersion != 1 && kvVersion != 2 {
		return nil, fmt.Errorf("vault kvVersion must be 1 or 2")
	}
	return &vaultProvider{
		client:    client,
		mount:     mount,
		kvVersion: kvVersion,
		key:       strings.TrimSpace(cfg.Key),
		auth:      authCfg,
	}, nil
}

func (p *vaultProvider) Resolve(ctx context.Context, secretPath string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("vault provider is not initialized")
	}
	path, key := splitVaultPath(secretPath)
	if path == "" {
		return "", fmt.Errorf("vault secret path is required")
	}
	if err := p.ensureAuth(ctx); err != nil {
		return "", err
	}
	data, err := p.read(ctx, path)
	if err != nil {
		return "", err
	}
	if key == "" {
		key = p.key
	}
	return selectSecretValue(data, key, "value")
}

func (p *vaultProvider) List(ctx context.Context, secretPath string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("vault provider is not initialized")
	}
	if err := p.ensureAuth(ctx); err != nil {
		return nil, err
	}
	path, _ := splitVaultPath(secretPath)
	path = strings.Trim(strings.TrimSpace(path), "/")
	var listPath string
	switch p.kvVersion {
	case 1:
		if path == "" {
			listPath = p.mount
		} else {
			listPath = fmt.Sprintf("%s/%s", p.mount, path)
		}
	case 2:
		if path == "" {
			listPath = fmt.Sprintf("%s/metadata", p.mount)
		} else {
			listPath = fmt.Sprintf("%s/metadata/%s", p.mount, path)
		}
	default:
		return nil, fmt.Errorf("vault kvVersion must be 1 or 2")
	}
	secret, err := p.client.Logical().ListWithContext(ctx, listPath)
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("vault secret path %q not found", secretPath)
	}
	rawKeys, ok := secret.Data["keys"]
	if !ok {
		return nil, fmt.Errorf("vault list response missing keys")
	}
	return coerceStringList(rawKeys)
}

func (p *vaultProvider) read(ctx context.Context, path string) (map[string]interface{}, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return nil, fmt.Errorf("vault secret path is required")
	}
	switch p.kvVersion {
	case 1:
		secret, err := p.client.Logical().ReadWithContext(ctx, fmt.Sprintf("%s/%s", p.mount, path))
		if err != nil {
			return nil, err
		}
		if secret == nil || secret.Data == nil {
			return nil, fmt.Errorf("vault secret not found")
		}
		return secret.Data, nil
	case 2:
		secret, err := p.client.KVv2(p.mount).Get(ctx, path)
		if err != nil {
			return nil, err
		}
		if secret == nil || secret.Data == nil {
			return nil, fmt.Errorf("vault secret not found")
		}
		return secret.Data, nil
	default:
		return nil, fmt.Errorf("vault kvVersion must be 1 or 2")
	}
}

func splitVaultPath(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	parts := strings.SplitN(raw, "#", 2)
	path := strings.Trim(strings.TrimSpace(parts[0]), "/")
	key := ""
	if len(parts) > 1 {
		key = strings.TrimSpace(parts[1])
	}
	return path, key
}

func selectSecretValue(data map[string]interface{}, key string, fallback string) (string, error) {
	if data == nil {
		return "", fmt.Errorf("secret data is empty")
	}
	candidates := []string{}
	if key != "" {
		candidates = append(candidates, key)
	}
	if fallback != "" {
		candidates = append(candidates, fallback)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if val, ok := data[candidate]; ok {
			return coerceStringValue(val)
		}
	}
	if len(data) == 1 {
		for _, val := range data {
			return coerceStringValue(val)
		}
	}
	if key == "" {
		return "", fmt.Errorf("secret value is ambiguous; specify a key")
	}
	return "", fmt.Errorf("secret key %q not found", key)
}

func coerceStringValue(val interface{}) (string, error) {
	switch typed := val.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("secret value must be a string")
	}
}
