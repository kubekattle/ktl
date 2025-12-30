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
	var printSHA bool
	logLevel := "info"

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
					if jsonOut {
						raw, _ := json.Marshal(map[string]any{
							"success": false,
							"error":   err.Error(),
						})
						fmt.Fprintln(cmd.OutOrStdout(), string(raw))
					}
					return err
				}
				if jsonOut {
					raw, _ := json.Marshal(res)
					fmt.Fprintln(cmd.OutOrStdout(), string(raw))
					return nil
				}
				if quiet {
					if printSHA {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.ContentSHA256, res.ArchivePath)
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), res.ArchivePath)
					}
					return nil
				}
				if strings.TrimSpace(res.ChartVersion) == "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning: archive chart version is empty")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Archive %s verified (files=%d bytes=%d sha256=%s)\n", res.ArchivePath, res.FileCount, res.TotalBytes, res.ContentSHA256)
				return nil
			}

			chartDir := "."
			if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
				chartDir = args[0]
			}
			if strings.TrimSpace(logLevel) == "debug" {
				fmt.Fprintf(cmd.ErrOrStderr(), "debug: packaging chart dir %s (output=%s)\n", chartDir, strings.TrimSpace(outputPath))
			}
			res, err := chartarchive.PackageDir(cmd.Context(), chartDir, chartarchive.PackageOptions{
				OutputPath: outputPath,
				Force:      force,
			})
			if err != nil {
				if jsonOut {
					raw, _ := json.Marshal(map[string]any{
						"success": false,
						"error":   err.Error(),
					})
					fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				}
				return err
			}
			if jsonOut {
				raw, _ := json.Marshal(res)
				fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			}
			if quiet {
				if printSHA {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.ContentSHA256, res.ArchivePath)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), res.ArchivePath)
				}
				return nil
			}
			if strings.TrimSpace(res.ChartVersion) == "" || strings.EqualFold(strings.TrimSpace(res.ChartVersion), "unknown") {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: chart version is empty; set 'version' in Chart.yaml for reproducible filenames")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Chart %s packaged to %s (files=%d bytes=%d sha256=%s)\n", res.ChartName, res.ArchivePath, res.FileCount, res.TotalBytes, res.ContentSHA256)
			if printSHA {
				fmt.Fprintf(cmd.OutOrStdout(), "SHA256: %s\n", res.ContentSHA256)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write the sqlite archive to this path (or directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the output file if it already exists")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Print only the output archive path")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&printSHA, "print-sha", false, "Print archive SHA256 (with --quiet: emits '<sha> <path>')")
	cmd.Flags().StringVar(&verifyPath, "verify", "", "Verify an existing sqlite chart archive (PATH)")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "Log level: debug|info|warn|error (debug prints extra diagnostics)")
	cmd.Example = `  # Package a chart directory (defaults to current directory)
  package ./chart

  # Write to a specific file
  package ./chart --output dist/chart.sqlite

  # Write into a directory (filename derived from Chart.yaml)
  package ./chart --output dist/

  # Verify an existing archive
  package --verify dist/chart.sqlite

  # Package then verify
  package ./chart --output dist/chart.sqlite && package --verify dist/chart.sqlite`
	return cmd
}
