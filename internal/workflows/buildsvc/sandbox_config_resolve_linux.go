//go:build linux

package buildsvc

import (
	"fmt"
	"os"
	"strings"
)

// ResolveSandboxConfigPath returns the nsjail config path that ktl build would use.
func ResolveSandboxConfigPath(explicit string, hermetic bool, allowNetwork bool) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("sandbox config: %w", err)
		}
		return explicit, nil
	}

	path := ""
	var err error
	if hermetic && !allowNetwork {
		path, err = ensureHermeticSandboxConfig()
	} else {
		path, err = ensureDefaultSandboxConfig()
	}
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("sandbox config: %w", err)
	}
	return path, nil
}
