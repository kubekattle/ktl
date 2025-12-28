// File: cmd/ktl/stack_runs.go
// Brief: `ktl stack runs` command wiring.

package main

import (
	"encoding/json"
	"fmt"
	"os"
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
			outFormat := strings.ToLower(strings.TrimSpace(*output))
			// Allow stack.yaml/env to set a default output format even when --output is hidden.
			if !cmd.Flags().Changed("output") {
				if v := strings.TrimSpace(os.Getenv("KTL_STACK_OUTPUT")); v != "" {
					outFormat = strings.ToLower(v)
				} else if u, err := stack.Discover(*rootDir); err == nil {
					profile := ""
					if pf := cmd.Flag("profile"); pf != nil {
						profile = strings.TrimSpace(pf.Value.String())
					}
					if strings.TrimSpace(profile) == "" {
						profile = strings.TrimSpace(u.DefaultProfile)
					}
					if cfg, err := stack.ResolveStackCLIConfig(u, profile); err == nil {
						outFormat = strings.ToLower(strings.TrimSpace(cfg.Output))
					}
				}
			}

			switch outFormat {
			case "", "table":
				return stack.PrintRunsTable(cmd.OutOrStdout(), runs)
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(runs)
			default:
				return fmt.Errorf("unknown --output %q (expected table|json)", outFormat)
			}
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of runs to list (newest first)")
	return cmd
}
