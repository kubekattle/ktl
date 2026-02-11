//go:build !linux

package buildsvc

import "fmt"

func ResolveSandboxConfigPath(explicit string, hermetic bool, allowNetwork bool) (string, error) {
	_ = explicit
	_ = hermetic
	_ = allowNetwork
	return "", fmt.Errorf("sandbox config resolution is only supported on Linux")
}
