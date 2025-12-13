// network.go introduces 'ktl diag network', summarizing Ingresses, Gateways, and Services to verify endpoint readiness.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/networkstatus"
	"github.com/spf13/cobra"
)

func newNetworkCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool
	var showServices bool

	cmd := &cobra.Command{
		Use:   "network",
		Short: "Show Ingress/Gateway/Service readiness (LB IPs, endpoints, TLS secrets)",
		Long: `Displays the health of HTTP entry points: Ingress load balancer IPs, Gateway addresses, TLS secrets, and Service endpoints so you can verify that
traffic will actually flow.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summary, err := networkstatus.Collect(ctx, kubeClient.Clientset, kubeClient.Dynamic, networkstatus.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
			})
			if err != nil {
				return err
			}
			networkstatus.Print(summary, networkstatus.RenderOptions{
				ShowServices: showServices,
			})
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to inspect (defaults to active kubeconfig namespace)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Inspect resources across every namespace")
	cmd.Flags().BoolVar(&showServices, "show-services", true, "Include the Services table (disable if you only care about Ingress/Gateways)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Network Flags")
	return cmd
}
