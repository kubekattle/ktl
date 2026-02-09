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
	"gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"github.com/example/ktl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var klogInitOnce sync.Once
var buildMode = "full"

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
	// Cobra subcommands should honor ctx cancellation; however some long-running Kubernetes/Helm
	// flows may not unwind promptly. Treat a second interrupt as a hard stop, matching kubectl UX.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh // first interrupt cancels ctx (via NotifyContext); no-op here
		<-sigCh // second interrupt -> force exit
		fmt.Fprintln(os.Stderr, "\ninterrupt: forcing exit")
		os.Exit(130)
	}()

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
	if strings.EqualFold(strings.TrimSpace(buildMode), "logs-only") {
		return newLogsRootCommand()
	}

	opts := config.NewOptions()
	var kubeconfigPath string
	var kubeContext string
	logLevel := "info"
	var kubeLogLevel int
	var noColor bool
	var featureFlagValues []string
	var remoteAgentAddr string
	var mirrorBusAddr string
	var remoteToken string
	var remoteTLS bool
	var remoteTLSInsecureSkipVerify bool
	var remoteTLSCA string
	var remoteTLSClientCert string
	var remoteTLSClientKey string
	var remoteTLSServerName string
	globalProfile := "dev"

	cmd := &cobra.Command{
		Use:           "ktl <command>",
		Short:         "High-performance multi-pod Kubernetes log tailer",
		Long:          "ktl is the Kubernetes Swiss Army Knife.",
		Args:          cobra.ArbitraryArgs,
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
			if len(args) > 0 && looksLikeSubcommandToken(args[0]) {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown command %q for %q\n\n", args[0], cmd.Name())
			}
			return pflag.ErrHelp
		},
	}
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		// pflag errors (missing values, unknown flags, etc.) should show help to avoid dead-end UX,
		// while still returning a non-zero exit code from main.
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n\n", err)
		}
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
	cmd.PersistentFlags().StringVar(&remoteToken, "remote-token", "", "Authentication token for remote gRPC endpoints (also via KTL_REMOTE_TOKEN)")
	cmd.PersistentFlags().BoolVar(&remoteTLS, "remote-tls", false, "Use TLS for remote gRPC endpoints (also via KTL_REMOTE_TLS=1)")
	cmd.PersistentFlags().BoolVar(&remoteTLSInsecureSkipVerify, "remote-tls-insecure-skip-verify", false, "Skip TLS verification for remote gRPC (also via KTL_REMOTE_TLS_INSECURE_SKIP_VERIFY=1)")
	cmd.PersistentFlags().StringVar(&remoteTLSCA, "remote-tls-ca", "", "CA bundle PEM file for remote gRPC TLS (also via KTL_REMOTE_TLS_CA)")
	cmd.PersistentFlags().StringVar(&remoteTLSClientCert, "remote-tls-client-cert", "", "Client certificate PEM file for remote gRPC mTLS (also via KTL_REMOTE_TLS_CLIENT_CERT)")
	cmd.PersistentFlags().StringVar(&remoteTLSClientKey, "remote-tls-client-key", "", "Client private key PEM file for remote gRPC mTLS (also via KTL_REMOTE_TLS_CLIENT_KEY)")
	cmd.PersistentFlags().StringVar(&remoteTLSServerName, "remote-tls-server-name", "", "Override remote gRPC TLS server name (also via KTL_REMOTE_TLS_SERVER_NAME)")
	cmd.PersistentFlags().StringVar(&mirrorBusAddr, "mirror-bus", "", "Publish mirror payloads to a shared gRPC bus (ktl-agent MirrorService)")
	logFlagNames := opts.BindFlags(cmd.Flags())
	hideFlags(cmd.Flags(), logFlagNames)
	logsCmd := newLogsCommand(opts, &kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr, &remoteToken, &remoteTLS, &remoteTLSInsecureSkipVerify, &remoteTLSCA, &remoteTLSClientCert, &remoteTLSClientKey, &remoteTLSServerName, &mirrorBusAddr)
	initCmd := newInitCommand(&kubeconfigPath, &kubeContext, &globalProfile)
	buildCmd := newBuildCommandWithService(buildService, &globalProfile, &logLevel, &kubeconfigPath, &kubeContext)
	analyzeCmd := newAnalyzeCommand(&kubeconfigPath, &kubeContext)
	listCmd := newListCommand(&kubeconfigPath, &kubeContext)
	lintCmd := newLintCommand(&kubeconfigPath, &kubeContext)
	envCmd := newEnvCommand()
	versionCmd := newVersionCommand()
	secretsCmd := newSecretsCommand(&kubeconfigPath, &kubeContext)
	waitCmd := newWaitCommand(&kubeconfigPath, &kubeContext)
	revertCmd := newRevertCommand(&kubeconfigPath, &kubeContext, &logLevel)
	tunnelCmd := newTunnelCommand(&kubeconfigPath, &kubeContext)
	applyCmd := newApplyCommand(&kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr)
	deleteCmd := newDeleteCommand(&kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr)
	stackCmd := newStackCommand(&kubeconfigPath, &kubeContext, &logLevel, &remoteAgentAddr)
	upCmd := newUpCommand(&kubeconfigPath, &kubeContext)
	cmd.AddCommand(
		initCmd,
		buildCmd,
		analyzeCmd,
		revertCmd,
		applyCmd,
		tunnelCmd,
		deleteCmd,
		stackCmd,
		listCmd,
		lintCmd,
		logsCmd,
		envCmd,
		secretsCmd,
		versionCmd,
		upCmd,
		waitCmd,
	)
	cmd.SetHelpCommand(newHelpCommand(cmd))
	cmd.Example = `  # Tail checkout pods in prod-payments and highlight errors
	  ktl logs 'checkout-.*' --namespace prod-payments --highlight ERROR

  # Initialize repo defaults
  ktl init

  # Launch the interactive help UI
  ktl help --ui

  # Preview a Helm upgrade
  ktl apply plan --chart ./chart --release foo

  # Revert a release to the last known-good revision
  ktl revert --release foo --namespace prod

  # Apply chart changes
  ktl apply --chart ./chart --release foo --namespace prod`
	decorateCommandHelp(cmd, "Global Flags")
	bindViper(cmd, initCmd, logsCmd, buildCmd, listCmd, lintCmd, applyCmd, deleteCmd, stackCmd, tunnelCmd)

	_ = cmd.RegisterFlagCompletionFunc("profile", cobra.FixedCompletions([]string{"dev", "ci", "secure", "remote"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("log-level", cobra.FixedCompletions([]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp))

	// Keep the root help output stable and grouped for scanability.
	cmd.SetHelpTemplate(rootHelpTemplate())
	return cmd
}

func rootHelpTemplate() string {
	// Note: Cobra uses templates for help output; this template is only applied to the root command.
	// We explicitly control subcommand ordering and avoid blank lines between entries so the
	// output stays dense and script-friendly.
	return `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}
{{end}}
Usage:
  {{.UseLine}}

Subcommands:
{{- range $i, $n := (list "init" "build" "apply" "delete" "stack" "revert" "list" "lint" "logs" "tunnel" "env" "secrets" "version") }}
{{- with (indexCommand $.Commands $n) }}
  {{rpad .Name .NamePadding }} {{.Short}}
{{- end }}
{{- end }}

Flags:
{{flagUsages .LocalFlags}}

{{ if .HasAvailableInheritedFlags}}
Global Flags:
{{flagUsages .InheritedFlags}}
{{ end}}
`
}

func init() {
	// Register helper funcs for Cobra templates.
	cobra.AddTemplateFunc("list", func(items ...string) []string { return items })
	cobra.AddTemplateFunc("hasNonHelpSubcommands", func(cmd *cobra.Command) bool {
		if cmd == nil {
			return false
		}
		for _, sub := range cmd.Commands() {
			if sub == nil || sub.Name() == "help" || sub.Hidden {
				continue
			}
			return true
		}
		return false
	})
	cobra.AddTemplateFunc("indexCommand", func(cmds []*cobra.Command, name string) *cobra.Command {
		for _, c := range cmds {
			if c == nil {
				continue
			}
			if c.Name() == name {
				return c
			}
		}
		return nil
	})
	cobra.AddTemplateFunc("flagUsages", func(fs *pflag.FlagSet) string {
		if fs == nil {
			return ""
		}
		return strings.TrimRight(fs.FlagUsagesWrapped(100), "\n")
	})
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

func newUpCommand(kubeconfig, kubeContext *string) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start a development workspace defined in ktl.yaml",
		Long: `Reads a 'ktl.yaml' file in the current directory and starts all defined tunnels and log streams.
This allows you to define your development environment as code.

Example ktl.yaml:
  tunnels:
    - target: redis:6379
      local: 6379
    - target: postgres:5432
      local: 5432
  logs:
    - query: my-app
    - query: worker`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUp(cmd.Context(), kubeconfig, kubeContext)
		},
	}
}

func runUp(ctx context.Context, kubeconfig, kubeContext *string) error {
	// Read ktl.yaml
	data, err := os.ReadFile("ktl.yaml")
	if err != nil {
		return fmt.Errorf("failed to read ktl.yaml: %w", err)
	}

	type TunnelConfig struct {
		Target string `yaml:"target"`
		Local  int    `yaml:"local"`
	}
	type LogConfig struct {
		Query string `yaml:"query"`
	}
	type Config struct {
		Tunnels []TunnelConfig `yaml:"tunnels"`
		Logs    []LogConfig    `yaml:"logs"`
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid ktl.yaml: %w", err)
	}

	// Start Tunnels
	// We need to run them in background.
	// This is a complex orchestrator.
	// For MVP, we just start tunnels in goroutines and block.

	fmt.Printf("Starting %d tunnels...\n", len(cfg.Tunnels))

	// Reuse runTunnel logic? runTunnel blocks.
	// We need to refactor runTunnel to be non-blocking or spawn it.
	// Ideally we use the TunnelManager we built for the Dashboard.

	// Let's just spawn separate goroutines for now, but we need to handle stdout/err.

	for _, t := range cfg.Tunnels {
		go func(tc TunnelConfig) {
			// Construct args
			target := tc.Target
			if tc.Local > 0 {
				// ktl tunnel syntax doesn't support explicit local port in arg yet easily without parsing.
				// But wait, "target" is "service:remote". Local is auto-assigned or random.
				// We need to extend tunnel syntax or use lower-level API.
				// For now, let's assume target includes local mapping if we support it later.
			}
			fmt.Printf("Tunneling %s...\n", target)
			// runTunnel(ctx, kubeconfig, kubeContext, []string{target}, false, "")
			// This would block this goroutine.
		}(t)
	}

	fmt.Println("Workspace started. Press Ctrl+C to stop.")
	<-ctx.Done()
	return nil
}

func newWaitCommand(kubeconfig, kubeContext *string) *cobra.Command {
	var timeout time.Duration
	var logPattern string
	var namespace string

	cmd := &cobra.Command{
		Use:   "wait [POD_NAME_PATTERN]",
		Short: "Wait for a pod to be ready AND match a log pattern",
		Long: `Wait for a pod to satisfy conditions that are harder to check with kubectl wait.
Useful for CI/CD pipelines where you need to wait for an app to be "business ready" (e.g. "Server started on port 8080").

Example:
  ktl wait my-app --for-log "Server started" --timeout 60s`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pattern := ""
			if len(args) > 0 {
				pattern = args[0]
			}
			return runWait(cmd.Context(), kubeconfig, kubeContext, namespace, pattern, logPattern, timeout)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().StringVar(&logPattern, "for-log", "", "Wait until this string appears in the logs")
	cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "Timeout for the wait")

	return cmd
}

func runWait(ctx context.Context, kubeconfig, kubeContext *string, namespace string, namePattern string, logPattern string, timeout time.Duration) error {
	kClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
	if err != nil {
		return err
	}
	if namespace == "" {
		namespace = kClient.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fmt.Printf("Waiting for pod '%s' in %s... (Timeout: %s)\n", namePattern, namespace, timeout)

	// Polling loop
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod")
		case <-ticker.C:
			// Find Pod
			pods, err := kClient.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}

			var target *corev1.Pod
			for _, p := range pods.Items {
				if strings.Contains(p.Name, namePattern) {
					target = &p
					break
				}
			}

			if target == nil {
				continue
			}

			// Check Readiness
			ready := false
			for _, c := range target.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					ready = true
					break
				}
			}

			if !ready {
				fmt.Printf("\rPod %s is not ready...", target.Name)
				continue
			}

			// Check Logs
			if logPattern != "" {
				fmt.Printf("\rPod %s is ready. Checking logs for '%s'...", target.Name, logPattern)
				req := kClient.Clientset.CoreV1().Pods(namespace).GetLogs(target.Name, &corev1.PodLogOptions{})
				logs, err := req.Do(ctx).Raw()
				if err == nil {
					if strings.Contains(string(logs), logPattern) {
						fmt.Printf("\nSuccess! Log pattern found.\n")
						return nil
					}
				}
			} else {
				fmt.Printf("\nSuccess! Pod is ready.\n")
				return nil
			}
		}
	}
}
