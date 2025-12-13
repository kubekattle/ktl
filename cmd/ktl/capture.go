// capture.go adds 'ktl logs capture', wrapping the tailer so investigations can persist multi-pod log sessions plus metadata into portable archives.
package main

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/kube"
	"github.com/spf13/cobra"
)

func newCaptureCommand(kubeconfig *string, kubeContext *string, logLevel *string) *cobra.Command {
	opts := config.NewOptions()
	capOpts := capture.NewOptions()

	cmd := &cobra.Command{
		Use:   "capture [POD_QUERY]",
		Short: "Record logs, events, and workload state into a replayable archive",
		Long:  `capture spins up the regular ktl tailer plus metadata informers, enriches each log line with pod/node/deployment state, and packages everything (logs.jsonl, metadata, optional SQLite mirror) into a single tarball for offline replay.`,
		Args:  cobra.MaximumNArgs(1),
		Example: `  # Record 3 minutes of checkout pod logs and store the capture in dist/
  ktl logs capture 'checkout-.*' --namespace prod-payments --duration 3m --capture-output dist/checkout.tar.gz

  # Capture all namespaces and keep a SQLite mirror for replay filters
  ktl logs capture . -A --capture-sqlite`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
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
			stopProgress()
			if err != nil {
				return err
			}
			cmd.Printf("Capture artifact written to %s\n", artifact)
			return nil
		},
	}

	opts.AddFlags(cmd)
	capOpts.AddFlags(cmd)
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Capture Flags")
	cmd.AddCommand(
		newCaptureReplayCommand(),
		newCaptureDiffCommand(kubeconfig, kubeContext),
	)

	return cmd
}

func startCaptureProgress(w io.Writer, interval time.Duration) func() {
	if w == nil {
		return func() {}
	}
	done := make(chan struct{})
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
			case <-done:
				fmt.Fprint(w, "\rCapture complete       \n")
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
	}
}

func addLegacyCaptureCommand(root *cobra.Command, kubeconfig *string, kubeContext *string, logLevel *string) *cobra.Command {
	legacy := newCaptureCommand(kubeconfig, kubeContext, logLevel)
	legacy.Hidden = true
	legacy.Deprecated = "use 'ktl logs capture'"
	root.AddCommand(legacy)
	return legacy
}
