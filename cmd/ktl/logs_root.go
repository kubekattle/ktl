// File: cmd/ktl/logs_root.go
// Brief: Logs-only ktl CLI entrypoint wiring.

package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/featureflags"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
)

func newLogsRootCommand() *cobra.Command {
	initKlogFlags()

	opts := config.NewOptions()
	var kubeconfigPath string
	var kubeContext string
	logLevel := "info"
	var kubeLogLevel int
	var noColor bool
	var featureFlagValues []string
	var remoteAgentAddr string
	var remoteToken string
	var remoteTLS bool
	var remoteInsecure bool
	var remoteCA string
	var remoteCert string
	var remoteKey string
	var remoteServerName string
	var mirrorBusAddr string

	cmd := newLogsCommand(opts, &kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr, &remoteToken, &remoteTLS, &remoteInsecure, &remoteCA, &remoteCert, &remoteKey, &remoteServerName, &mirrorBusAddr)
	cmd.Use = "ktl-logs [POD_QUERY]"
	cmd.Short = "Tail Kubernetes pod logs"
	cmd.Long = "Stream pod logs with ktl's high-performance tailer."
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommand(newHelpCommand(cmd))
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n\n", err)
		}
		return pflag.ErrHelp
	})
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if commandNamespaceHelpRequested(cmd) {
			return pflag.ErrHelp
		}
		if kubeLogLevel == 0 {
			if val := strings.TrimSpace(os.Getenv("KTL_KUBE_LOG_LEVEL")); val != "" {
				if n, err := strconv.Atoi(val); err == nil {
					kubeLogLevel = n
				} else {
					return fmt.Errorf("invalid KTL_KUBE_LOG_LEVEL %q: %w", val, err)
				}
			} else if shouldLogAtLevel(logLevel, zapcore.DebugLevel) {
				kubeLogLevel = 6
			}
		}
		if kubeLogLevel > 0 {
			_ = flag.CommandLine.Set("v", strconv.Itoa(kubeLogLevel))
			_ = flag.CommandLine.Set("logtostderr", "true")
			_ = flag.CommandLine.Set("alsologtostderr", "true")
		}
		switch strings.TrimSpace(logLevel) {
		case "-h", "--help":
			return pflag.ErrHelp
		}
		if noColor || os.Getenv("NO_COLOR") != "" {
			color.NoColor = true
			_ = os.Setenv("NO_COLOR", "1")
		}
		flags, err := featureflags.Resolve(featureFlagValues, featureflags.EnabledFromEnv(nil))
		if err != nil {
			return err
		}
		ctx := featureflags.ContextWithFlags(cmd.Context(), flags)
		cmd.SetContext(ctx)
		return nil
	}

	cmd.PersistentFlags().StringVarP(&kubeconfigPath, "kubeconfig", "k", "", "Path to the kubeconfig file to use for CLI requests")
	cmd.PersistentFlags().StringVarP(&kubeContext, "context", "K", "", "Name of the kubeconfig context to use")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", logLevel, "Log level for ktl output (debug, info, warn, error)")
	cmd.PersistentFlags().IntVar(&kubeLogLevel, "kube-log-level", 0, "Kubernetes client-go verbosity (klog -v); at >=6 enables HTTP request/response tracing; can also set KTL_KUBE_LOG_LEVEL")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	cmd.PersistentFlags().StringSliceVar(&featureFlagValues, "feature", nil, "Enable experimental ktl features (repeat or pass comma-separated names)")
	if err := cmd.PersistentFlags().MarkHidden("feature"); err != nil {
		cobra.CheckErr(err)
	}
	cmd.PersistentFlags().StringVar(&remoteAgentAddr, "remote-agent", "", "Forward ktl logs operations to a remote ktl-agent gRPC endpoint")
	cmd.PersistentFlags().StringVar(&remoteToken, "remote-token", "", "Authentication token for remote gRPC endpoints (also via KTL_REMOTE_TOKEN)")
	cmd.PersistentFlags().BoolVar(&remoteTLS, "remote-tls", false, "Use TLS for remote gRPC endpoints (also via KTL_REMOTE_TLS=1)")
	cmd.PersistentFlags().BoolVar(&remoteInsecure, "remote-tls-insecure-skip-verify", false, "Skip TLS verification for remote gRPC (also via KTL_REMOTE_TLS_INSECURE_SKIP_VERIFY=1)")
	cmd.PersistentFlags().StringVar(&remoteCA, "remote-tls-ca", "", "CA bundle PEM file for remote gRPC TLS (also via KTL_REMOTE_TLS_CA)")
	cmd.PersistentFlags().StringVar(&remoteCert, "remote-tls-client-cert", "", "Client certificate PEM file for remote gRPC mTLS (also via KTL_REMOTE_TLS_CLIENT_CERT)")
	cmd.PersistentFlags().StringVar(&remoteKey, "remote-tls-client-key", "", "Client private key PEM file for remote gRPC mTLS (also via KTL_REMOTE_TLS_CLIENT_KEY)")
	cmd.PersistentFlags().StringVar(&remoteServerName, "remote-tls-server-name", "", "Override remote gRPC TLS server name (also via KTL_REMOTE_TLS_SERVER_NAME)")
	cmd.PersistentFlags().StringVar(&mirrorBusAddr, "mirror-bus", "", "Publish mirror payloads to a shared gRPC bus (ktl-agent MirrorService)")

	bindViper(cmd)

	return cmd
}
