// File: cmd/ktl/package.go
// Brief: CLI command wiring and implementation for 'package'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/chartarchive"
	"github.com/spf13/cobra"
)

func newPackageCommand() *cobra.Command {
	var outputPath string
	var force bool
	var quiet bool
	var jsonOut bool
	var verifyPath string

	cmd := &cobra.Command{
		Use:           "package [CHART_DIR]",
		Short:         "Package a chart directory into a sqlite chart archive",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if quiet && jsonOut {
				return fmt.Errorf("--quiet and --json are mutually exclusive")
			}
			if p := strings.TrimSpace(verifyPath); p != "" {
				res, err := chartarchive.VerifyArchive(cmd.Context(), p)
				if err != nil {
					return err
				}
				if jsonOut {
					raw, _ := json.Marshal(res)
					fmt.Fprintln(cmd.OutOrStdout(), string(raw))
					return nil
				}
				if quiet {
					fmt.Fprintln(cmd.OutOrStdout(), res.ArchivePath)
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Archive %s verified (files=%d bytes=%d sha256=%s)\n", res.ArchivePath, res.FileCount, res.TotalBytes, res.ContentSHA256)
				return nil
			}

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
			if jsonOut {
				raw, _ := json.Marshal(res)
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			}
			if quiet {
				fmt.Fprintln(cmd.OutOrStdout(), res.ArchivePath)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Chart %s packaged to %s (files=%d bytes=%d sha256=%s)\n", res.ChartName, res.ArchivePath, res.FileCount, res.TotalBytes, res.ContentSHA256)
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write the sqlite archive to this path (or directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the output file if it already exists")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Print only the output archive path")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().StringVar(&verifyPath, "verify", "", "Verify an existing sqlite chart archive (PATH)")
	cmd.Example = `  # Package a chart directory (defaults to current directory)
  ktl package ./chart

  # Write to a specific file
  ktl package ./chart --output dist/chart.sqlite

  # Write into a directory (filename derived from Chart.yaml)
  ktl package ./chart --output dist/

  # Verify an existing archive
  ktl package --verify dist/chart.sqlite`
	decorateCommandHelp(cmd, "Package Flags")
	return cmd
}
