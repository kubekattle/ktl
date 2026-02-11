// File: internal/workflows/buildsvc/sandbox_common.go
// Brief: Internal buildsvc package implementation for 'sandbox common'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"context"
	"os"
)

const (
	sandboxActiveEnvKey        = "KTL_SANDBOX_ACTIVE"
	legacySandboxActiveEnvKey  = "KTL_NSJAIL_ACTIVE"
	sandboxLogPathEnvKey       = "KTL_SANDBOX_LOG_PATH"
	legacySandboxLogPathEnv    = "KTL_NSJAIL_LOG_PATH"
	sandboxDisableEnvKey       = "KTL_SANDBOX_DISABLE"
	legacySandboxDisableEnv    = "KTL_NSJAIL_DISABLE"
	sandboxContextEnvKey       = "KTL_SANDBOX_CONTEXT"
	legacySandboxContextEnvKey = "KTL_NSJAIL_CONTEXT"
	sandboxCacheEnvKey         = "KTL_SANDBOX_CACHE"
	legacySandboxCacheEnvKey   = "KTL_NSJAIL_CACHE"
	sandboxBuilderEnvKey       = "KTL_SANDBOX_BUILDER"
	legacySandboxBuilderEnvKey = "KTL_NSJAIL_BUILDER"
)

type sandboxInjector func(ctx context.Context, opts *Options, streams Streams, contextAbs string) (bool, error)

func sandboxActive() bool {
	return os.Getenv(sandboxActiveEnvKey) == "1" || os.Getenv(legacySandboxActiveEnvKey) == "1"
}

func sandboxLogPathFromEnv() string {
	if v := os.Getenv(sandboxLogPathEnvKey); v != "" {
		return v
	}
	return os.Getenv(legacySandboxLogPathEnv)
}

func sandboxContextFromEnv() string {
	if v := os.Getenv(sandboxContextEnvKey); v != "" {
		return v
	}
	return os.Getenv(legacySandboxContextEnvKey)
}

func sandboxCacheFromEnv() string {
	if v := os.Getenv(sandboxCacheEnvKey); v != "" {
		return v
	}
	return os.Getenv(legacySandboxCacheEnvKey)
}
