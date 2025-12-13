//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	_ "embed"
)

const defaultSandboxConfigName = "sandbox-default.cfg"

//go:embed sandbox/ktl-default.cfg
var embeddedSandboxConfig []byte

func ensureDefaultSandboxConfig() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "ktl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, defaultSandboxConfigName)
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, embeddedSandboxConfig) {
			return path, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(path, embeddedSandboxConfig, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
