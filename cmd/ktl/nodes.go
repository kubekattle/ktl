// nodes.go registers 'ktl diag nodes', collecting allocatable/capacity stats plus pressures across cluster nodes.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/nodes"
	"github.com/spf13/cobra"
)

func newNodesCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var (
		selector      string
		allNamespaces bool
		namespace     string
	)

	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "Inspect allocatable vs capacity resources per node",
		Long: `Summarizes each node's CPU/memory/ephemeral-storage capacity, allocatable headroom, and actual pod requests.
Highlights nodes that are cordoned, tainted, NotReady, or saturated so you can answer "Why can't I schedule here?" quickly.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summaries, err := nodes.Collect(ctx, kubeClient.Clientset, nodes.Options{
				LabelSelector: selector,
			})
			if err != nil {
				return err
			}
			nodes.PrintTable(summaries, nodes.RenderOptions{
				WarnThreshold:  0.80,
				BlockThreshold: 1.0,
			})
			if allNamespaces {
				cmd.Printf("note: --all-namespaces is ignored for cluster-scoped resources like nodes\n")
			}
			if namespace != "" {
				cmd.Printf("note: --namespace is ignored for cluster-scoped resources like nodes\n")
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&selector, "selector", "l", "", "Label selector to filter nodes")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Parity flag; nodes are cluster-scoped so this has no effect")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Parity flag; nodes are cluster-scoped so this has no effect")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Node Flags")
	return cmd
}
