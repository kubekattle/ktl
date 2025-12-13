// diag.go glues together the 'ktl diag' umbrella command and registers every diagnostic subcommand (nodes, quotas, reports, etc.).
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

type diagCommandFactory func(kubeconfig *string, kubeContext *string) *cobra.Command

type diagCommandEntry struct {
	factory   diagCommandFactory
	keepAlias bool
}

var diagCommandEntries = []diagCommandEntry{
	{factory: newQuotaCommand, keepAlias: true},
	{factory: newNodesCommand, keepAlias: true},
	{factory: newPodSecurityCommand, keepAlias: true},
	{factory: newPrioritiesCommand, keepAlias: true},
	{factory: newResourcesCommand, keepAlias: true},
	{factory: newCronJobsCommand, keepAlias: true},
	{factory: newNetworkCommand, keepAlias: true},
	{factory: newStorageCommand, keepAlias: true},
	{factory: newTopCommand, keepAlias: true},
	{factory: newHealthCommand, keepAlias: true},
	{factory: newReportCommand, keepAlias: false},
	{factory: newDiagSnapshotCommand, keepAlias: false},
}

func newDiagCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diag",
		Short: "Run cluster diagnostics (nodes, quotas, resources, reports, and more)",
		Long:  "Group of read-only diagnostics such as nodes, quotas, resources, storage, and HTML reports.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	for _, entry := range diagCommandEntries {
		cmd.AddCommand(entry.factory(kubeconfig, kubeContext))
	}

	return cmd
}

func addLegacyDiagCommands(root *cobra.Command, kubeconfig *string, kubeContext *string) {
	for _, entry := range diagCommandEntries {
		if !entry.keepAlias {
			continue
		}
		legacy := entry.factory(kubeconfig, kubeContext)
		legacy.Hidden = true
		legacy.Deprecated = fmt.Sprintf("use 'ktl diag %s'", legacy.Name())
		root.AddCommand(legacy)
	}
}
