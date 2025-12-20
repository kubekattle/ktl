// File: cmd/ktl/capture.go
// Brief: CLI command wiring and implementation for 'capture'.

// capture.go adds 'ktl logs capture', wrapping the tailer so investigations can persist multi-pod log sessions plus metadata into portable archives.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/grpcutil"
	"github.com/example/ktl/internal/kube"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newCaptureCommand(kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	opts := config.NewOptions()
	capOpts := capture.NewOptions()

	cmd := &cobra.Command{
		Use:   "capture [POD_QUERY]",
		Short: "Record logs, events, and workload state into a replayable archive",
		Long:  `capture spins up the regular ktl tailer plus metadata informers, enriches each log line with pod/node/deployment state, and packages everything (logs.jsonl, metadata, optional SQLite mirror) into a single tarball for offline replay.`,
		Args:  cobra.MaximumNArgs(1),
		Example: `  # Record 3 minutes of checkout pod logs and store the capture in dist/
  ktl logs capture 'checkout-.*' --namespace prod-payments --duration 3m --capture-output dist/checkout.tar.gz

  # Capture all namespaces (includes logs.sqlite + context attachments by default)
  ktl logs capture . -A

  # Launch the capture console UI (start/stop captures from the browser)
  ktl logs capture --ui`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if requestedHelp(opts.UIAddr) {
				return cmd.Help()
			}
			if len(args) > 0 {
				opts.PodQuery = args[0]
			}
			opts.KubeConfigPath = *kubeconfig
			opts.Context = *kubeContext
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := capOpts.Validate(); err != nil {
				return err
			}
			if addr := strings.TrimSpace(opts.UIAddr); addr != "" {
				// UI mode acts as an interactive capture console: the browser triggers start/stop and
				// renders the resulting replay HTML once the session completes.
				if !cmd.Flags().Changed("duration") {
					capOpts.Duration = 24 * time.Hour
				}
				if !cmd.Flags().Changed("capture-sqlite") {
					capOpts.SQLite = true
				}
				if !cmd.Flags().Changed("attach-describe") {
					capOpts.AttachDescribe = true
				}
				if !cmd.Flags().Changed("attach-manifests") {
					capOpts.AttachManifests = true
				}

				logger, err := buildLogger(*logLevel)
				if err != nil {
					return err
				}
				kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
				if err != nil {
					return err
				}
				if !opts.AllNamespaces && len(opts.Namespaces) == 0 && kubeClient.Namespace != "" {
					opts.Namespaces = []string{kubeClient.Namespace}
				}
				clusterInfo := describeClusterLabel(kubeClient, *kubeContext)
				captureCtrl := newUICaptureController(ctx, kubeClient, opts, capOpts, logger.WithName("ui-capture"))
				uiServer := caststream.New(
					addr,
					caststream.ModeWeb,
					clusterInfo,
					logger.WithName("ui"),
					caststream.WithoutClusterInfo(),
					caststream.WithoutLogTitle(),
					caststream.WithCaptureController(captureCtrl),
				)
				if err := castutil.StartCastServer(ctx, uiServer, "ktl capture UI", logger.WithName("ui"), cmd.ErrOrStderr()); err != nil {
					return err
				}
				cmd.Printf("Serving ktl capture UI on %s\n", addr)
				<-ctx.Done()
				return nil
			}
			if remoteAgent != nil && strings.TrimSpace(*remoteAgent) != "" {
				return runRemoteCapture(cmd, opts, capOpts, strings.TrimSpace(*remoteAgent))
			}
			logger, err := buildLogger(*logLevel)
			if err != nil {
				return err
			}
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			if !opts.AllNamespaces && len(opts.Namespaces) == 0 && kubeClient.Namespace != "" {
				opts.Namespaces = []string{kubeClient.Namespace}
			}
			session, err := capture.NewSession(kubeClient, opts, capOpts, logger)
			if err != nil {
				return err
			}
			cmd.Printf("Starting capture window (%s) — press Ctrl+C to cancel\n", capOpts.Duration)
			stopProgress := startCaptureProgress(cmd.ErrOrStderr(), 500*time.Millisecond)
			artifact, err := session.Run(ctx)
			canceled := errors.Is(ctx.Err(), context.Canceled)
			success := err == nil && !canceled
			stopProgress(success)
			if artifact != "" {
				if !success {
					cmd.Printf("Partial capture artifact written to %s\n", artifact)
				} else {
					cmd.Printf("Capture artifact written to %s\n", artifact)
				}
			}
			if success && artifact != "" && capOpts.AttachManifests {
				fmt.Fprintln(cmd.ErrOrStderr(), "Warning: capture artifacts include manifests/describes by default and may contain sensitive configuration. Review before sharing: README.md#review-before-sharing")
			}
			if canceled {
				return nil
			}
			return err
		},
	}

	logFlagNames := opts.BindFlags(cmd.Flags())
	hideFlags(cmd.Flags(), logFlagNames)
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.Hidden = false
	}
	capOpts.AddFlags(cmd)
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Capture Flags")
	cmd.AddCommand(
		newCaptureReplayCommand(),
		newCaptureAnalyzeCommand(),
		newCaptureSliceCommand(),
		newCaptureDiffCommand(kubeconfig, kubeContext),
	)

	return cmd
}

func startCaptureProgress(w io.Writer, interval time.Duration) func(success bool) {
	if w == nil {
		return func(bool) {}
	}
	done := make(chan string, 1)
	var once sync.Once
	go func() {
		frames := []string{"Capturing   ", "Capturing.  ", "Capturing.. ", "Capturing..."}
		idx := 0
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		fmt.Fprint(w, frames[idx])
		for {
			select {
			case <-ticker.C:
				idx = (idx + 1) % len(frames)
				fmt.Fprintf(w, "\r%s", frames[idx])
			case msg := <-done:
				fmt.Fprintf(w, "\r%s\n", msg)
				return
			}
		}
	}()
	return func(success bool) {
		once.Do(func() {
			if success {
				done <- "Capture complete       "
			} else {
				done <- "Capture stopped        "
			}
			close(done)
		})
	}
}

func runRemoteCapture(cmd *cobra.Command, opts *config.Options, capOpts *capture.Options, remoteAddr string) error {
	ctx := cmd.Context()
	conn, err := grpcutil.Dial(ctx, remoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := apiv1.NewCaptureServiceClient(conn)

	req := &apiv1.CaptureRequest{
		Log: convert.LogOptionsToProto(opts),
		Capture: convert.CaptureToProto(convert.CaptureConfig{
			Duration:       capOpts.Duration,
			OutputName:     capOpts.OutputPath,
			SQLite:         capOpts.SQLite,
			AttachDescribe: capOpts.AttachDescribe,
			SessionName:    capOpts.SessionName,
		}),
	}
	stream, err := client.RunCapture(ctx, req)
	if err != nil {
		return err
	}
	outputPath := capOpts.ResolveOutputPath(time.Now())
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if chunk.GetError() != "" {
			return errors.New(chunk.GetError())
		}
		if data := chunk.GetData(); len(data) > 0 {
			if _, writeErr := file.Write(data); writeErr != nil {
				return writeErr
			}
		}
		if chunk.GetLast() {
			break
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Remote capture artifact saved to %s\n", outputPath)
	return nil
}
