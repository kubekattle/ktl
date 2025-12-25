// File: cmd/ktl/stack_status.go
// Brief: `ktl stack status` command wiring.

package main

import (
	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackStatusCommand(rootDir *string) *cobra.Command {
	var runID string
	var follow bool
	var limit int
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of the most recent (or selected) stack run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return stack.RunStatus(cmd.Context(), stack.StatusOptions{
				RootDir: *rootDir,
				RunID:   runID,
				Follow:  follow,
				Limit:   limit,
				Format:  format,
			}, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID (directory name under .ktl/stack/runs); defaults to most recent")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the events stream")
	cmd.Flags().IntVar(&limit, "tail", 50, "How many recent event lines to show before following")
	cmd.Flags().StringVar(&format, "format", "raw", "Output format: raw|table|json")
	return cmd
}
