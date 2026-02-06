package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/example/ktl/internal/secretstore"
	"github.com/spf13/cobra"
)

func newSecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Inspect and validate deploy-time secret providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSecretsTestCommand())
	cmd.AddCommand(newSecretsListCommand())
	return cmd
}

func newSecretsTestCommand() *cobra.Command {
	var secretProvider string
	var secretConfig string
	var ref string
	var path string
	var key string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Validate access to a secret reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			resolver, _, err := buildDeploySecretResolver(ctx, deploySecretConfig{
				Chart:      ".",
				ConfigPath: secretConfig,
				Provider:   secretProvider,
				Mode:       secretstore.ResolveModeValue,
				ErrOut:     cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(ref) == "" {
				if strings.TrimSpace(secretProvider) == "" {
					return fmt.Errorf("--secret-provider or --ref is required")
				}
				if strings.TrimSpace(path) == "" {
					return fmt.Errorf("--path or --ref is required")
				}
				if strings.TrimSpace(key) != "" {
					ref = fmt.Sprintf("secret://%s/%s#%s", strings.TrimSpace(secretProvider), strings.Trim(strings.TrimSpace(path), "/"), strings.TrimSpace(key))
				} else {
					ref = fmt.Sprintf("secret://%s/%s", strings.TrimSpace(secretProvider), strings.Trim(strings.TrimSpace(path), "/"))
				}
			}
			value, ok, err := resolver.ResolveString(ctx, ref)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("reference %q is not a secret:// reference", ref)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Resolved %s (len=%d)\n", ref, len(value))
			return nil
		},
	}
	cmd.Flags().StringVar(&secretProvider, "secret-provider", "", "Secret provider name for secret:// references")
	cmd.Flags().StringVar(&secretConfig, "secret-config", "", "Secrets provider config file (defaults to ~/.ktl/config.yaml and repo .ktl.yaml)")
	cmd.Flags().StringVar(&ref, "ref", "", "Secret reference (secret://provider/path#key)")
	cmd.Flags().StringVar(&path, "path", "", "Secret path (when not using --ref)")
	cmd.Flags().StringVar(&key, "key", "", "Secret key (optional when not using --ref)")
	return cmd
}

func newSecretsListCommand() *cobra.Command {
	var secretProvider string
	var secretConfig string
	var path string
	var format string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List secrets under a provider/path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if strings.TrimSpace(secretProvider) == "" {
				return fmt.Errorf("--secret-provider is required")
			}
			resolver, _, err := buildDeploySecretResolver(ctx, deploySecretConfig{
				Chart:      ".",
				ConfigPath: secretConfig,
				Provider:   secretProvider,
				Mode:       secretstore.ResolveModeValue,
				ErrOut:     cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			provider, ok := resolver.Provider(strings.TrimSpace(secretProvider))
			if !ok {
				return fmt.Errorf("secret provider %q is not configured", secretProvider)
			}
			lister, ok := provider.(secretstore.Lister)
			if !ok {
				return fmt.Errorf("secret provider %q does not support listing", secretProvider)
			}
			items, err := lister.List(ctx, path)
			if err != nil {
				return err
			}
			sort.Strings(items)
			return renderSecretList(cmd.OutOrStdout(), strings.TrimSpace(format), items)
		},
	}
	cmd.Flags().StringVar(&secretProvider, "secret-provider", "", "Secret provider name")
	cmd.Flags().StringVar(&secretConfig, "secret-config", "", "Secrets provider config file (defaults to ~/.ktl/config.yaml and repo .ktl.yaml)")
	cmd.Flags().StringVar(&path, "path", "", "Secret path to list (default: provider root)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	return cmd
}

func renderSecretList(out io.Writer, format string, items []string) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	switch format {
	case "text":
		for _, item := range items {
			fmt.Fprintln(out, item)
		}
		return nil
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}
