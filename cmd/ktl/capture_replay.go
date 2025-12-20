// capture_replay.go wires 'ktl logs capture replay', streaming stored log archives back through the formatter for offline triage.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/tailer"
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
	var uiAddr string
	var wsListenAddr string
	var noStdout bool

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

			level, _ := cmd.Flags().GetString("log-level")
			logger, err := buildLogger(level)
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			uiAddr = strings.TrimSpace(uiAddr)
			wsListenAddr = strings.TrimSpace(wsListenAddr)
			hasUI := uiAddr != "" || wsListenAddr != ""
			if hasUI {
				meta, metaErr := capture.LoadMetadata(artifact)
				clusterInfo := ""
				if metaErr == nil && meta != nil {
					name := strings.TrimSpace(meta.SessionName)
					if name == "" {
						name = filepath.Base(artifact)
					}
					ended := meta.EndedAt
					if ended.IsZero() {
						ended = meta.StartedAt.Add(time.Duration(meta.DurationSeconds * float64(time.Second)))
					}
					clusterInfo = fmt.Sprintf(
						"Capture %s · %s → %s · %d pods",
						name,
						meta.StartedAt.Format(time.RFC3339),
						ended.Format(time.RFC3339),
						meta.PodCount,
					)
				}
				if clusterInfo == "" {
					clusterInfo = fmt.Sprintf("Capture replay · %s", filepath.Base(artifact))
				}
				title := "ktl Capture Replay"
				replay := func(ctx context.Context, send func(tailer.LogRecord) error) error {
					return capture.ReplayEntries(ctx, artifact, opts, func(entry capture.Entry) error {
						rec := tailer.LogRecord{
							Timestamp:          entry.Timestamp,
							FormattedTimestamp: entry.FormattedTimestamp,
							Namespace:          entry.Namespace,
							Pod:                entry.Pod,
							Container:          entry.Container,
							Raw:                entry.Raw,
							Rendered:           entry.Rendered,
							Source:             "capture",
							SourceGlyph:        "●",
						}
						return send(rec)
					})
				}
				if uiAddr != "" {
					uiServer := caststream.New(uiAddr, caststream.ModeWeb, clusterInfo, logger.WithName("capture-ui"), caststream.WithLogTitle(title), caststream.WithLogReplay(replay))
					if err := castutil.StartCastServer(ctx, uiServer, "ktl capture replay UI", logger.WithName("capture-ui"), cmd.ErrOrStderr()); err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Serving capture replay UI on %s\n", uiAddr)
				}
				if wsListenAddr != "" {
					wsServer := caststream.New(wsListenAddr, caststream.ModeWS, clusterInfo, logger.WithName("capture-ws"), caststream.WithLogReplay(replay))
					if err := castutil.StartCastServer(ctx, wsServer, "ktl capture replay websocket stream", logger.WithName("capture-ws"), cmd.ErrOrStderr()); err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "Serving capture replay websocket stream on %s\n", wsListenAddr)
				}
			}
			if !noStdout {
				if err := capture.Replay(ctx, artifact, opts, os.Stdout); err != nil {
					return err
				}
			}
			if hasUI {
				fmt.Fprintln(cmd.ErrOrStderr(), "Replay complete; press Ctrl+C to stop the viewer")
				<-ctx.Done()
				return nil
			}
			return nil
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
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve the capture replay log viewer at this address (e.g. :8080)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8080"
	}
	cmd.Flags().StringVar(&wsListenAddr, "ws-listen", "", "Expose a raw WebSocket replay feed at this address (e.g. :9080)")
	cmd.Flags().BoolVar(&noStdout, "no-stdout", false, "Do not replay logs to stdout (use with --ui/--ws-listen)")
	decorateCommandHelp(cmd, "Replay Flags")

	return cmd
}
