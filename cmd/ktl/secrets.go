package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/kubekattle/ktl/internal/kube"
	"github.com/kubekattle/ktl/internal/secretstore"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newSecretsCommand(kubeconfig, kubeContext *string) *cobra.Command {
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
	cmd.AddCommand(newSecretsDiscoverCommand())
	cmd.AddCommand(newSecretsExecCommand(kubeconfig, kubeContext))
	decorateCommandHelp(cmd, "Secrets Flags")
	return cmd
}

func newSecretsExecCommand(kubeconfig, kubeContext *string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "exec [SECRET_NAME] -- [COMMAND]...",
		Short: "Execute a command with secrets injected as environment variables",
		Long: `Injects all keys from a Kubernetes Secret as environment variables into a local command.
Keys are automatically capitalized and sanitized (e.g. "db.password" -> "DB_PASSWORD").

Example:
  ktl secrets exec my-db-secret -- ./start-app`,
		Args: cobra.MinimumNArgs(2), // secret + command
		RunE: func(cmd *cobra.Command, args []string) error {
			secretName := args[0]
			// Find "--"
			dashIdx := cmd.ArgsLenAtDash()
			if dashIdx == -1 || dashIdx == len(args) {
				return fmt.Errorf("command must be separated by --")
			}
			execArgs := args[dashIdx:]

			return runSecretsExec(cmd.Context(), kubeconfig, kubeContext, namespace, secretName, execArgs)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	return cmd
}

func runSecretsExec(ctx context.Context, kubeconfig, kubeContext *string, namespace string, secretName string, execArgs []string) error {
	kClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
	if err != nil {
		return err
	}
	if namespace == "" {
		namespace = kClient.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	secret, err := kClient.Clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	env := os.Environ()
	for k, v := range secret.Data {
		// Sanitize Key
		key := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(k, ".", "_"), "-", "_"))
		env = append(env, fmt.Sprintf("%s=%s", key, string(v)))
	}

	fmt.Printf("Injecting %d secrets from %s...\n", len(secret.Data), secretName)

	cmd := exec.CommandContext(ctx, execArgs[0], execArgs[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
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
	decorateCommandHelp(cmd, "Test Flags")
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
	decorateCommandHelp(cmd, "List Flags")
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
