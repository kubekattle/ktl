// quotas.go defines 'ktl diag quotas', printing ResourceQuota/LimitRange consumption per namespace with warning thresholds.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/quota"
	"github.com/spf13/cobra"
)

func newQuotaCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "quotas",
		Short: "Display ResourceQuota and LimitRange utilization per namespace",
		Long:  "Retrieves Kubernetes ResourceQuota and LimitRange data, compares used versus hard limits for pods/CPU/memory/PVCs, highlights risky namespaces (>80%), and flags namespaces that have exhausted their quotas.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summaries, err := quota.Collect(ctx, kubeClient.Clientset, quota.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
			})
			if err != nil {
				return err
			}
			quota.PrintTable(summaries, quota.RenderOptions{
				WarnThreshold:  0.80,
				BlockThreshold: 1.0,
			})
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to inspect (defaults to the active kubeconfig namespace)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Show quotas for every namespace")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Quota Flags")

	return cmd
}
