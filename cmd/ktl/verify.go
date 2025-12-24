package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/verify"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

func newVerifyCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "verify",
		Short:         "Verify Kubernetes configuration for security and policy issues",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(
		newVerifyChartCommand(kubeconfigPath, kubeContext, logLevel),
		newVerifyNamespaceCommand(kubeconfigPath, kubeContext, logLevel),
	)

	return cmd
}

func newVerifyChartCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	var chartRef string
	var release string
	var namespace string
	var valuesFiles []string
	var setValues []string
	var format string
	var mode string
	var rulesDir string
	var failOn string

	cmd := &cobra.Command{
		Use:           "chart --chart <path> --release <name>",
		Short:         "Verify a Helm chart by rendering and scanning namespaced resources",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if strings.TrimSpace(chartRef) == "" || strings.TrimSpace(release) == "" {
				return fmt.Errorf("--chart and --release are required")
			}

			settings := cli.New()
			settings.KubeConfig = strings.TrimSpace(deref(kubeconfigPath))
			if settings.KubeConfig == "" {
				settings.KubeConfig = os.Getenv("KUBECONFIG")
			}
			if v := strings.TrimSpace(deref(kubeContext)); v != "" {
				settings.KubeContext = v
			}

			actionCfg := new(action.Configuration)
			if err := actionCfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(format string, args ...interface{}) {
				_ = logLevel
			}); err != nil {
				return err
			}

			result, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
				Chart:       chartRef,
				ReleaseName: release,
				Namespace:   namespace,
				ValuesFiles: valuesFiles,
				SetValues:   setValues,
				UseCluster:  false,
				IncludeCRDs: false,
			})
			if err != nil {
				return err
			}
			objs, err := verify.DecodeK8SYAML(result.Manifest)
			if err != nil {
				return err
			}
			if strings.TrimSpace(rulesDir) == "" {
				rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
			}
			rep, err := verify.VerifyObjects(ctx, objs, verify.Options{
				Mode:     verify.Mode(strings.ToLower(strings.TrimSpace(mode))),
				FailOn:   verify.Severity(strings.ToLower(strings.TrimSpace(failOn))),
				Format:   verify.OutputFormat(strings.ToLower(strings.TrimSpace(format))),
				RulesDir: rulesDir,
			})
			if err != nil {
				return err
			}
			if err := verify.WriteReport(cmd.OutOrStdout(), rep, repModeFormat(format)); err != nil {
				return err
			}
			if rep.Blocked {
				return fmt.Errorf("verify blocked (fail-on=%s)", failOn)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&chartRef, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&release, "release", "", "Release name")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace (defaults to active context)")
	cmd.Flags().StringSliceVarP(&valuesFiles, "values", "f", nil, "Values files (repeatable)")
	cmd.Flags().StringArrayVar(&setValues, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json, sarif")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Mode: warn, block, off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Block threshold when --mode=block: info, low, medium, high, critical")
	cmd.Flags().StringVar(&rulesDir, "rules-dir", "", "Rules directory (defaults to the pinned builtin rules)")
	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")
	return cmd
}

func newVerifyNamespaceCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	var format string
	var mode string
	var rulesDir string
	var failOn string

	cmd := &cobra.Command{
		Use:           "namespace <name>",
		Short:         "Verify a live namespace by scanning namespaced resources only",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			client, err := kube.New(ctx, strings.TrimSpace(deref(kubeconfigPath)), strings.TrimSpace(deref(kubeContext)))
			if err != nil {
				return err
			}
			namespace := strings.TrimSpace(args[0])
			if namespace == "" {
				return fmt.Errorf("namespace is required")
			}

			objs, err := collectNamespacedObjects(ctx, client, namespace)
			if err != nil {
				return err
			}
			if strings.TrimSpace(rulesDir) == "" {
				rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
			}
			rep, err := verify.VerifyObjects(ctx, objs, verify.Options{
				Mode:     verify.Mode(strings.ToLower(strings.TrimSpace(mode))),
				FailOn:   verify.Severity(strings.ToLower(strings.TrimSpace(failOn))),
				Format:   verify.OutputFormat(strings.ToLower(strings.TrimSpace(format))),
				RulesDir: rulesDir,
			})
			if err != nil {
				return err
			}
			if err := verify.WriteReport(cmd.OutOrStdout(), rep, repModeFormat(format)); err != nil {
				return err
			}
			if rep.Blocked {
				return fmt.Errorf("verify blocked (fail-on=%s)", failOn)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json, sarif")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Mode: warn, block, off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Block threshold when --mode=block: info, low, medium, high, critical")
	cmd.Flags().StringVar(&rulesDir, "rules-dir", "", "Rules directory (defaults to the pinned builtin rules)")
	_ = logLevel
	return cmd
}

func repModeFormat(v string) verify.OutputFormat {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "table":
		return verify.OutputTable
	case "json":
		return verify.OutputJSON
	case "sarif":
		return verify.OutputSARIF
	default:
		return verify.OutputTable
	}
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
