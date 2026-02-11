package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/example/ktl/internal/chartarchive"
	"github.com/example/ktl/internal/version"
	"github.com/spf13/cobra"
)

func newPackageCommand() *cobra.Command {
	var outputPath string
	var force bool
	var quiet bool
	var jsonOut bool
	var verifyPath string
	var unpackPath string
	var unpackDest string
	var maxStreamBytes int64
	var printSHA bool
	logLevel := "info"
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "package [CHART_DIR]",
		Short:         "Package a chart directory into a sqlite chart archive",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				fmt.Fprintf(cmd.OutOrStdout(), "package %s\n", version.Version)
				return nil
			}
			if quiet && jsonOut {
				return fmt.Errorf("--quiet and --json are mutually exclusive")
			}
			if strings.TrimSpace(verifyPath) != "" && strings.TrimSpace(unpackPath) != "" {
				return fmt.Errorf("--verify and --unpack cannot be used together")
			}
			if strings.TrimSpace(unpackPath) != "" && len(args) > 0 {
				return fmt.Errorf("chart dir should not be provided with --unpack")
			}

			// Materialize stdin to a temp file when users pass "-" to streaming flags.
			cleanup := func() {}
			materializeIfStdin := func(path string) (string, error) {
				if strings.TrimSpace(path) != "-" {
					return path, nil
				}
				temp, err := materializeStdin(maxStreamBytes)
				if err != nil {
					return "", err
				}
				cleanup = func() { _ = os.Remove(temp) }
				return temp, nil
			}

			if p := strings.TrimSpace(verifyPath); p != "" {
				p, err := materializeIfStdin(p)
				if err != nil {
					return err
				}
				defer cleanup()
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

			if p := strings.TrimSpace(unpackPath); p != "" {
				p, err := materializeIfStdin(p)
				if err != nil {
					return err
				}
				defer cleanup()

				verifyRes, err := chartarchive.VerifyArchive(cmd.Context(), p)
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
				res, err := chartarchive.UnpackArchive(cmd.Context(), p, chartarchive.UnpackOptions{
					DestinationPath: unpackDest,
					Force:           force,
					Workers:         0,
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
				// propagate sha256 if present from verify.
				if verifyRes != nil && strings.TrimSpace(verifyRes.ContentSHA256) != "" {
					res.ContentSHA256 = verifyRes.ContentSHA256
				}
				if jsonOut {
					raw, _ := json.Marshal(res)
					fmt.Fprintln(cmd.OutOrStdout(), string(raw))
					return nil
				}
				if quiet {
					if printSHA && strings.TrimSpace(res.ContentSHA256) != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", res.ContentSHA256, res.DestinationPath)
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), res.DestinationPath)
					}
					return nil
				}
				sha := strings.TrimSpace(res.ContentSHA256)
				if sha == "" && verifyRes != nil {
					sha = strings.TrimSpace(verifyRes.ContentSHA256)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Archive %s unpacked to %s (files=%d bytes=%d", p, res.DestinationPath, res.FileCount, res.TotalBytes)
				if sha != "" {
					fmt.Fprintf(cmd.OutOrStdout(), " sha256=%s", sha)
				}
				fmt.Fprintln(cmd.OutOrStdout(), ")")
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
			if strings.TrimSpace(outputPath) == "-" {
				if err := streamFileToWriter(res.ArchivePath, cmd.OutOrStdout(), maxStreamBytes); err != nil {
					return err
				}
				_ = os.Remove(res.ArchivePath)
				return nil
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
	cmd.Flags().StringVar(&unpackPath, "unpack", "", "Unpack an existing sqlite chart archive (PATH)")
	cmd.Flags().StringVarP(&unpackDest, "destination", "d", "", "Destination directory for --unpack (defaults to chart name/version)")
	cmd.Flags().Int64Var(&maxStreamBytes, "max-stream-bytes", 512*1024*1024, "Maximum bytes to read when --verify/--unpack use '-' (0 = unlimited)")
	cmd.Flags().StringVar(&logLevel, "log-level", logLevel, "Log level: debug|info|warn|error (debug prints extra diagnostics)")
	cmd.Flags().BoolVar(&showVersion, "version", false, "Print version and exit")
	cmd.Example = `  # Package a chart directory (defaults to current directory)
  package ./chart

  # Write to a specific file
  package ./chart --output dist/chart.sqlite

  # Write into a directory (filename derived from Chart.yaml)
  package ./chart --output dist/

  # Verify an existing archive
  package --verify dist/chart.sqlite

  # Unpack an existing archive into ./demo-0.1.0
  package --unpack dist/chart.sqlite
  # Stream archive to stdout (for ssh/kubectl cp)
  package ./chart --output - > chart.sqlite
  # Verify from stdin with a 64MB cap
  package --verify - --max-stream-bytes 67108864 < chart.sqlite

  # Package then verify
  package ./chart --output dist/chart.sqlite && package --verify dist/chart.sqlite`
	return cmd
}

func materializeStdin(maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 0 // unlimited
	}
	tmpFile, err := os.CreateTemp("", "ktl-package-stdin-*")
	if err != nil {
		return "", fmt.Errorf("create temp archive: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmpFile.Name())
		}
	}()

	var r io.Reader = os.Stdin
	if maxBytes > 0 {
		r = io.LimitReader(os.Stdin, maxBytes+1)
	}
	written, err := io.Copy(tmpFile, r)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if maxBytes > 0 && written > maxBytes {
		return "", fmt.Errorf("stream exceeds --max-stream-bytes (%d bytes)", maxBytes)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close temp archive: %w", err)
	}
	return tmpFile.Name(), nil
}

func streamFileToWriter(path string, w io.Writer, maxBytes int64) error {
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
