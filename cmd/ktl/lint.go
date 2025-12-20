// File: cmd/ktl/lint.go
// Brief: CLI command wiring and implementation for 'lint'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/kube"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	cliValues "helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/lint/support"
)

func newLintCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	client := action.NewLint()
	valueOpts := &cliValues.Options{}

	var namespace string
	var kubeVersion string

	cmd := &cobra.Command{
		Use:   "lint [CHART]...",
		Short: "Examine a chart for possible issues",
		Args:  cobra.ArbitraryArgs,
		Example: `  # Lint the current chart directory
  ktl lint

  # Lint a specific chart with values
  ktl lint ./chart -f values/prod.yaml --strict`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := []string{"."}
			if len(args) > 0 {
				paths = append([]string(nil), args...)
			}

			resolvedNamespace := strings.TrimSpace(namespace)
			if resolvedNamespace == "" {
				ns, err := resolveNamespaceFallback(cmd.Context(), kubeconfig, kubeContext)
				if err != nil {
					return err
				}
				resolvedNamespace = ns
			}
			client.Namespace = resolvedNamespace

			envSettings := cli.New()
			if kubeconfig != nil && strings.TrimSpace(*kubeconfig) != "" {
				envSettings.KubeConfig = strings.TrimSpace(*kubeconfig)
			}
			if kubeContext != nil && strings.TrimSpace(*kubeContext) != "" {
				envSettings.KubeContext = strings.TrimSpace(*kubeContext)
			}
			if strings.TrimSpace(resolvedNamespace) != "" {
				envSettings.SetNamespace(resolvedNamespace)
			}

			if strings.TrimSpace(kubeVersion) != "" {
				parsedKubeVersion, err := chartutil.ParseKubeVersion(kubeVersion)
				if err != nil {
					return fmt.Errorf("invalid kube version %q: %w", kubeVersion, err)
				}
				client.KubeVersion = parsedKubeVersion
			}

			if client.WithSubcharts {
				for _, p := range paths {
					chartsDir := filepath.Join(p, "charts")
					if err := filepath.Walk(chartsDir, func(path string, info fs.FileInfo, walkErr error) error {
						if walkErr != nil || info == nil {
							return nil
						}
						if info.Name() == "Chart.yaml" {
							paths = append(paths, filepath.Dir(path))
							return nil
						}
						if strings.HasSuffix(path, ".tgz") || strings.HasSuffix(path, ".tar.gz") {
							paths = append(paths, path)
						}
						return nil
					}); err != nil && !errors.Is(err, fs.ErrNotExist) {
						return fmt.Errorf("scan subcharts under %s: %w", p, err)
					}
				}
			}

			vals, err := valueOpts.MergeValues(getter.All(envSettings))
			if err != nil {
				return err
			}

			var failed int
			var chartsWithWarningsOrErrors int
			out := cmd.OutOrStdout()

			for _, path := range paths {
				result := client.Run([]string{path}, vals)
				hasWarningsOrErrors := action.HasWarningsOrErrors(result)
				if hasWarningsOrErrors {
					chartsWithWarningsOrErrors++
				}
				if client.Quiet && !hasWarningsOrErrors {
					continue
				}

				fmt.Fprintf(out, "==> Linting %s\n", path)

				if len(result.Messages) == 0 {
					for _, err := range result.Errors {
						fmt.Fprintf(out, "Error %s\n", err)
					}
				}

				for _, msg := range result.Messages {
					if !client.Quiet || msg.Severity > support.InfoSev {
						fmt.Fprintf(out, "%s\n", msg)
					}
				}

				if len(result.Errors) != 0 {
					failed++
				}
				fmt.Fprintln(out)
			}

			summary := fmt.Sprintf("%d chart(s) linted, %d chart(s) failed", len(paths), failed)
			if failed > 0 {
				return errors.New(summary)
			}
			if !client.Quiet || chartsWithWarningsOrErrors > 0 {
				fmt.Fprintln(out, summary)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&namespace, "namespace", "n", "", "Namespace used for template rendering (default: current context)")
	f.BoolVar(&client.Strict, "strict", false, "Fail on lint warnings")
	f.BoolVar(&client.WithSubcharts, "with-subcharts", false, "Lint dependent charts under charts/")
	f.BoolVar(&client.Quiet, "quiet", false, "Print only warnings and errors")
	f.BoolVar(&client.SkipSchemaValidation, "skip-schema-validation", false, "Disable JSON schema validation")
	f.StringVar(&kubeVersion, "kube-version", "", "Kubernetes version used for capabilities and deprecation checks")

	f.StringSliceVarP(&valueOpts.ValueFiles, "values", "f", nil, "Values files to apply (can be repeated)")
	f.StringArrayVar(&valueOpts.Values, "set", nil, "Set values on the command line (key=val)")
	f.StringArrayVar(&valueOpts.StringValues, "set-string", nil, "Set STRING values on the command line")
	f.StringArrayVar(&valueOpts.FileValues, "set-file", nil, "Set values from files (key=path)")

	decorateCommandHelp(cmd, "Lint Flags")
	return cmd
}

func resolveNamespaceFallback(ctx context.Context, kubeconfig *string, kubeContext *string) (string, error) {
	kubeconfigPath := ""
	if kubeconfig != nil {
		kubeconfigPath = strings.TrimSpace(*kubeconfig)
	}
	contextName := ""
	if kubeContext != nil {
		contextName = strings.TrimSpace(*kubeContext)
	}

	client, err := kube.New(ctx, kubeconfigPath, contextName)
	if err != nil {
		if kubeconfigPath != "" || contextName != "" {
			return "", fmt.Errorf("resolve namespace from kubeconfig: %w", err)
		}
		return "default", nil
	}
	if ns := strings.TrimSpace(client.Namespace); ns != "" {
		return ns, nil
	}
	return "default", nil
}
