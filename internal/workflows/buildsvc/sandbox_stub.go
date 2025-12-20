//go:build !linux

// File: internal/workflows/buildsvc/sandbox_stub.go
// Brief: Internal buildsvc package implementation for 'sandbox stub'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

func getSandboxInjector() sandboxInjector {
	return nil
}
