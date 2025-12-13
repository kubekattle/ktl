// logs.go defines the top-level 'ktl logs' command, connecting CLI flags to the tailer, capture, drift, and streaming subcommands.
package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/tailer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	ctrl "sigs.k8s.io/controller-runtime/pkg/log"
)

func newLogsCommand(opts *config.Options, kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "logs [POD_QUERY]",
		Aliases:       []string{"tail"},
		Short:         "Tail Kubernetes pod logs",
		Long:          "Stream pod logs with ktl's high-performance tailer. Accepts the same query/flag set as the legacy ktl entrypoint.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, args, opts, kubeconfigPath, kubeContext, logLevel)
		},
	}

	opts.AddFlags(cmd)
	registerNamespaceCompletion(cmd, "namespace", kubeconfigPath, kubeContext)
	decorateCommandHelp(cmd, "Log Flags")
	return cmd
}

func runLogs(cmd *cobra.Command, args []string, opts *config.Options, kubeconfigPath *string, kubeContext *string, logLevel *string) error {
	if requestedHelp(opts.UIAddr) || requestedHelp(opts.WSListenAddr) {
		return cmd.Help()
	}
	if len(args) > 0 && requestedHelp(args[0]) {
		return cmd.Help()
	}
	hasFilters := hasNonGlobalFlag(cmd.Flags())
	if len(args) == 0 && !hasFilters {
		if strings.TrimSpace(opts.UIAddr) != "" || strings.TrimSpace(opts.WSListenAddr) != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "Provide a POD_QUERY or filter flags before using --ui/--ws-listen so there is something to mirror.")
		}
		return cmd.Help()
	}

	opts.KubeConfigPath = *kubeconfigPath
	opts.Context = *kubeContext
	if len(args) > 0 {
		opts.PodQuery = args[0]
	}
	if err := opts.Validate(); err != nil {
		return err
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
	clusterInfo := describeClusterLabel(kubeClient, *kubeContext)
	if addr := strings.TrimSpace(opts.UIAddr); addr != "" {
		uiServer := caststream.New(
			addr,
			caststream.ModeWeb,
			clusterInfo,
			logger.WithName("ui"),
			caststream.WithoutClusterInfo(),
			caststream.WithoutLogTitle(),
		)
		if err := startCastServer(ctx, uiServer, "ktl log UI", logger.WithName("ui"), cmd.ErrOrStderr()); err != nil {
			return err
		}
		tailerOpts = append(tailerOpts, tailer.WithLogObserver(uiServer))
		fmt.Fprintf(cmd.ErrOrStderr(), "Serving ktl log UI on %s\n", addr)
	}
	if addr := strings.TrimSpace(opts.WSListenAddr); addr != "" {
		wsServer := caststream.New(addr, caststream.ModeWS, clusterInfo, logger.WithName("wscast"))
		if err := startCastServer(ctx, wsServer, "ktl log websocket stream", logger.WithName("wscast"), cmd.ErrOrStderr()); err != nil {
			return err
		}
		tailerOpts = append(tailerOpts, tailer.WithLogObserver(wsServer))
		fmt.Fprintf(cmd.ErrOrStderr(), "Serving ktl websocket stream on %s\n", addr)
	}

	t, err := tailer.New(kubeClient.Clientset, opts, logger, tailerOpts...)
	if err != nil {
		return err
	}

	return t.Run(ctx)
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
