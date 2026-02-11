// File: cmd/ktl/stack_status.go
// Brief: `ktl stack status` command wiring.

package main

import (
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackStatusCommand(rootDir *string) *cobra.Command {
	var runID string
	var follow bool
	var limit int
	var format string
	var helmLogs string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of the most recent (or selected) stack run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stack.RunStatus(cmd.Context(), stack.StatusOptions{
				RootDir:  *rootDir,
				RunID:    runID,
				Follow:   follow,
				Limit:    limit,
				Format:   format,
				HelmLogs: strings.TrimSpace(helmLogs),
			}, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID (stored in .ktl/stack/state.sqlite); defaults to most recent")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the events stream")
	cmd.Flags().IntVar(&limit, "tail", 50, "How many recent event lines to show before following")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: raw|table|json|tty")
	cmd.Flags().StringVar(&helmLogs, "helm-logs", "", "TTY helm logs mode: off|on|all (default off)")
	cmd.Flags().Lookup("helm-logs").NoOptDefVal = "on"
	return cmd
}
