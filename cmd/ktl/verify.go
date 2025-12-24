package main

import (
	"fmt"

	"github.com/spf13/cobra"
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
	_ = kubeconfigPath
	_ = kubeContext
	_ = logLevel
	return &cobra.Command{
		Use:           "chart --chart <path> --release <name>",
		Short:         "Verify a Helm chart by rendering and scanning namespaced resources",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd
			_ = args
			return fmt.Errorf("verify chart: not implemented")
		},
	}
}

func newVerifyNamespaceCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	_ = kubeconfigPath
	_ = kubeContext
	_ = logLevel
	return &cobra.Command{
		Use:           "namespace <name>",
		Short:         "Verify a live namespace by scanning namespaced resources only",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd
			_ = args
			return fmt.Errorf("verify namespace: not implemented")
		},
	}
}
