// File: cmd/ktl/logs.go
// Brief: CLI command wiring and implementation for 'logs'.

// logs.go defines the top-level 'ktl logs' command, connecting CLI flags to the tailer, capture, drift, and streaming subcommands.
package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/grpcutil"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/mirrorbus"
	"github.com/example/ktl/internal/tailer"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
)

func newLogsCommand(opts *config.Options, kubeconfigPath *string, kubeContext *string, logLevel *string, remoteAgent *string, mirrorBus *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "logs [POD_QUERY]",
		Aliases:       []string{"tail"},
		Short:         "Tail Kubernetes pod logs",
		Long:          "Stream pod logs with ktl's high-performance tailer. Accepts the same query/flag set as the legacy ktl entrypoint.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, args, opts, kubeconfigPath, kubeContext, logLevel, remoteAgent, mirrorBus)
		},
	}

	opts.AddFlags(cmd)
	registerNamespaceCompletion(cmd, "namespace", kubeconfigPath, kubeContext)
	decorateCommandHelp(cmd, "Log Flags")
	return cmd
}

func runLogs(cmd *cobra.Command, args []string, opts *config.Options, kubeconfigPath *string, kubeContext *string, logLevel *string, remoteAgent *string, mirrorBus *string) error {
	if requestedHelp(opts.UIAddr) || requestedHelp(opts.WSListenAddr) {
		return cmd.Help()
	}
	if len(args) > 0 && requestedHelp(args[0]) {
		return cmd.Help()
	}
	hasFilters := hasNonGlobalFlag(cmd.Flags())
	if len(args) == 0 && !hasFilters {
		// Allow launching the UI as an "idle" control surface so responders can start
		// a capture from the browser without committing to a pod query up front.
		if strings.TrimSpace(opts.UIAddr) == "" {
			if strings.TrimSpace(opts.WSListenAddr) != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Provide a POD_QUERY or filter flags before using --ws-listen so there is something to mirror.")
			}
			return cmd.Help()
		}
	}

	opts.KubeConfigPath = *kubeconfigPath
	opts.Context = *kubeContext
	if len(args) > 0 {
		opts.PodQuery = args[0]
	}
	if err := opts.Validate(); err != nil {
		return err
	}
	remoteAddr := ""
	if remoteAgent != nil {
		remoteAddr = strings.TrimSpace(*remoteAgent)
	}
	if remoteAddr != "" {
		return runRemoteLogs(cmd, opts, remoteAddr)
	}
	logger, err := buildLogger(*logLevel)
	if err != nil {
		return err
	}
	ctrl.SetLogger(logger)

	ctx := cmd.Context()
	if opts.Stdin {
		return streamFromStdin(ctx, opts)
	}
	kubeClient, err := kube.New(ctx, opts.KubeConfigPath, opts.Context)
	if err != nil {
		return err
	}
	if !opts.AllNamespaces && len(opts.Namespaces) == 0 && kubeClient.Namespace != "" {
		opts.Namespaces = []string{kubeClient.Namespace}
		if !cmd.Flags().Changed("namespace") {
			fmt.Fprintf(cmd.ErrOrStderr(), "Defaulting to namespace %s from the active kubeconfig context\n", kubeClient.Namespace)
		}
	}

	var (
		tailerOpts []tailer.Option
	)
	var mirrorPublisher io.Closer
	if mirrorBus != nil {
		if addr := strings.TrimSpace(*mirrorBus); addr != "" {
			sessionID := fmt.Sprintf("logs-%d", time.Now().UnixNano())
			fmt.Fprintf(cmd.ErrOrStderr(), "Publishing logs to mirror bus %s (session %s)\n", addr, sessionID)
			pub, err := mirrorbus.NewPublisher(ctx, addr, sessionID, "logs")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Mirror bus unavailable: %v\n", err)
			} else {
				mirrorPublisher = pub
				tailerOpts = append(tailerOpts, tailer.WithLogObserver(pub))
				fmt.Fprintf(cmd.ErrOrStderr(), "Share via: ktl mirror proxy --bus %s --session %s\n", addr, sessionID)
			}
		}
	}
	defer func() {
		if mirrorPublisher != nil {
			_ = mirrorPublisher.Close()
		}
	}()
	clusterInfo := describeClusterLabel(kubeClient, *kubeContext)
	if addr := strings.TrimSpace(opts.UIAddr); addr != "" {
		uiCapOpts := capture.NewOptions()
		uiCapOpts.Duration = 24 * time.Hour
		uiCapOpts.SQLite = true
		uiCapOpts.AttachDescribe = true
		uiCapOpts.AttachManifests = true
		captureCtrl := newUICaptureController(ctx, kubeClient, opts, uiCapOpts, logger.WithName("ui-capture"))
		uiServer := caststream.New(
			addr,
			caststream.ModeWeb,
			clusterInfo,
			logger.WithName("ui"),
			caststream.WithoutClusterInfo(),
			caststream.WithoutLogTitle(),
			caststream.WithCaptureController(captureCtrl),
		)
		if err := castutil.StartCastServer(ctx, uiServer, "ktl log UI", logger.WithName("ui"), cmd.ErrOrStderr()); err != nil {
			return err
		}
		tailerOpts = append(tailerOpts, tailer.WithLogObserver(uiServer))
		fmt.Fprintf(cmd.ErrOrStderr(), "Serving ktl log UI on %s\n", addr)
	}
	if addr := strings.TrimSpace(opts.WSListenAddr); addr != "" {
		wsServer := caststream.New(addr, caststream.ModeWS, clusterInfo, logger.WithName("wscast"))
		if err := castutil.StartCastServer(ctx, wsServer, "ktl log websocket stream", logger.WithName("wscast"), cmd.ErrOrStderr()); err != nil {
			return err
		}
		tailerOpts = append(tailerOpts, tailer.WithLogObserver(wsServer))
		fmt.Fprintf(cmd.ErrOrStderr(), "Serving ktl websocket stream on %s\n", addr)
	}

	if len(args) == 0 && !hasFilters {
		fmt.Fprintln(cmd.ErrOrStderr(), "UI is running without a live log stream; use the Start capture button to record an archive.")
		<-ctx.Done()
		return nil
	}

	t, err := tailer.New(kubeClient.Clientset, opts, logger, tailerOpts...)
	if err != nil {
		return err
	}
	return t.Run(ctx)
}

func runRemoteLogs(cmd *cobra.Command, opts *config.Options, remoteAddr string) error {
	ctx := cmd.Context()
	conn, err := grpcutil.Dial(ctx, remoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	client := apiv1.NewLogServiceClient(conn)
	req := convert.LogOptionsToProto(opts)
	stream, err := client.StreamLogs(ctx, req)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for {
		line, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rec := convert.FromProtoLogLine(line)
		payload := rec.Rendered
		if payload == "" {
			payload = rec.Raw
		}
		if payload == "" {
			continue
		}
		fmt.Fprintln(out, payload)
	}
}

func hasNonGlobalFlag(fs *pflag.FlagSet) bool {
	if fs == nil {
		return false
	}
	allowed := map[string]struct{}{
		"kubeconfig": {},
		"context":    {},
		"log-level":  {},
	}
	found := false
	fs.Visit(func(f *pflag.Flag) {
		if _, ok := allowed[f.Name]; ok {
			return
		}
		found = true
	})
	return found
}

func describeClusterLabel(client *kube.Client, contextName string) string {
	ctx := strings.TrimSpace(contextName)
	if ctx == "" {
		ctx = "current context"
	}
	host := ""
	if client != nil && client.RESTConfig != nil {
		host = strings.TrimSpace(client.RESTConfig.Host)
	}
	if host == "" {
		return fmt.Sprintf("Context: %s", ctx)
	}
	return fmt.Sprintf("Context: %s · API: %s", ctx, host)
}

func requestedHelp(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	switch value {
	case "-":
		return true
	case "-h", "--help", "-help", "help":
		return true
	default:
		return false
	}
}
