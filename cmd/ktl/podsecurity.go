// podsecurity.go adds 'ktl diag podsecurity', explaining namespace PodSecurity admission levels and risky exemptions.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/podsecurity"
	"github.com/spf13/cobra"
)

func newPodSecurityCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "podsecurity",
		Short: "Summarize PodSecurity admission labels and violations",
		Long: `Inspects namespace PodSecurity admission labels (enforce/audit/warn) and scans pods for common restricted/baseline violations such as host networking,
privileged containers, and forbidden capabilities so you can react before deployments fail.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summaries, err := podsecurity.Collect(ctx, kubeClient.Clientset, podsecurity.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
			})
			if err != nil {
				return err
			}
			podsecurity.PrintReport(summaries, podsecurity.RenderOptions{})
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to inspect (defaults to the active kubeconfig namespace)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Show PodSecurity data for every namespace")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "PodSecurity Flags")

	return cmd
}
