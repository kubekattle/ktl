// File: cmd/torque-agent/main.go
// Brief: Remote agent CLI entrypoint.

// Package main provides the torque CLI entrypoints.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ingresslabs/torque/internal/agent"
	natsworker "github.com/ingresslabs/torque/internal/ops/transport/nats/worker"
	"github.com/ingresslabs/torque/internal/workflows/buildsvc"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "nats":
			runNATSCommand(os.Args[2:])
			return
		case "capabilities":
			runCapabilitiesCommand(os.Args[2:])
			return
		}
	}

	mode := flag.String("mode", "serve", "Runtime mode: serve or durable (durable enables mirror storage and sandboxed remote builds by default)")
	listen := flag.String("listen", ":7443", "gRPC listen address (host:port)")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig for log/traffic services")
	kubeContext := flag.String("context", "", "Kubeconfig context for log/traffic services")
	token := flag.String("token", "", "Authentication token required for all RPCs (optional; sent as `authorization: Bearer <token>`)")
	httpListen := flag.String("http-listen", "", "HTTP listen address for the mirror gateway (optional; exposes /api/v1/mirror/*)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate PEM file for gRPC (optional; enables TLS when set with -tls-key)")
	tlsKey := flag.String("tls-key", "", "TLS private key PEM file for gRPC (optional; enables TLS when set with -tls-cert)")
	tlsClientCA := flag.String("tls-client-ca", "", "Client CA bundle PEM file for mTLS (optional; when set, client certs are required)")
	mirrorStore := flag.String("mirror-store", "", "Path to the SQLite flight recorder DB for MirrorService (optional; enables ListSessions/Export and durable replay)")
	mirrorMaxSessions := flag.Int("mirror-max-sessions", 0, "Max number of mirror sessions to retain in the flight recorder (0 = unlimited)")
	mirrorMaxFrames := flag.Uint64("mirror-max-frames", 0, "Max frames to retain per mirror session in the flight recorder (0 = unlimited)")
	mirrorMaxBytes := flag.Int64("mirror-max-bytes", 0, "Soft cap for retained mirror DB size in bytes (0 = unlimited; best-effort)")
	mirrorPruneInterval := flag.Duration("mirror-prune-interval", 0, "How often to enforce mirror retention (0 = default)")
	buildSandbox := flag.Bool("build-sandbox", false, "Require sandbox execution for remote BuildService.RunBuild requests")
	buildSandboxConfig := flag.String("build-sandbox-config", "", "Default sandbox runtime config for remote builds (requests may override)")
	buildSandboxBin := flag.String("build-sandbox-bin", "", "Default sandbox runtime binary for remote builds")
	buildSandboxLogs := flag.Bool("build-sandbox-logs", false, "Stream sandbox runtime logs for remote builds")
	flag.Parse()

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "", "serve":
	case "durable":
		if strings.TrimSpace(*mirrorStore) == "" {
			*mirrorStore = defaultDurableMirrorStore()
		}
		if strings.TrimSpace(*httpListen) == "" {
			*httpListen = "127.0.0.1:8081"
		}
		if !flagWasSet("build-sandbox") {
			*buildSandbox = true
		}
		if !flagWasSet("build-sandbox-logs") {
			*buildSandboxLogs = true
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported -mode %q (want serve or durable)\n", *mode)
		os.Exit(2)
	}
	if strings.TrimSpace(*token) == "" {
		*token = strings.TrimSpace(os.Getenv("TORQUE_REMOTE_TOKEN"))
	}

	cfg := agent.Config{
		ListenAddr:                *listen,
		KubeconfigPath:            *kubeconfig,
		KubeContext:               *kubeContext,
		AuthToken:                 *token,
		HTTPListenAddr:            *httpListen,
		TLSCertFile:               *tlsCert,
		TLSKeyFile:                *tlsKey,
		TLSClientCAFile:           *tlsClientCA,
		MirrorStore:               *mirrorStore,
		MirrorMaxSessions:         *mirrorMaxSessions,
		MirrorMaxFramesPerSession: *mirrorMaxFrames,
		MirrorMaxBytes:            *mirrorMaxBytes,
		MirrorPruneInterval:       *mirrorPruneInterval,
		BuildRequireSandbox:       *buildSandbox,
		BuildSandboxConfig:        *buildSandboxConfig,
		BuildSandboxBin:           *buildSandboxBin,
		BuildSandboxLogs:          *buildSandboxLogs,
	}
	srv, err := agent.New(cfg, buildsvc.New(buildsvc.Dependencies{}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runNATSCommand(args []string) {
	if len(args) == 0 {
		printNATSUsage(os.Stderr)
		os.Exit(2)
	}
	if strings.TrimSpace(args[0]) == "-h" || strings.TrimSpace(args[0]) == "--help" {
		printNATSUsage(os.Stdout)
		os.Exit(0)
	}
	switch strings.TrimSpace(args[0]) {
	case "worker":
		config, err := parseNATSWorkerConfig(args[1:], os.Getenv)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			printNATSWorkerUsage(os.Stderr)
			os.Exit(2)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		ready := make(chan struct{})
		config.Ready = ready
		worker, err := natsworker.New(config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(2)
		}
		errCh := make(chan error, 1)
		go func() {
			errCh <- worker.Run(ctx)
		}()
		select {
		case <-ready:
			fmt.Fprintf(os.Stderr, "nats worker ready subject=%s\n", config.Subject)
		case err := <-errCh:
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := <-errCh; err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "heartbeat":
		config, err := parseNATSHeartbeatConfig(args[1:], os.Getenv)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			printNATSHeartbeatUsage(os.Stderr)
			os.Exit(2)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		if err := runNATSHeartbeat(ctx, config); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown nats command %q\n\n", args[0])
		printNATSUsage(os.Stderr)
		os.Exit(2)
	}
}

func parseNATSWorkerConfig(args []string, getenv func(string) string) (natsworker.Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	defaultServer := firstNonEmptyAgent(getenv("TORQUE_NATS_URL"), getenv("TORQUE_NATS_SERVER"))
	defaultSubject := firstNonEmptyAgent(getenv("TORQUE_NATS_SUBJECT"), getenv("TORQUE_NATS_WORKER_SUBJECT"))
	defaultQueue := firstNonEmptyAgent(getenv("TORQUE_NATS_QUEUE"), getenv("TORQUE_NATS_WORKER_QUEUE"))
	defaultTimeout := 30 * time.Second
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_TIMEOUT"), getenv("TORQUE_NATS_WORKER_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse NATS worker timeout env: %w", err)
		}
		defaultTimeout = parsed
	}
	fs := flag.NewFlagSet("torque-agent nats worker", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("nats-url", defaultServer, "NATS server URL (also TORQUE_NATS_URL or TORQUE_NATS_SERVER)")
	subject := fs.String("subject", defaultSubject, "NATS assignment subject to serve (also TORQUE_NATS_SUBJECT or TORQUE_NATS_WORKER_SUBJECT)")
	queue := fs.String("queue", defaultQueue, "Optional NATS queue group (also TORQUE_NATS_QUEUE or TORQUE_NATS_WORKER_QUEUE)")
	creds := fs.String("creds", strings.TrimSpace(getenv("TORQUE_NATS_CREDS")), "NATS user credentials file (also TORQUE_NATS_CREDS)")
	nkey := fs.String("nkey", strings.TrimSpace(getenv("TORQUE_NATS_NKEY")), "NATS NKey seed file (also TORQUE_NATS_NKEY)")
	timeout := fs.Duration("timeout", defaultTimeout, "Per-assignment execution timeout (also TORQUE_NATS_TIMEOUT or TORQUE_NATS_WORKER_TIMEOUT)")
	shell := fs.String("shell", strings.TrimSpace(getenv("TORQUE_AGENT_SHELL")), "Shell binary for local command execution (default sh)")
	if err := fs.Parse(args); err != nil {
		return natsworker.Config{}, err
	}
	if fs.NArg() != 0 {
		return natsworker.Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*subject) == "" {
		return natsworker.Config{}, fmt.Errorf("--subject is required")
	}
	if *timeout <= 0 {
		return natsworker.Config{}, fmt.Errorf("--timeout must be greater than zero")
	}
	return natsworker.Config{
		Server:      strings.TrimSpace(*server),
		Subject:     strings.TrimSpace(*subject),
		Queue:       strings.TrimSpace(*queue),
		Creds:       strings.TrimSpace(*creds),
		NKey:        strings.TrimSpace(*nkey),
		Timeout:     *timeout,
		ShellBinary: strings.TrimSpace(*shell),
	}, nil
}

func printNATSUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  torque-agent nats worker --subject <assignment-subject> [flags]")
	fmt.Fprintln(out, "  torque-agent nats heartbeat --agent-id <id> [flags]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Other commands:")
	fmt.Fprintln(out, "  torque-agent capabilities report [--format json]")
}

func printNATSWorkerUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  torque-agent nats worker --subject <assignment-subject> [--nats-url nats://127.0.0.1:4222] [--queue workers]")
}

func flagWasSet(name string) bool {
	seen := false
	flag.Visit(func(f *flag.Flag) {
		if f != nil && f.Name == name {
			seen = true
		}
	})
	return seen
}

func defaultDurableMirrorStore() string {
	if os.Getuid() == 0 {
		return "/var/lib/torque/agent/mirror.sqlite"
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".torque", "agent", "mirror.sqlite")
	}
	return filepath.Join(os.TempDir(), "torque-agent", "mirror.sqlite")
}

func firstNonEmptyAgent(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
