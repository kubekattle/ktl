// File: cmd/ktl/stack_export.go
// Brief: `ktl stack export` command wiring.

package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackExportCommand(rootDir *string) *cobra.Command {
	var runID string
	var outPath string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a portable bundle for a stack run (sqlite + manifest)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(runID)
			if id == "" {
				var err error
				id, err = stack.LoadMostRecentRun(*rootDir)
				if err != nil {
					return err
				}
			}
			out := strings.TrimSpace(outPath)
			if out == "" {
				out = filepath.Join(*rootDir, ".ktl", "stack", "exports", id+".tgz")
			}
			wrote, err := stack.ExportRunBundle(cmd.Context(), *rootDir, id, out)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "ktl stack export: wrote %s\n", wrote)
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID to export (defaults to most recent)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output bundle path (defaults to --root/.ktl/stack/exports/<runId>.tgz)")
	return cmd
}
