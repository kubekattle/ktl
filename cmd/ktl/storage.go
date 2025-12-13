// storage.go registers 'ktl diag storage', reviewing PVCs, PVs, and pending claims to surface capacity or access issues.
package main

import (
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/pvcstatus"
	"github.com/spf13/cobra"
)

func newStorageCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Correlate PVC status with node pressure to explain Pending volumes",
		Long: `Lists PersistentVolumeClaims, their phases, capacity, and the pods/nodes that mount them, then overlays node Memory/Disk/PID pressure
to explain why a pod may be stuck in Pending despite having a claim.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			summaries, err := pvcstatus.Collect(ctx, kubeClient.Clientset, pvcstatus.Options{
				Namespaces:       namespaces,
				AllNamespaces:    allNamespaces,
				DefaultNamespace: kubeClient.Namespace,
			})
			if err != nil {
				return err
			}
			pvcstatus.Print(summaries, pvcstatus.RenderOptions{})
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces whose PVCs should be inspected (defaults to active context)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Inspect PVCs across every namespace")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Storage Flags")
	return cmd
}
