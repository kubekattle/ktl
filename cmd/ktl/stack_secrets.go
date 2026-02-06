package main

import (
	"context"
	"io"
	"strings"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/secretstore"
)

func buildStackSecretOptions(ctx context.Context, root string, secretProvider string, secretConfig string, errOut io.Writer) (*deploy.SecretOptions, error) {
	root = strings.TrimSpace(root)
	secretProvider = strings.TrimSpace(secretProvider)
	secretConfig = strings.TrimSpace(secretConfig)
	resolver, auditSink, err := buildDeploySecretResolver(ctx, deploySecretConfig{
		Chart:      root,
		ConfigPath: secretConfig,
		Provider:   secretProvider,
		Mode:       secretstore.ResolveModeValue,
		ErrOut:     errOut,
	})
	if err != nil {
		return nil, err
	}
	return &deploy.SecretOptions{Resolver: resolver, AuditSink: auditSink}, nil
}
