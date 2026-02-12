// File: cmd/ktl/stack_audit.go
// Brief: `ktl stack audit` command wiring.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kubekattle/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackAuditCommand(rootDir *string) *cobra.Command {
	var runID string
	var output string
	var verify bool
	var eventsLimit int
	var includePlan bool
	var includeEvents bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show who/what/when for a stack run (sqlite-backed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := stack.GetRunAudit(cmd.Context(), stack.RunAuditOptions{
				RootDir:       *rootDir,
				RunID:         runID,
				Verify:        verify,
				EventsLimit:   eventsLimit,
				IncludePlan:   includePlan,
				IncludeEvents: includeEvents,
			})
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(output)) {
			case "", "table":
				return stack.PrintRunAuditTable(cmd.OutOrStdout(), a)
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(a)
			case "html":
				return stack.PrintRunAuditHTML(cmd.OutOrStdout(), a)
			default:
				return fmt.Errorf("unknown --output %q (expected table|json|html)", output)
			}
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID (stored in .ktl/stack/state.sqlite); defaults to most recent")
	cmd.Flags().StringVar(&output, "output", "table", "Output format: table|json|html")
	cmd.Flags().BoolVar(&verify, "verify", true, "Verify event chain and run digest")
	cmd.Flags().IntVar(&eventsLimit, "events", 1000, "How many events to include in json/html output (0 uses default, -1 means all)")
	cmd.Flags().BoolVar(&includePlan, "include-plan", true, "Include the stored run plan in json/html output")
	cmd.Flags().BoolVar(&includeEvents, "include-events", true, "Include stored events in json/html output")
	return cmd
}
