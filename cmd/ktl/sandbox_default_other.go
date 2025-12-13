//go:build !linux

package main

import "fmt"

func ensureDefaultSandboxConfig() (string, error) {
	return "", fmt.Errorf("sandboxing requires linux")
}
