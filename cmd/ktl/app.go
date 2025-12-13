// app.go defines the 'ktl app' parent command that groups packaging, unpacking, and vendoring actions for ktl archives.
package main

import "github.com/spf13/cobra"

func newAppCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Work with ktl application archives",
	}

	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "Namespace used for application templates (defaults to kube context)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	cmd.AddCommand(
		newAppPackageCommand(&namespace, kubeconfig, kubeContext),
		newAppUnpackCommand(),
		newAppVendorCommand(),
	)

	return cmd
}
