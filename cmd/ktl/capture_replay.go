// capture_replay.go wires 'ktl logs capture replay', streaming stored log archives back through the formatter for offline triage.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/spf13/cobra"
)

func newCaptureReplayCommand() *cobra.Command {
	var namespaces []string
	var pods []string
	var containers []string
	var grep []string
	var sinceStr string
	var untilStr string
	var limit int
	var preferJSON bool
	var raw bool
	var jsonOut bool
	var desc bool
	var templateStr string
	var follow bool

	cmd := &cobra.Command{
		Use:   "replay <CAPTURE_TARBALL>",
		Short: "Replay a ktl capture artifact locally",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifact := args[0]
			opts := capture.ReplayOptions{
				Namespaces: namespaces,
				Pods:       pods,
				Containers: containers,
				Grep:       grep,
				Limit:      limit,
				PreferJSON: preferJSON,
				Raw:        raw,
				JSON:       jsonOut,
				Desc:       desc,
				Template:   templateStr,
				Follow:     follow,
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
			return capture.Replay(cmd.Context(), artifact, opts, os.Stdout)
		},
	}

	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Filter by namespace")
	cmd.Flags().StringSliceVar(&pods, "pod", nil, "Filter by pod name")
	cmd.Flags().StringSliceVar(&containers, "container", nil, "Filter by container name")
	cmd.Flags().StringSliceVar(&grep, "grep", nil, "Substring match required in raw or rendered output (case-insensitive)")
	cmd.Flags().StringVar(&sinceStr, "since", "", "Only include logs with timestamp >= this RFC3339 value")
	cmd.Flags().StringVar(&untilStr, "until", "", "Only include logs with timestamp <= this RFC3339 value")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of log lines to print (0 = no limit)")
	cmd.Flags().BoolVar(&preferJSON, "prefer-json", false, "Force replay to use logs.jsonl even when logs.sqlite is present")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print the raw log payload instead of rendered output")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit each log line as JSON")
	cmd.Flags().BoolVar(&desc, "desc", false, "Reverse chronological order (only when using SQLite)")
	cmd.Flags().StringVar(&templateStr, "template", "", "Go template for formatted output; fields: Timestamp, FormattedTimestamp, Namespace, Pod, Container, Raw, Rendered")
	cmd.Flags().BoolVar(&follow, "follow", false, "Stream new logs as they are written (only when pointing at a live capture directory)")
	decorateCommandHelp(cmd, "Replay Flags")

	return cmd
}
