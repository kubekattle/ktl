package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/spf13/cobra"
)

func newCaptureSliceCommand() *cobra.Command {
	var namespaces []string
	var pods []string
	var containers []string
	var grep []string
	var sinceStr string
	var untilStr string
	var limit int
	var outputPath string

	cmd := &cobra.Command{
		Use:   "slice <CAPTURE_TARBALL|CAPTURE_DIR>",
		Short: "Write a smaller capture artifact from a filtered replay",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifact := args[0]
			opts := capture.ReplayOptions{
				Namespaces: namespaces,
				Pods:       pods,
				Containers: containers,
				Grep:       grep,
				Limit:      limit,
				PreferJSON: true,
			}
			if sinceStr != "" {
				ts, err := time.Parse(time.RFC3339, sinceStr)
				if err != nil {
					return fmt.Errorf("parse --since: %w", err)
				}
				opts.Since = ts
			}
			if untilStr != "" {
				ts, err := time.Parse(time.RFC3339, untilStr)
				if err != nil {
					return fmt.Errorf("parse --until: %w", err)
				}
				opts.Until = ts
			}
			path := strings.TrimSpace(outputPath)
			if path == "" {
				path = fmt.Sprintf("ktl-capture-slice-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
			}
			out, err := capture.Slice(cmd.Context(), artifact, path, opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Slice written to %s\n", out)
			return nil
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Filter by namespace")
	cmd.Flags().StringSliceVar(&pods, "pod", nil, "Filter by pod name")
	cmd.Flags().StringSliceVar(&containers, "container", nil, "Filter by container name")
	cmd.Flags().StringSliceVar(&grep, "grep", nil, "Substring match required in raw or rendered output (case-insensitive)")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Only include logs with timestamp >= this RFC3339 value")
	cmd.Flags().StringVar(&untilStr, "until", "", "Only include logs with timestamp <= this RFC3339 value")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of log lines to include (0 = no limit)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write the sliced capture to this path (defaults to ./ktl-capture-slice-<timestamp>.tar.gz)")
	decorateCommandHelp(cmd, "Slice Flags")

	return cmd
}
