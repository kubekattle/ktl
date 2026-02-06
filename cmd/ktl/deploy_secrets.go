package main

import (
	"context"
	"fmt"
	"io"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/secretstore"
)

type deploySecretConfig struct {
	Chart      string
	ConfigPath string
	Provider   string
	Mode       secretstore.ResolveMode
	ErrOut     io.Writer
}

func buildDeploySecretResolver(ctx context.Context, cfg deploySecretConfig) (*secretstore.Resolver, func(secretstore.AuditReport), error) {
	secretsCfg, baseDir, err := secretstore.LoadConfigFromApp(ctx, cfg.Chart, cfg.ConfigPath)
	if err != nil {
		return nil, nil, err
	}
	resolver, err := secretstore.NewResolver(secretsCfg, secretstore.ResolverOptions{
		DefaultProvider: cfg.Provider,
		Mode:            cfg.Mode,
		BaseDir:         baseDir,
	})
	if err != nil {
		return nil, nil, err
	}
	auditSink := newSecretAuditLogger(cfg.ErrOut, cfg.Mode)
	return resolver, auditSink, nil
}

func newSecretAuditLogger(out io.Writer, mode secretstore.ResolveMode) func(secretstore.AuditReport) {
	if out == nil {
		return nil
	}
	seen := map[string]struct{}{}
	return func(report secretstore.AuditReport) {
		if report.Empty() {
			return
		}
		entries := make([]secretstore.AuditEntry, 0, len(report.Entries))
		for _, entry := range report.Entries {
			if entry.Reference == "" {
				continue
			}
			if _, ok := seen[entry.Reference]; ok {
				continue
			}
			seen[entry.Reference] = struct{}{}
			entries = append(entries, entry)
		}
		if len(entries) == 0 {
			return
		}
		suffix := "resolved"
		if mode == secretstore.ResolveModeMask {
			suffix = "resolved (masked for plan)"
		}
		fmt.Fprintf(out, "Secrets: %d reference(s) %s.\n", len(entries), suffix)
		for _, entry := range entries {
			fmt.Fprintf(out, "  - %s\n", entry.Reference)
		}
	}
}

func secretRefsFromAudit(report secretstore.AuditReport) []deploy.SecretRef {
	if report.Empty() {
		return nil
	}
	out := make([]deploy.SecretRef, 0, len(report.Entries))
	for _, entry := range report.Entries {
		if entry.Provider == "" && entry.Path == "" && entry.Reference == "" {
			continue
		}
		out = append(out, deploy.SecretRef{
			Provider:  entry.Provider,
			Path:      entry.Path,
			Reference: entry.Reference,
			Masked:    entry.Masked,
		})
	}
	return out
}

func cloneSecretRefs(in []deploy.SecretRef) []deploy.SecretRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]deploy.SecretRef, len(in))
	copy(out, in)
	return out
}
