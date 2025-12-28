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

func newStackRunsCommand(common stackCommandCommon) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "List recent stack runs from the sqlite state store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir := strings.TrimSpace(derefString(common.rootDir))
			if !flagChanged(cmd, "root") {
				if v := strings.TrimSpace(os.Getenv("KTL_STACK_ROOT")); v != "" {
					rootDir = v
				}
			}
			if rootDir == "" {
				rootDir = "."
			}

			runs, err := stack.ListRuns(rootDir, limit)
			if err != nil {
				return err
			}
			outFormat := strings.ToLower(strings.TrimSpace(derefString(common.output)))
			if cfg, err := resolveStackCommandConfig(cmd, common); err == nil {
				printStackConfigWarnings(cmd, cfg.Warnings)
				outFormat = cfg.Output
			} else if !isNoStackRootError(err) {
				return err
			} else if !flagChanged(cmd, "output") {
				if v := strings.TrimSpace(os.Getenv("KTL_STACK_OUTPUT")); v != "" {
					outFormat = strings.ToLower(v)
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
