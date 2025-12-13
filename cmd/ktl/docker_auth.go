package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/credentials"
)

func loadDockerConfigFile(path string, stderr io.Writer) (*configfile.ConfigFile, error) {
	if path == "" {
		cfg := config.LoadDefaultConfigFile(stderr)
		if cfg == nil {
			return nil, errors.New("unable to load docker config")
		}
		return cfg, nil
	}
	cfg := configfile.New(path)
	if data, err := os.ReadFile(path); err == nil {
		if len(data) > 0 {
			if err := cfg.LoadFromReader(bytes.NewReader(data)); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if !cfg.ContainsAuth() {
		cfg.CredentialsStore = credentials.DetectDefaultStore(cfg.CredentialsStore)
	}
	return cfg, nil
}

func ensureDockerConfigDir(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o755)
}

func applyAuthfileEnv(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	return os.Setenv(config.EnvOverrideConfigDir, dir)
}
