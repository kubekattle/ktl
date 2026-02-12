package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kubekattle/ktl/internal/appconfig"
	"github.com/spf13/cobra"
)

func applyBuildDefaults(cmd *cobra.Command, opts *buildCLIOptions) error {
	if cmd == nil || opts == nil {
		return nil
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	contextDir := strings.TrimSpace(opts.contextDir)
	if contextDir == "" {
		contextDir = "."
	}
	absContextDir, err := filepath.Abs(contextDir)
	if err == nil {
		contextDir = absContextDir
	}
	repoRoot := appconfig.FindRepoRoot(contextDir)
	cfg, err := appconfig.Load(ctx, appconfig.DefaultGlobalPath(), appconfig.DefaultRepoPath(repoRoot))
	if err != nil {
		return err
	}

	if !rootFlagChanged(cmd, "profile") && strings.TrimSpace(cfg.Build.Profile) != "" {
		opts.profile = cfg.Build.Profile
	}

	applyBuildConfig(cmd, opts, cfg.Build)
	applyBuildProfile(cmd, opts)
	applyBuildIntents(cmd, opts)
	return nil
}

func applyBuildConfig(cmd *cobra.Command, opts *buildCLIOptions, cfg appconfig.BuildConfig) {
	if !flagChanged(cmd, "cache-dir") && strings.TrimSpace(cfg.CacheDir) != "" {
		opts.cacheDir = cfg.CacheDir
	}
	if !flagChanged(cmd, "attest-dir") && strings.TrimSpace(cfg.AttestDir) != "" {
		opts.attestDir = cfg.AttestDir
	}
	if !flagChanged(cmd, "policy") && strings.TrimSpace(cfg.Policy) != "" {
		opts.policyRef = cfg.Policy
	}
	if !flagChanged(cmd, "policy-mode") && strings.TrimSpace(cfg.PolicyMode) != "" {
		opts.policyMode = cfg.PolicyMode
	}
	if !flagChanged(cmd, "secrets-mode") && strings.TrimSpace(cfg.SecretsMode) != "" {
		opts.secretsMode = cfg.SecretsMode
	}
	if !flagChanged(cmd, "secrets-config") && strings.TrimSpace(cfg.SecretsConfig) != "" {
		opts.secretsConfig = cfg.SecretsConfig
	}
	if !flagChanged(cmd, "hermetic") && cfg.Hermetic != nil {
		opts.hermetic = *cfg.Hermetic
	}
	if !flagChanged(cmd, "sandbox") && cfg.Sandbox != nil {
		opts.sandboxRequired = *cfg.Sandbox
	}
	if !flagChanged(cmd, "sandbox-config") && strings.TrimSpace(cfg.SandboxConfig) != "" {
		opts.sandboxConfig = cfg.SandboxConfig
	}
	if !flagChanged(cmd, "push") && cfg.Push != nil {
		opts.push = *cfg.Push
	}
	if !flagChanged(cmd, "load") && cfg.Load != nil {
		opts.load = *cfg.Load
	}
	if !flagChanged(cmd, "remote-build") && strings.TrimSpace(cfg.RemoteBuild) != "" {
		opts.remoteAddr = cfg.RemoteBuild
	}
}

func applyBuildProfile(cmd *cobra.Command, opts *buildCLIOptions) {
	if cmd == nil || opts == nil {
		return
	}
	switch strings.TrimSpace(opts.profile) {
	case "", "dev":
		return
	case "ci":
		if !flagChanged(cmd, "push") && !opts.push {
			opts.push = true
		}
	case "secure":
		if !flagChanged(cmd, "hermetic") && !opts.hermetic {
			opts.hermetic = true
		}
		if !flagChanged(cmd, "sandbox") && !opts.sandboxRequired {
			opts.sandboxRequired = true
		}
		if !flagChanged(cmd, "sbom") && !opts.sbom {
			opts.sbom = true
		}
		if !flagChanged(cmd, "provenance") && !opts.provenance {
			opts.provenance = true
		}
		if !flagChanged(cmd, "attest-dir") && strings.TrimSpace(opts.attestDir) == "" {
			opts.attestDir = "dist/attest"
		}
		if !flagChanged(cmd, "policy-mode") && strings.TrimSpace(opts.policyMode) != "enforce" {
			opts.policyMode = "enforce"
		}
		if !flagChanged(cmd, "secrets-mode") && strings.TrimSpace(opts.secretsMode) != "enforce" {
			opts.secretsMode = "enforce"
		}
	case "remote":
		return
	default:
		fmt.Fprintf(cmd.ErrOrStderr(), "warn: unknown profile %q; proceeding with explicit flags\n", opts.profile)
	}
}

func applyBuildIntents(cmd *cobra.Command, opts *buildCLIOptions) {
	if cmd == nil || opts == nil {
		return
	}
	if opts.intentPublish && !flagChanged(cmd, "push") && !opts.push {
		opts.push = true
	}
	if opts.intentSecure {
		if !flagChanged(cmd, "hermetic") && !opts.hermetic {
			opts.hermetic = true
		}
		if !flagChanged(cmd, "sandbox") && !opts.sandboxRequired {
			opts.sandboxRequired = true
		}
		if !flagChanged(cmd, "sbom") && !opts.sbom {
			opts.sbom = true
		}
		if !flagChanged(cmd, "provenance") && !opts.provenance {
			opts.provenance = true
		}
		if !flagChanged(cmd, "attest-dir") && strings.TrimSpace(opts.attestDir) == "" {
			opts.attestDir = "dist/attest"
		}
		if !flagChanged(cmd, "policy-mode") && strings.TrimSpace(opts.policyMode) != "enforce" {
			opts.policyMode = "enforce"
		}
		if !flagChanged(cmd, "secrets-mode") && strings.TrimSpace(opts.secretsMode) != "enforce" {
			opts.secretsMode = "enforce"
		}
	}
	if opts.intentOCI && !flagChanged(cmd, "attest-dir") && strings.TrimSpace(opts.attestDir) == "" {
		opts.attestDir = "dist/attest"
	}
}

func flagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}
	if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
		return true
	}
	if f := cmd.PersistentFlags().Lookup(name); f != nil && f.Changed {
		return true
	}
	if f := cmd.InheritedFlags().Lookup(name); f != nil && f.Changed {
		return true
	}
	return false
}

func rootFlagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil || cmd.Root() == nil {
		return false
	}
	if f := cmd.Root().PersistentFlags().Lookup(name); f != nil && f.Changed {
		return true
	}
	return false
}
