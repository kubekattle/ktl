package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ingresslabs/torque/internal/chartarchive"
	"github.com/spf13/cobra"
)

func newHelmerArchiveCommand() *cobra.Command {
	var outputPath string
	var force bool
	var quiet bool
	var jsonOut bool
	var printSHA bool
	var maxStreamBytes int64

	cmd := &cobra.Command{
		Use:           "archive [CHART_DIR]",
		Short:         "Package a chart directory into a Helmer archive",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if quiet && jsonOut {
				return fmt.Errorf("--quiet and --json are mutually exclusive")
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
				return writeHelmerArchiveError(cmd, jsonOut, err)
			}
			if strings.TrimSpace(outputPath) == "-" {
				if err := streamHelmerArchiveFile(res.ArchivePath, cmd.OutOrStdout(), maxStreamBytes); err != nil {
					return err
				}
				_ = os.Remove(res.ArchivePath)
				return nil
			}
			if jsonOut {
				return writeHelmerArchiveJSON(cmd.OutOrStdout(), res)
			}
			if quiet {
				if printSHA {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.ContentSHA256, res.ArchivePath)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), res.ArchivePath)
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Chart %s archived to %s (files=%d bytes=%d sha256=%s)\n", res.ChartName, res.ArchivePath, res.FileCount, res.TotalBytes, res.ContentSHA256)
			if printSHA {
				fmt.Fprintf(cmd.OutOrStdout(), "SHA256: %s\n", res.ContentSHA256)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write the archive to this path (or directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the output file if it already exists")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Print only the output archive path")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&printSHA, "print-sha", false, "Print archive SHA256 (with --quiet: emits '<sha> <path>')")
	cmd.Flags().Int64Var(&maxStreamBytes, "max-stream-bytes", 512*1024*1024, "Maximum bytes to write when --output - is used (0 = unlimited)")
	cmd.Example = strings.TrimSpace(`
  # Archive a chart directory
  helmer archive ./chart --output dist/chart.sqlite

  # Stream an archive to stdout
  helmer archive ./chart --output - > chart.sqlite
`)
	decorateCommandHelp(cmd, "Archive Flags")
	return cmd
}

func newHelmerVerifyArchiveCommand() *cobra.Command {
	var quiet bool
	var jsonOut bool
	var printSHA bool
	var maxStreamBytes int64

	cmd := &cobra.Command{
		Use:           "verify-archive <ARCHIVE>",
		Short:         "Verify a Helmer chart archive",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if quiet && jsonOut {
				return fmt.Errorf("--quiet and --json are mutually exclusive")
			}
			path, cleanup, err := materializeHelmerArchiveInput(args[0], maxStreamBytes)
			if err != nil {
				return err
			}
			defer cleanup()

			res, err := chartarchive.VerifyArchive(cmd.Context(), path)
			if err != nil {
				return writeHelmerArchiveError(cmd, jsonOut, err)
			}
			if jsonOut {
				return writeHelmerArchiveJSON(cmd.OutOrStdout(), res)
			}
			if quiet {
				if printSHA {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.ContentSHA256, res.ArchivePath)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), res.ArchivePath)
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Archive %s verified (files=%d bytes=%d sha256=%s)\n", res.ArchivePath, res.FileCount, res.TotalBytes, res.ContentSHA256)
			return nil
		},
	}
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Print only the verified archive path")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&printSHA, "print-sha", false, "Print archive SHA256 (with --quiet: emits '<sha> <path>')")
	cmd.Flags().Int64Var(&maxStreamBytes, "max-stream-bytes", 512*1024*1024, "Maximum bytes to read when ARCHIVE is '-' (0 = unlimited)")
	cmd.Example = strings.TrimSpace(`
  # Verify an archive file
  helmer verify-archive dist/chart.sqlite

  # Verify from stdin
  helmer verify-archive - < chart.sqlite
`)
	decorateCommandHelp(cmd, "Verify Flags")
	return cmd
}

func newHelmerUnpackCommand() *cobra.Command {
	var destination string
	var force bool
	var quiet bool
	var jsonOut bool
	var printSHA bool
	var maxStreamBytes int64

	cmd := &cobra.Command{
		Use:           "unpack <ARCHIVE>",
		Short:         "Unpack a Helmer chart archive",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if quiet && jsonOut {
				return fmt.Errorf("--quiet and --json are mutually exclusive")
			}
			path, cleanup, err := materializeHelmerArchiveInput(args[0], maxStreamBytes)
			if err != nil {
				return err
			}
			defer cleanup()

			verifyRes, err := chartarchive.VerifyArchive(cmd.Context(), path)
			if err != nil {
				return writeHelmerArchiveError(cmd, jsonOut, err)
			}
			res, err := chartarchive.UnpackArchive(cmd.Context(), path, chartarchive.UnpackOptions{
				DestinationPath: destination,
				Force:           force,
			})
			if err != nil {
				return writeHelmerArchiveError(cmd, jsonOut, err)
			}
			if verifyRes != nil && strings.TrimSpace(verifyRes.ContentSHA256) != "" {
				res.ContentSHA256 = verifyRes.ContentSHA256
			}
			if jsonOut {
				return writeHelmerArchiveJSON(cmd.OutOrStdout(), res)
			}
			if quiet {
				if printSHA && strings.TrimSpace(res.ContentSHA256) != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.ContentSHA256, res.DestinationPath)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), res.DestinationPath)
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Archive %s unpacked to %s (files=%d bytes=%d", path, res.DestinationPath, res.FileCount, res.TotalBytes)
			if strings.TrimSpace(res.ContentSHA256) != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " sha256=%s", res.ContentSHA256)
			}
			fmt.Fprintln(cmd.OutOrStdout(), ")")
			return nil
		},
	}
	cmd.Flags().StringVarP(&destination, "destination", "d", "", "Destination directory for the unpacked chart")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite destination files if they already exist")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Print only the destination path")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print JSON output")
	cmd.Flags().BoolVar(&printSHA, "print-sha", false, "Print archive SHA256 (with --quiet: emits '<sha> <path>')")
	cmd.Flags().Int64Var(&maxStreamBytes, "max-stream-bytes", 512*1024*1024, "Maximum bytes to read when ARCHIVE is '-' (0 = unlimited)")
	cmd.Example = strings.TrimSpace(`
  # Unpack an archive into a directory
  helmer unpack dist/chart.sqlite --destination ./chart-unpacked

  # Unpack from stdin
  helmer unpack - --destination ./chart-unpacked < chart.sqlite
`)
	decorateCommandHelp(cmd, "Unpack Flags")
	return cmd
}

func writeHelmerArchiveJSON(w io.Writer, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(raw))
	return err
}

func writeHelmerArchiveError(cmd *cobra.Command, jsonOut bool, err error) error {
	if !jsonOut {
		return err
	}
	_ = writeHelmerArchiveJSON(cmd.OutOrStdout(), map[string]any{
		"success": false,
		"error":   err.Error(),
	})
	return err
}

func materializeHelmerArchiveInput(path string, maxBytes int64) (string, func(), error) {
	if strings.TrimSpace(path) != "-" {
		return path, func() {}, nil
	}
	temp, err := os.CreateTemp("", "helmer-archive-stdin-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp archive: %w", err)
	}
	cleanup := func() { _ = os.Remove(temp.Name()) }
	if maxBytes <= 0 {
		maxBytes = 0
	}
	var r io.Reader = os.Stdin
	if maxBytes > 0 {
		r = io.LimitReader(os.Stdin, maxBytes+1)
	}
	written, err := io.Copy(temp, r)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("read stdin: %w", err)
	}
	if maxBytes > 0 && written > maxBytes {
		cleanup()
		return "", nil, fmt.Errorf("stream exceeds --max-stream-bytes (%d bytes)", maxBytes)
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temp archive: %w", err)
	}
	return temp.Name(), cleanup, nil
}

func streamHelmerArchiveFile(path string, w io.Writer, maxBytes int64) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()
	var r io.Reader = f
	if maxBytes > 0 {
		r = io.LimitReader(f, maxBytes+1)
	}
	written, err := io.Copy(w, r)
	if err != nil {
		return fmt.Errorf("stream archive: %w", err)
	}
	if maxBytes > 0 && written > maxBytes {
		return fmt.Errorf("stream exceeds --max-stream-bytes (%d bytes)", maxBytes)
	}
	return nil
}
