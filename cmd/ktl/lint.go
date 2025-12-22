// File: cmd/ktl/lint.go
// Brief: CLI command wiring and implementation for 'lint'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
			argsProvided := len(args) > 0
			paths := []string{"."}
			if argsProvided {
				paths = append([]string(nil), args...)
			} else {
				defaultPaths, err := resolveDefaultLintPaths(cmd)
				if err != nil {
					return err
				}
				paths = defaultPaths
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

			if err := validateLintPaths(cmd, paths, argsProvided); err != nil {
				return err
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

func resolveDefaultLintPaths(cmd *cobra.Command) ([]string, error) {
	// Preserve the documented behavior: if the current directory looks like a chart, lint it.
	ok, err := isChartDir(".")
	if err != nil {
		return nil, err
	}
	if ok {
		return []string{"."}, nil
	}

	// Common layout: repo root contains a ./chart directory.
	ok, err = isChartDir("chart")
	if err != nil {
		return nil, err
	}
	if ok {
		return []string{"./chart"}, nil
	}

	// Otherwise, provide a helpful error with suggestions.
	candidates, err := findChartDirs(".", 4)
	if err != nil {
		return nil, err
	}
	return nil, formatNoChartError(cmd, ".", candidates)
}

func validateLintPaths(cmd *cobra.Command, paths []string, argsProvided bool) error {
	// When a user explicitly passes paths, fail fast with a clear error instead of
	// delegating to Helm's "stat Chart.yaml" message.
	if !argsProvided {
		return nil
	}

	var invalid []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			invalid = append(invalid, p)
			continue
		}
		if strings.HasSuffix(p, ".tgz") || strings.HasSuffix(p, ".tar.gz") {
			if _, err := os.Stat(p); err != nil {
				invalid = append(invalid, p)
			}
			continue
		}
		ok, err := isChartDir(p)
		if err != nil {
			return err
		}
		if !ok {
			invalid = append(invalid, p)
		}
	}

	if len(invalid) == 0 {
		return nil
	}

	candidates, _ := findChartDirs(".", 4)
	return formatNoChartError(cmd, strings.Join(invalid, ", "), candidates)
}

func isChartDir(path string) (bool, error) {
	stat, err := os.Stat(filepath.Join(path, "Chart.yaml"))
	if err == nil {
		return !stat.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func findChartDirs(root string, maxDepth int) ([]string, error) {
	root = filepath.Clean(root)
	var found []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d == nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "Chart.yaml" {
			found = append(found, filepath.Dir(path))
			return nil
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			if rel == "." {
				return nil
			}
			depth := strings.Count(rel, string(filepath.Separator)) + 1
			if depth > maxDepth {
				return filepath.SkipDir
			}
			switch filepath.Base(path) {
			case ".git", "bin", "dist", "vendor":
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(found)
	return found, nil
}

func formatNoChartError(cmd *cobra.Command, path string, candidates []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "unable to lint: %s does not look like a Helm chart (missing Chart.yaml)\n\n", path)
	b.WriteString("Pass a chart directory or a packaged chart:\n")
	b.WriteString("  ktl lint ./path/to/chart\n")
	b.WriteString("  ktl lint ./path/to/chart.tgz\n")

	if len(candidates) > 0 {
		limit := 5
		if len(candidates) < limit {
			limit = len(candidates)
		}
		b.WriteString("\nCharts found under the current directory:\n")
		for _, c := range candidates[:limit] {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
		if len(candidates) > limit {
			fmt.Fprintf(&b, "  (and %d more)\n", len(candidates)-limit)
		}
	}

	_ = cmd // reserved for future contextual hints without forcing Cobra usage output.
	return errors.New(strings.TrimRight(b.String(), "\n"))
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
