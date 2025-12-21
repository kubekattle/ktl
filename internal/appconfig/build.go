package appconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type BuildConfig struct {
	Profile       string `yaml:"profile,omitempty"`
	CacheDir      string `yaml:"cacheDir,omitempty"`
	AttestDir     string `yaml:"attestDir,omitempty"`
	Policy        string `yaml:"policy,omitempty"`
	PolicyMode    string `yaml:"policyMode,omitempty"`
	SecretsMode   string `yaml:"secretsMode,omitempty"`
	SecretsConfig string `yaml:"secretsConfig,omitempty"`
	Hermetic      *bool  `yaml:"hermetic,omitempty"`
	Sandbox       *bool  `yaml:"sandbox,omitempty"`
	SandboxConfig string `yaml:"sandboxConfig,omitempty"`
	Push          *bool  `yaml:"push,omitempty"`
	Load          *bool  `yaml:"load,omitempty"`
	RemoteBuild   string `yaml:"remoteBuild,omitempty"`
}

type Config struct {
	Build BuildConfig `yaml:"build,omitempty"`
}

func DefaultGlobalPath() string {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".ktl", "config.yaml")
}

func DefaultRepoPath(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return ""
	}
	return filepath.Join(repoRoot, ".ktl.yaml")
}

func Load(ctx context.Context, globalPath, repoPath string) (Config, error) {
	_ = ctx
	cfg := Config{}
	if strings.TrimSpace(globalPath) != "" {
		if c, err := loadOne(globalPath); err != nil {
			return Config{}, fmt.Errorf("load global config: %w", err)
		} else {
			cfg = merge(cfg, c)
		}
	}
	if strings.TrimSpace(repoPath) != "" {
		if c, err := loadOne(repoPath); err != nil {
			return Config{}, fmt.Errorf("load repo config: %w", err)
		} else {
			cfg = merge(cfg, c)
		}
	}
	return cfg, nil
}

func loadOne(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return Config{}, nil
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func merge(a, b Config) Config {
	out := a
	out.Build = mergeBuild(a.Build, b.Build)
	return out
}

func mergeBuild(a, b BuildConfig) BuildConfig {
	out := a
	if b.Profile != "" {
		out.Profile = b.Profile
	}
	if b.CacheDir != "" {
		out.CacheDir = b.CacheDir
	}
	if b.AttestDir != "" {
		out.AttestDir = b.AttestDir
	}
	if b.Policy != "" {
		out.Policy = b.Policy
	}
	if b.PolicyMode != "" {
		out.PolicyMode = b.PolicyMode
	}
	if b.SecretsMode != "" {
		out.SecretsMode = b.SecretsMode
	}
	if b.SecretsConfig != "" {
		out.SecretsConfig = b.SecretsConfig
	}
	if b.Hermetic != nil {
		out.Hermetic = b.Hermetic
	}
	if b.Sandbox != nil {
		out.Sandbox = b.Sandbox
	}
	if b.SandboxConfig != "" {
		out.SandboxConfig = b.SandboxConfig
	}
	if b.Push != nil {
		out.Push = b.Push
	}
	if b.Load != nil {
		out.Load = b.Load
	}
	if b.RemoteBuild != "" {
		out.RemoteBuild = b.RemoteBuild
	}
	return out
}
