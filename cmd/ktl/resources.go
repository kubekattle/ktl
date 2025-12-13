// resources.go introduces 'ktl diag resources', summarizing deployments/pods per namespace with readiness and image info.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/resources"
	"github.com/spf13/cobra"
)

func newResourcesCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool
	var top int

	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Show per-container CPU/memory requests, limits, and live usage",
		Long: `Combines pod specs and metrics to surface every container's CPU/memory requests, limits, and current usage so you can instantly spot
the pod that's eating 32 GiB or pegging the CPU.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summary, err := resources.Collect(ctx, kubeClient.Clientset, kubeClient.Metrics, resources.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
				Top:              top,
			})
			if err != nil {
				return err
			}
			resources.Print(summary, resources.RenderOptions{})
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to inspect (defaults to active context)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Show containers from every namespace")
	cmd.Flags().IntVarP(&top, "top", "t", 0, "Limit output to the top N containers by memory usage (0 = all)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Resource Flags")

	return cmd
}
