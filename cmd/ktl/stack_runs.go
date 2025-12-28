// File: cmd/ktl/stack_runs.go
// Brief: `ktl stack runs` command wiring.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackRunsCommand(rootDir *string, output *string) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List recent stack runs from the sqlite state store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runs, err := stack.ListRuns(*rootDir, limit)
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(*output)) {
			case "", "table":
				return stack.PrintRunsTable(cmd.OutOrStdout(), runs)
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(runs)
			default:
				return fmt.Errorf("unknown --output %q (expected table|json)", *output)
			}
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of runs to list (newest first)")
	return cmd
}
