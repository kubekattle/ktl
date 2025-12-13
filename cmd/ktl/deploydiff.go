// deploydiff.go provides 'ktl logs diff-deployments', tailing events for specified Deployments to contrast new vs old ReplicaSet behavior.
package main

import (
	"github.com/example/ktl/internal/deploydiff"
	"github.com/example/ktl/internal/kube"
	"github.com/spf13/cobra"
)

func newDeployDiffCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "diff-deployments [DEPLOYMENT]...",
		Short: "Compare rollout events between old and new pods during a Deployment rollout",
		Long: `Streams Kubernetes events for the active rollout and color-codes them by pod generation:
NEW (green) events come from the ReplicaSet that is currently scaling up, while OLD (yellow) represents everything else.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			ns := namespace
			if ns == "" {
				ns = kubeClient.Namespace
				if ns == "" {
					ns = "default"
				}
			}
			runner := deploydiff.New(kubeClient.Clientset, deploydiff.Options{
				Namespace:   ns,
				Deployments: args,
			})
			return runner.Run(ctx)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace containing the deployments (defaults to kubectl context namespace)")
	decorateCommandHelp(cmd, "DeployDiff Flags")
	return cmd
}
