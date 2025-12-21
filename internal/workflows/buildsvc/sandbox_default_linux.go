//go:build linux

// File: internal/workflows/buildsvc/sandbox_default_linux.go
// Brief: Internal buildsvc package implementation for 'sandbox default linux'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	_ "embed"
)

const defaultSandboxConfigName = "sandbox-default.cfg"
const hermeticSandboxConfigName = "sandbox-hermetic.cfg"

//go:embed sandbox/ktl-default.cfg
var embeddedSandboxConfig []byte

//go:embed sandbox/ktl-hermetic.cfg
var embeddedHermeticSandboxConfig []byte

func ensureDefaultSandboxConfig() (string, error) {
	return ensureEmbeddedSandboxConfig(defaultSandboxConfigName, embeddedSandboxConfig)
}

func ensureHermeticSandboxConfig() (string, error) {
	return ensureEmbeddedSandboxConfig(hermeticSandboxConfigName, embeddedHermeticSandboxConfig)
}

func ensureEmbeddedSandboxConfig(name string, payload []byte) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "ktl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, payload) {
			return path, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
