// main.go bootstraps ktl: it builds the root Cobra command, wires profiling, and executes with signal-aware contexts.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/featureflags"
	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	normalizedArgs := normalizeOptionalValueArgs(os.Args)
	if len(normalizedArgs) != len(os.Args) {
		os.Args = normalizedArgs
	}
	if err := enforceStrictShortFlags(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	stopProfile := setupProfiling()
	defer stopProfile()

	rootCmd := newRootCommand()
	err := rootCmd.ExecuteContext(ctx)
	handleError(err)
	if err != nil {
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	opts := config.NewOptions()
	var kubeconfigPath string
	var kubeContext string
	logLevel := "info"
	var featureFlagValues []string
	cmd := &cobra.Command{
		Use:           "ktl [POD_QUERY]",
		Short:         "High-performance multi-pod Kubernetes log tailer",
		Long:          "ktl is the Kubernetes Swiss Army Knife with blazing fast startup and advanced filtering.",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if commandNamespaceHelpRequested(cmd) {
				return pflag.ErrHelp
			}
			flags, err := featureflags.Resolve(featureFlagValues, featureflags.EnabledFromEnv(nil))
			if err != nil {
				return err
			}
			ctx := featureflags.ContextWithFlags(cmd.Context(), flags)
			cmd.Root().SetContext(ctx)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, args, opts, &kubeconfigPath, &kubeContext, &logLevel)
		},
	}
	cmd.PersistentFlags().StringVarP(&kubeconfigPath, "kubeconfig", "k", "", "Path to the kubeconfig file to use for CLI requests")
	cmd.PersistentFlags().StringVarP(&kubeContext, "context", "K", "", "Name of the kubeconfig context to use")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", logLevel, "Log level for ktl output (debug, info, warn, error)")
	cmd.PersistentFlags().StringSliceVar(&featureFlagValues, "feature", nil, "Enable experimental ktl features (repeat or pass comma-separated names)")
	if err := cmd.PersistentFlags().MarkHidden("feature"); err != nil {
		cobra.CheckErr(err)
	}
	logFlagNames := opts.BindFlags(cmd.Flags())
	hideFlags(cmd.Flags(), logFlagNames)
	logsCmd := newLogsCommand(opts, &kubeconfigPath, &kubeContext, &logLevel)
	captureCmd := newCaptureCommand(&kubeconfigPath, &kubeContext, &logLevel)
	diffDeploymentsCmd := newDeployDiffCommand(&kubeconfigPath, &kubeContext)
	driftCmd := newDriftCommand(&kubeconfigPath, &kubeContext)
	logsCmd.AddCommand(captureCmd, diffDeploymentsCmd, driftCmd)
	registerNamespaceCompletion(cmd, "namespace", &kubeconfigPath, &kubeContext)
	diagCmd := newDiagCommand(&kubeconfigPath, &kubeContext)
	buildCmd := newBuildCommand()
	composeCmd := newComposeCommand()
	tagCmd := newTagCommand()
	pushCmd := newPushCommand()
	cmd.AddCommand(
		logsCmd,
		diagCmd,
		newAppCommand(&kubeconfigPath, &kubeContext),
		newDBCommand(&kubeconfigPath, &kubeContext),
		newDeployCommand(&kubeconfigPath, &kubeContext, &logLevel),
		newAnalyzeCommand(&kubeconfigPath, &kubeContext),
		newCompletionCommand(cmd),
		buildCmd,
		composeCmd,
		tagCmd,
		pushCmd,
	)
	addLegacyDiagCommands(cmd, &kubeconfigPath, &kubeContext)
	legacyCapture := addLegacyCaptureCommand(cmd, &kubeconfigPath, &kubeContext, &logLevel)
	legacyDeployDiff := newDeployDiffCommand(&kubeconfigPath, &kubeContext)
	legacyDeployDiff.Hidden = true
	legacyDeployDiff.Deprecated = "use 'ktl logs diff-deployments'"
	cmd.AddCommand(legacyDeployDiff)
	legacyDrift := newDriftCommand(&kubeconfigPath, &kubeContext)
	legacyDrift.Hidden = true
	legacyDrift.Deprecated = "use 'ktl logs drift'"
	cmd.AddCommand(legacyDrift)
	cmd.Example = `  # Tail checkout pods in prod-payments and highlight errors
	  ktl logs 'checkout-.*' --namespace prod-payments --highlight ERROR

	  # Capture an incident for offline replay
	  ktl logs capture checkout --namespace prod-payments --duration 5m --capture-output dist/checkout.tar.gz

	  # Stream nodes view with kubeconfig context
	  ktl diag nodes --context staging`
	decorateCommandHelp(cmd, "Global Flags")
	bindViper(cmd, logsCmd, captureCmd, diffDeploymentsCmd, driftCmd, legacyCapture, legacyDeployDiff, legacyDrift, buildCmd, composeCmd, tagCmd, pushCmd)
	return cmd
}

func bindViper(commands ...*cobra.Command) {
	if len(commands) == 0 {
		return
	}
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.SetEnvPrefix("KTL")
	v.AutomaticEnv()
	configFile := os.Getenv("KTL_CONFIG")
	configureConfigFile(v, configFile)

	cobra.OnInitialize(func() {
		for _, cmd := range commands {
			if err := v.BindPFlags(cmd.Flags()); err != nil {
				cobra.CheckErr(err)
			}
			if err := v.BindPFlags(cmd.PersistentFlags()); err != nil {
				cobra.CheckErr(err)
			}
		}
		if err := readConfigFile(v, configFile != ""); err != nil {
			cobra.CheckErr(err)
		}
		for _, cmd := range commands {
			flagSets := []*pflag.FlagSet{cmd.Flags(), cmd.PersistentFlags()}
			for _, fs := range flagSets {
				fs.VisitAll(func(f *pflag.Flag) {
					if f.Changed {
						return
					}
					if !v.IsSet(f.Name) {
						return
					}
					val := fmt.Sprintf("%v", v.Get(f.Name))
					if val != "" {
						_ = f.Value.Set(val)
					}
				})
			}
		}
	})
}

func handleError(err error) {
	if err == nil || errors.Is(err, pflag.ErrHelp) {
		return
	}
	message := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		message = fmt.Sprintf("%s\nHint: increase --duration or verify network connectivity to the cluster.", err)
	case apierrors.IsUnauthorized(err):
		message = fmt.Sprintf("%s\nHint: kubeconfig credentials were rejected. Run 'kubectl config view' to confirm the active user.", err)
	case apierrors.IsForbidden(err):
		message = fmt.Sprintf("%s\nHint: missing Kubernetes permissions. See docs/rbac.md for the verbs ktl requires.", err)
	}
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
}

func streamFromStdin(ctx context.Context, opts *config.Options) error {
	tmpl, err := template.New("ktl-stdin").Parse(opts.Template)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	scanner := bufio.NewScanner(os.Stdin)
	highlight := color.New(color.BgYellow, color.FgBlack)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if opts.ExcludeLineRegex != nil && opts.ExcludeLineRegex.MatchString(line) {
			continue
		}
		message := line
		if len(opts.SearchRegex) > 0 && !color.NoColor {
			for _, re := range opts.SearchRegex {
				message = re.ReplaceAllStringFunc(message, func(m string) string {
					return highlight.Sprint(m)
				})
			}
		}
		timestamp := ""
		if opts.ShowTimestamp {
			now := time.Now()
			if opts.TimeLocation != nil {
				now = now.In(opts.TimeLocation)
			}
			timestamp = now.Format(opts.TimestampFormat)
		}
		entry := struct {
			Timestamp     string
			Namespace     string
			PodName       string
			PodDisplay    string
			ContainerName string
			ContainerTag  string
			Message       string
			Raw           string
		}{
			Timestamp:     timestamp,
			Namespace:     "-",
			PodName:       "stdin",
			PodDisplay:    "stdin",
			ContainerName: "-",
			ContainerTag:  "[stdin]",
			Message:       message,
			Raw:           line,
		}
		var b strings.Builder
		if err := tmpl.Execute(&b, entry); err != nil {
			return fmt.Errorf("execute template: %w", err)
		}
		fmt.Println(b.String())
	}
	return scanner.Err()
}

func configureConfigFile(v *viper.Viper, explicitPath string) {
	if explicitPath != "" {
		v.SetConfigFile(explicitPath)
		return
	}
	v.SetConfigName("config")
	for _, dir := range configSearchDirs() {
		v.AddConfigPath(dir)
	}
}

func buildLogger(level string) (logr.Logger, error) {
	lower := strings.ToLower(level)
	opts := crzap.Options{}
	var zapLevel zapcore.Level
	switch lower {
	case "debug":
		opts.Development = true
		zapLevel = zapcore.DebugLevel
	case "info", "":
		zapLevel = zapcore.InfoLevel
	case "warn", "warning":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		return logr.Logger{}, fmt.Errorf("unknown log level %q (expected debug, info, warn, or error)", level)
	}
	atomic := zap.NewAtomicLevelAt(zapLevel)
	opts.Level = &atomic
	logger := crzap.New(crzap.UseFlagOptions(&opts))
	if lower == "" {
		return logger, nil
	}
	return logger, nil
}

func readConfigFile(v *viper.Viper, strict bool) error {
	if err := v.ReadInConfig(); err != nil {
		var cfgErr viper.ConfigFileNotFoundError
		if errors.As(err, &cfgErr) && !strict {
			return nil
		}
		return err
	}
	return nil
}

func hideFlags(fs *pflag.FlagSet, names []string) {
	if fs == nil {
		return
	}
	for _, name := range names {
		_ = fs.MarkHidden(name)
	}
}

func configSearchDirs() []string {
	added := make(map[string]struct{})
	var dirs []string
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := added[path]; ok {
			return
		}
		added[path] = struct{}{}
		dirs = append(dirs, path)
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		add(filepath.Join(xdg, "ktl"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, ".config", "ktl"))
		add(filepath.Join(home, ".ktl"))
	}
	return dirs
}

func setupProfiling() func() {
	mode := strings.ToLower(os.Getenv("KTL_PROFILE"))
	if mode != "startup" {
		return func() {}
	}
	ts := time.Now().UTC().Format("20060102-150405")
	cpuPath := fmt.Sprintf("ktl-startup-%s.cpu.pprof", ts)
	cpuFile, err := os.Create(cpuPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: unable to create CPU profile %s: %v\n", cpuPath, err)
		return func() {}
	}
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		fmt.Fprintf(os.Stderr, "warn: unable to start CPU profile: %v\n", err)
		cpuFile.Close()
		return func() {}
	}
	fmt.Fprintf(os.Stderr, "KTL_PROFILE=startup: writing CPU profile to %s\n", cpuPath)
	memPath := fmt.Sprintf("ktl-startup-%s.mem.pprof", ts)
	return func() {
		pprof.StopCPUProfile()
		cpuFile.Close()
		memFile, err := os.Create(memPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: unable to create heap profile %s: %v\n", memPath, err)
			return
		}
		defer memFile.Close()
		runtime.GC()
		if err := pprof.WriteHeapProfile(memFile); err != nil {
			fmt.Fprintf(os.Stderr, "warn: unable to write heap profile: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "KTL_PROFILE=startup: writing heap profile to %s\n", memPath)
	}
}
