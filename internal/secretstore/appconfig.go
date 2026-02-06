package secretstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/appconfig"
)

// ConfigFromApp maps appconfig secrets to secretstore config.
func ConfigFromApp(cfg appconfig.SecretsConfig) Config {
	providers := make(map[string]ProviderConfig, len(cfg.Providers))
	for name, provider := range cfg.Providers {
		providers[name] = ProviderConfig{
			Type:                provider.Type,
			Path:                provider.Path,
			Address:             provider.Address,
			Token:               provider.Token,
			Namespace:           provider.Namespace,
			Mount:               provider.Mount,
			KVVersion:           provider.KVVersion,
			Key:                 provider.Key,
			AuthMethod:          provider.AuthMethod,
			AuthMount:           provider.AuthMount,
			RoleID:              provider.RoleID,
			SecretID:            provider.SecretID,
			KubernetesRole:      provider.KubernetesRole,
			KubernetesToken:     provider.KubernetesToken,
			KubernetesTokenPath: provider.KubernetesTokenPath,
			AWSRole:             provider.AWSRole,
			AWSRegion:           provider.AWSRegion,
			AWSHeaderValue:      provider.AWSHeaderValue,
		}
	}
	return Config{
		DefaultProvider: cfg.DefaultProvider,
		Providers:       providers,
	}
}

// LoadConfigFromApp loads secret providers from an explicit config path or the default app config.
func LoadConfigFromApp(ctx context.Context, chartPath string, explicitPath string) (Config, string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		path := explicitPath
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		cfg, err := LoadConfig(path)
		if err != nil {
			return Config{}, "", err
		}
		return cfg, filepath.Dir(path), nil
	}
	repoRoot := appconfig.FindRepoRoot(chartPath)
	cfg, err := appconfig.Load(ctx, appconfig.DefaultGlobalPath(), appconfig.DefaultRepoPath(repoRoot))
	if err != nil {
		return Config{}, "", err
	}
	baseDir := repoRoot
	if baseDir == "" {
		if wd, err := os.Getwd(); err == nil {
			baseDir = wd
		}
	}
	return ConfigFromApp(cfg.Secrets), baseDir, nil
}
