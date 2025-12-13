// priorities.go exposes 'ktl diag priorities', correlating PriorityClasses with workloads so teams can inspect preemption policies.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/priorities"
	"github.com/spf13/cobra"
)

func newPrioritiesCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "priorities",
		Short: "Inspect PriorityClasses and pod priority/preemption state",
		Long: `Shows every PriorityClass (value, default, preemption policy) and lists pods sorted by priority so you can see which workloads preempt others.
Flags low-priority pods being terminated and pods nominating nodes so it’s obvious why lower-priority pods are removed first.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summary, err := priorities.Collect(ctx, kubeClient.Clientset, priorities.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
			})
			if err != nil {
				return err
			}
			priorities.Render(summary)
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces whose pods should be inspected for priority/preemption state")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Inspect pods across every namespace")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Priority Flags")
	return cmd
}
