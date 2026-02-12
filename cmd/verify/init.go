package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cfgpkg "github.com/kubekattle/ktl/internal/verify/config"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func newVerifyInitCommand() *cobra.Command {
	var outPath string
	var force bool
	var kind string
	var chartPath string
	var release string
	var namespace string
	var manifest string
	var valuesFiles []string
	var setValues []string
	var kubeContext string
	var useCluster bool
	var includeCRDs bool
	var mode string
	var failOn string
	var format string
	var report string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a starter verify config YAML",
		Long: strings.TrimSpace(`
Generate a ready-to-run verify config YAML for common targets.

Examples:
  verify init --chart ./chart --release foo -n default --write verify.yaml
  verify init namespace -n default --context my-context --write verify.yaml
  verify init manifest --manifest ./rendered.yaml --write verify.yaml
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cfgpkg.BuildFromParams(cfgpkg.Params{
				Kind: kind,

				ChartPath:   chartPath,
				Release:     release,
				Namespace:   namespace,
				Manifest:    manifest,
				ValuesFiles: valuesFiles,
				SetValues:   setValues,

				UseCluster:  useCluster,
				IncludeCRDs: includeCRDs,

				Mode:        mode,
				FailOn:      failOn,
				Format:      format,
				Report:      report,
				KubeContext: kubeContext,
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.Target.Kind) == "" {
				return cmd.Help()
			}
			return writeVerifyInitConfig(cmd, cfg, outPath, force)
		},
	}

	cmd.Flags().StringVar(&outPath, "write", "-", `Output path ("-" for stdout)`)
	cmd.Flags().StringVar(&outPath, "out", "-", `Output path ("-" for stdout)`)
	_ = cmd.Flags().MarkDeprecated("out", "use --write")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite --write when it exists")
	cmd.Flags().StringVar(&kind, "kind", "", "Target kind: chart|namespace|manifest (optional; inferred from flags when omitted)")

	// Shared flags for scripting convenience (only used when calling `verify init` without a subcommand).
	cmd.Flags().StringVar(&chartPath, "chart", "", "Helm chart path (kind=chart)")
	cmd.Flags().StringVar(&release, "release", "", "Helm release name (kind=chart)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace (kind=namespace or chart namespace for kind=chart)")
	cmd.Flags().StringVar(&manifest, "manifest", "", "Manifest file path (kind=manifest)")
	cmd.Flags().StringSliceVar(&valuesFiles, "values", nil, "Values file(s) (kind=chart)")
	cmd.Flags().StringSliceVar(&setValues, "set", nil, "Set value(s) (kind=chart)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kube context (optional)")
	cmd.Flags().BoolVar(&useCluster, "use-cluster", false, "Allow cluster lookups while rendering (kind=chart)")
	cmd.Flags().BoolVar(&includeCRDs, "include-crds", false, "Include CRDs in the rendered output (kind=chart)")
	cmd.Flags().StringVar(&mode, "mode", "", "Verify mode: warn|block|off")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "Fail threshold: info|low|medium|high|critical")
	cmd.Flags().StringVar(&format, "format", "", "Output format: table|json|sarif|html|md")
	cmd.Flags().StringVar(&report, "report", "-", `Report path ("-" for stdout)`)

	cmd.AddCommand(newVerifyInitChartCommand(), newVerifyInitNamespaceCommand(), newVerifyInitManifestCommand())
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
  verify init chart --chart ./chart --release foo -n default --write verify.yaml

  # Verify a chart render with cluster lookups enabled
  verify init chart --chart ./chart --release foo -n default --use-cluster --context my-context --write verify.yaml
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cfgpkg.BuildFromParams(cfgpkg.Params{
				Kind: "chart",

				ChartPath:   chartPath,
				Release:     release,
				Namespace:   namespace,
				ValuesFiles: valuesFiles,
				SetValues:   setValues,

				UseCluster:  useCluster,
				IncludeCRDs: includeCRDs,

				Mode:        mode,
				FailOn:      failOn,
				Format:      format,
				Report:      "-",
				KubeContext: kubeContext,
			})
			if err != nil {
				return err
			}
			return writeVerifyInitConfig(cmd, cfg, outPath, force)
		},
	}

	cmd.Flags().StringVar(&outPath, "write", "-", `Output path ("-" for stdout)`)
	cmd.Flags().StringVar(&outPath, "out", "-", `Output path ("-" for stdout)`)
	_ = cmd.Flags().MarkDeprecated("out", "use --write")
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
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table|json|sarif|html|md")

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
  verify init namespace -n default --context my-context --write verify.yaml
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cfgpkg.BuildFromParams(cfgpkg.Params{
				Kind: "namespace",

				Namespace: namespace,

				Mode:        mode,
				FailOn:      failOn,
				Format:      format,
				Report:      "-",
				KubeContext: kubeContext,
			})
			if err != nil {
				return err
			}
			return writeVerifyInitConfig(cmd, cfg, outPath, force)
		},
	}

	cmd.Flags().StringVar(&outPath, "write", "-", `Output path ("-" for stdout)`)
	cmd.Flags().StringVar(&outPath, "out", "-", `Output path ("-" for stdout)`)
	_ = cmd.Flags().MarkDeprecated("out", "use --write")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite --out when it exists")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace to verify")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kube context (recommended)")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Verify mode: warn|block|off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Fail threshold: info|low|medium|high|critical")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table|json|sarif|html|md")

	return cmd
}

func newVerifyInitManifestCommand() *cobra.Command {
	var outPath string
	var force bool
	var manifestPath string
	var mode string
	var failOn string
	var format string

	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Generate a verify config for an already-rendered manifest YAML file",
		Example: strings.TrimSpace(`
  # Verify a rendered manifest file
  verify init manifest --manifest ./rendered.yaml --write verify.yaml
  verify verify.yaml
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := cfgpkg.BuildFromParams(cfgpkg.Params{
				Kind: "manifest",

				Manifest: manifestPath,

				Mode:   mode,
				FailOn: failOn,
				Format: format,
				Report: "-",
			})
			if err != nil {
				return err
			}
			return writeVerifyInitConfig(cmd, cfg, outPath, force)
		},
	}
	cmd.Flags().StringVar(&outPath, "write", "-", `Output path ("-" for stdout)`)
	cmd.Flags().StringVar(&outPath, "out", "-", `Output path ("-" for stdout)`)
	_ = cmd.Flags().MarkDeprecated("out", "use --write")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite --out when it exists")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Manifest YAML path")
	cmd.Flags().StringVar(&mode, "mode", "block", "Verify mode: warn|block|off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Fail threshold: info|low|medium|high|critical")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table|json|sarif|html|md")
	return cmd
}

func writeVerifyInitConfig(cmd *cobra.Command, cfg cfgpkg.Config, outPath string, force bool) error {
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		outPath = "-"
	}

	// Normalize defaults (also ensures the config is valid).
	if err := cfg.Validate("."); err != nil {
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
