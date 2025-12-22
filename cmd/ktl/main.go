// File: cmd/ktl/main.go
// Brief: Main ktl CLI entrypoint and root command wiring.

// main.go bootstraps ktl: it builds the root Cobra command, wires profiling, and executes with signal-aware contexts.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/example/ktl/internal/config"
	"github.com/example/ktl/internal/featureflags"
	"github.com/example/ktl/internal/logging"
	"github.com/example/ktl/internal/workflows/buildsvc"
	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
)

var klogInitOnce sync.Once

func initKlogFlags() {
	klogInitOnce.Do(func() {
		// Initialize klog's flags so we can control client-go verbosity from Cobra/pflag.
		klog.InitFlags(nil)
		_ = flag.CommandLine.Set("logtostderr", "true")
		_ = flag.CommandLine.Set("alsologtostderr", "true")
	})
}

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

	initKlogFlags()

	stopProfile := setupProfiling()
	defer stopProfile()

	rootCmd := newRootCommand()
	err := rootCmd.ExecuteContext(ctx)
	handleError(err)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Match conventional SIGINT exit code while keeping output clean.
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	return newRootCommandWithBuildService(defaultBuildService)
}

func newRootCommandWithBuildService(buildService buildsvc.Service) *cobra.Command {
	initKlogFlags()

	opts := config.NewOptions()
	var kubeconfigPath string
	var kubeContext string
	logLevel := "info"
	var kubeLogLevel int
	var noColor bool
	var featureFlagValues []string
	var remoteAgentAddr string
	var mirrorBusAddr string
	globalProfile := "dev"
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
			if kubeLogLevel == 0 {
				if val := strings.TrimSpace(os.Getenv("KTL_KUBE_LOG_LEVEL")); val != "" {
					if n, err := strconv.Atoi(val); err == nil {
						kubeLogLevel = n
					} else {
						return fmt.Errorf("invalid KTL_KUBE_LOG_LEVEL %q: %w", val, err)
					}
				} else if shouldLogAtLevel(logLevel, zapcore.DebugLevel) {
					// Enable HTTP request/response tracing in client-go (DebugWrappers) when ktl runs in debug mode.
					kubeLogLevel = 6
				}
			}
			if kubeLogLevel > 0 {
				_ = flag.CommandLine.Set("v", strconv.Itoa(kubeLogLevel))
				// klog writes to stderr by default when logtostderr is set; ensure it doesn't try to log to files.
				_ = flag.CommandLine.Set("logtostderr", "true")
				_ = flag.CommandLine.Set("alsologtostderr", "true")
			}
			// Treat accidental `--log-level -h/--help` as a help request rather than trying to run.
			// This commonly happens when the user intends to read flag docs but forgets the value.
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
			cmd.Root().SetContext(ctx)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.WSListenAddr) != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "Note: use `ktl logs --ws-listen` (or `ktl logs ... --ws-listen`) instead of running the legacy `ktl [POD_QUERY]` entrypoint with streaming flags.")
				return pflag.ErrHelp
			}
			// Guard against a common footgun: `ktl <typo>` currently falls back to log tailing
			// (legacy `ktl [POD_QUERY]` behavior) and can block on cluster access instead of
			// showing help. If the user provided a single "plain word" argument without any
			// log-tailer flags, treat it as an attempted subcommand and show help immediately.
			if len(args) == 1 && !hasNonGlobalFlag(cmd.Flags()) && looksLikeSubcommandToken(args[0]) {
				fmt.Fprintf(cmd.ErrOrStderr(), "Error: unknown command %q for %q\n\n", args[0], cmd.Name())
				return pflag.ErrHelp
			}
			return runLogs(cmd, args, opts, &kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr, &mirrorBusAddr, "", nil, "", "", 0, 0)
		},
	}
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		// pflag errors (missing values, unknown flags, etc.) should show help to avoid dead-end UX,
		// while still returning a non-zero exit code from main.
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n\n", err)
		}
		_ = cmd.Help()
		return pflag.ErrHelp
	})
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.PersistentFlags().StringVarP(&kubeconfigPath, "kubeconfig", "k", "", "Path to the kubeconfig file to use for CLI requests")
	cmd.PersistentFlags().StringVarP(&kubeContext, "context", "K", "", "Name of the kubeconfig context to use")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", logLevel, "Log level for ktl output (debug, info, warn, error)")
	cmd.PersistentFlags().IntVar(&kubeLogLevel, "kube-log-level", 0, "Kubernetes client-go verbosity (klog -v); at >=6 enables HTTP request/response tracing; can also set KTL_KUBE_LOG_LEVEL")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	cmd.PersistentFlags().Var(newEnumStringValue(&globalProfile, "dev", "ci", "secure", "remote"), "profile", "Execution profile: dev, ci, secure, or remote (sets sensible defaults for supported commands)")
	cmd.PersistentFlags().StringSliceVar(&featureFlagValues, "feature", nil, "Enable experimental ktl features (repeat or pass comma-separated names)")
	if err := cmd.PersistentFlags().MarkHidden("feature"); err != nil {
		cobra.CheckErr(err)
	}
	cmd.PersistentFlags().StringVar(&remoteAgentAddr, "remote-agent", "", "Forward ktl logs and deploy operations to a remote ktl-agent gRPC endpoint")
	cmd.PersistentFlags().StringVar(&mirrorBusAddr, "mirror-bus", "", "Publish mirror payloads to a shared gRPC bus (ktl-agent MirrorService)")
	logFlagNames := opts.BindFlags(cmd.Flags())
	hideFlags(cmd.Flags(), logFlagNames)
	logsCmd := newLogsCommand(opts, &kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr, &mirrorBusAddr)
	buildCmd := newBuildCommandWithService(buildService, &globalProfile)
	planCmd := newPlanCommand(&kubeconfigPath, &kubeContext)
	listCmd := newListCommand(&kubeconfigPath, &kubeContext)
	lintCmd := newLintCommand(&kubeconfigPath, &kubeContext)
	packageCmd := newPackageCommand()
	envCmd := newEnvCommand()
	applyCmd := newApplyCommand(&kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr)
	deleteCmd := newDeleteCommand(&kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr)
	cmd.AddCommand(
		logsCmd,
		buildCmd,
		planCmd,
		listCmd,
		lintCmd,
		packageCmd,
		envCmd,
		applyCmd,
		deleteCmd,
	)
	cmd.Example = `  # Tail checkout pods in prod-payments and highlight errors
	  ktl logs 'checkout-.*' --namespace prod-payments --highlight ERROR

  # Preview a Helm upgrade
  ktl plan --chart ./chart --release foo

  # Apply chart changes
  ktl apply --chart ./chart --release foo --namespace prod`
	decorateCommandHelp(cmd, "Global Flags")
	bindViper(cmd, logsCmd, buildCmd, planCmd, listCmd, lintCmd, packageCmd, applyCmd, deleteCmd)

	_ = cmd.RegisterFlagCompletionFunc("profile", cobra.FixedCompletions([]string{"dev", "ci", "secure", "remote"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("log-level", cobra.FixedCompletions([]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func looksLikeSubcommandToken(arg string) bool {
	if arg == "" {
		return false
	}
	for i := 0; i < len(arg); i++ {
		ch := arg[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			continue
		}
		return false
	}
	return true
}

func bindViper(commands ...*cobra.Command) {
	if len(commands) == 0 {
		return
	}
	registerViperCommands(commands...)
}

var (
	viperInitOnce sync.Once
	viperMu       sync.Mutex
	viperCmds     []*cobra.Command
)

func registerViperCommands(commands ...*cobra.Command) {
	viperMu.Lock()
	viperCmds = append(viperCmds, commands...)
	viperMu.Unlock()

	viperInitOnce.Do(func() {
		v := viper.New()
		v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		v.SetEnvPrefix("KTL")
		v.AutomaticEnv()

		cobra.OnInitialize(func() {
			configFile := os.Getenv("KTL_CONFIG")
			configureConfigFile(v, configFile)

			viperMu.Lock()
			cmds := append([]*cobra.Command(nil), viperCmds...)
			viperMu.Unlock()

			for _, cmd := range cmds {
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
			for _, cmd := range cmds {
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
	})
}

func handleError(err error) {
	if err == nil || errors.Is(err, pflag.ErrHelp) {
		return
	}
	if errors.Is(err, context.Canceled) {
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
	writeHighlightedError(os.Stderr, message)
}

func writeHighlightedError(w io.Writer, message string) {
	if w == nil {
		return
	}
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) && !color.NoColor {
		errPrefix := color.New(color.FgRed, color.Bold).Sprint("Error:")
		hintPrefix := color.New(color.FgYellow, color.Bold).Sprint("Hint:")
		lines := strings.Split(message, "\n")
		if len(lines) == 0 {
			fmt.Fprintf(w, "%s\n", errPrefix)
			return
		}
		fmt.Fprintf(w, "%s %s\n", errPrefix, lines[0])
		for _, line := range lines[1:] {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "Hint:") {
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Hint:"))
				if rest != "" {
					fmt.Fprintf(w, "%s %s\n", hintPrefix, rest)
				} else {
					fmt.Fprintf(w, "%s\n", hintPrefix)
				}
				continue
			}
			fmt.Fprintf(w, "%s\n", line)
		}
		return
	}
	fmt.Fprintf(w, "Error: %s\n", message)
}

func streamFromStdin(ctx context.Context, opts *config.Options, in io.Reader, out io.Writer) error {
	tmpl, err := template.New("ktl-stdin").Parse(opts.Template)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	scanner := bufio.NewScanner(in)
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
		fmt.Fprintln(out, b.String())
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
	return logging.New(level)
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
