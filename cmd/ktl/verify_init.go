package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func newVerifyInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a starter verify config YAML",
		Long: strings.TrimSpace(`
Generate a ready-to-run verify config YAML for common targets.

Examples:
  ktl verify init chart --chart ./chart --release foo -n default > verify.yaml
  ktl verify init namespace -n default --context my-context > verify.yaml
`),
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newVerifyInitChartCommand(), newVerifyInitNamespaceCommand())
	return cmd
}

func newVerifyInitChartCommand() *cobra.Command {
	var outPath string
	var force bool
	var chartPath string
	var release string
	var namespace string
	var valuesFiles []string
	var setValues []string
	var kubeContext string
	var useCluster bool
	var includeCRDs bool
	var mode string
	var failOn string
	var format string

	cmd := &cobra.Command{
		Use:   "chart",
		Short: "Generate a verify config for rendering a Helm chart",
		Example: strings.TrimSpace(`
  # Verify a chart render (no cluster access)
  ktl verify init chart --chart ./chart --release foo -n default > verify.yaml

  # Verify a chart render with cluster lookups enabled
  ktl verify init chart --chart ./chart --release foo -n default --use-cluster --context my-context > verify.yaml
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			chartPath = strings.TrimSpace(chartPath)
			release = strings.TrimSpace(release)
			namespace = strings.TrimSpace(namespace)
			if chartPath == "" {
				return fmt.Errorf("--chart is required")
			}
			if release == "" {
				return fmt.Errorf("--release is required")
			}
			if namespace == "" {
				namespace = "default"
			}

			cfg := verifyConfig{
				Version: "v1",
				Target: verifyTarget{
					Kind: "chart",
					Chart: verifyTargetChart{
						Chart:       chartPath,
						Release:     release,
						Namespace:   namespace,
						ValuesFiles: splitCSV(valuesFiles),
						SetValues:   splitCSV(setValues),
						UseCluster:  ptrBool(useCluster),
						IncludeCRDs: ptrBool(includeCRDs),
					},
				},
				Verify: verifyConfigRules{
					Mode:   strings.TrimSpace(mode),
					FailOn: strings.TrimSpace(failOn),
				},
				Output: verifyConfigOut{
					Format: strings.TrimSpace(format),
					Report: "-",
				},
				Kube: verifyConfigKube{
					Context: strings.TrimSpace(kubeContext),
				},
			}

			return writeVerifyInitConfig(cmd, cfg, outPath, force)
		},
	}

	cmd.Flags().StringVar(&outPath, "out", "-", `Output path ("-" for stdout)`)
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite --out when it exists")
	cmd.Flags().StringVar(&chartPath, "chart", "", "Helm chart path (directory or archive)")
	cmd.Flags().StringVar(&release, "release", "", "Helm release name")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Target namespace")
	cmd.Flags().StringSliceVar(&valuesFiles, "values", nil, "Values file(s) (repeatable or comma-separated)")
	cmd.Flags().StringSliceVar(&setValues, "set", nil, "Set value(s) (repeatable or comma-separated, KEY=VALUE)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kube context (optional; needed when --use-cluster is set)")
	cmd.Flags().BoolVar(&useCluster, "use-cluster", false, "Allow cluster lookups while rendering")
	cmd.Flags().BoolVar(&includeCRDs, "include-crds", false, "Include CRDs in the rendered output")
	cmd.Flags().StringVar(&mode, "mode", "block", "Verify mode: warn|block|off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Fail threshold: info|low|medium|high|critical")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table|json|sarif")

	return cmd
}

func newVerifyInitNamespaceCommand() *cobra.Command {
	var outPath string
	var force bool
	var namespace string
	var kubeContext string
	var mode string
	var failOn string
	var format string

	cmd := &cobra.Command{
		Use:   "namespace",
		Short: "Generate a verify config for a live namespace",
		Example: strings.TrimSpace(`
  # Verify a live namespace
  ktl verify init namespace -n default --context my-context > verify.yaml
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace = strings.TrimSpace(namespace)
			if namespace == "" {
				return fmt.Errorf("--namespace is required")
			}

			cfg := verifyConfig{
				Version: "v1",
				Target: verifyTarget{
					Kind:      "namespace",
					Namespace: namespace,
				},
				Verify: verifyConfigRules{
					Mode:   strings.TrimSpace(mode),
					FailOn: strings.TrimSpace(failOn),
				},
				Output: verifyConfigOut{
					Format: strings.TrimSpace(format),
					Report: "-",
				},
				Kube: verifyConfigKube{
					Context: strings.TrimSpace(kubeContext),
				},
			}

			return writeVerifyInitConfig(cmd, cfg, outPath, force)
		},
	}

	cmd.Flags().StringVar(&outPath, "out", "-", `Output path ("-" for stdout)`)
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite --out when it exists")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to verify")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kube context (recommended)")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Verify mode: warn|block|off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Fail threshold: info|low|medium|high|critical")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table|json|sarif")

	return cmd
}

func writeVerifyInitConfig(cmd *cobra.Command, cfg verifyConfig, outPath string, force bool) error {
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		outPath = "-"
	}

	// Normalize defaults (also ensures the config is valid).
	if err := cfg.validate("."); err != nil {
		return err
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if outPath == "-" {
		_, err := cmd.OutOrStdout().Write(raw)
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("refusing to overwrite %q (use --force)", outPath)
		}
	}
	return os.WriteFile(outPath, raw, 0o644)
}

func ptrBool(v bool) *bool {
	return &v
}
