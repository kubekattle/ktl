// File: cmd/ktl/package.go
// Brief: CLI command wiring and implementation for 'package'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/chartarchive"
	"github.com/spf13/cobra"
)

func newPackageCommand() *cobra.Command {
	var outputPath string
	var force bool

	cmd := &cobra.Command{
		Use:           "package [CHART_DIR]",
		Short:         "Package a chart directory into a sqlite chart archive",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			chartDir := "."
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				chartDir = args[0]
			}
			res, err := chartarchive.PackageDir(cmd.Context(), chartDir, chartarchive.PackageOptions{
				OutputPath: outputPath,
				Force:      force,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Chart %s packaged to %s\n", res.ChartName, res.ArchivePath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write the sqlite archive to this path (or directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the output file if it already exists")
	cmd.Example = `  # Package a chart directory (defaults to current directory)
  ktl package ./chart

  # Write to a specific file
  ktl package ./chart --output dist/chart.sqlite

  # Write into a directory (filename derived from Chart.yaml)
  ktl package ./chart --output dist/`
	decorateCommandHelp(cmd, "Package Flags")
	return cmd
}
