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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ingresslabs/torque/internal/agent"
	opsmysql "github.com/ingresslabs/torque/internal/ops/mysql"
	opspostgres "github.com/ingresslabs/torque/internal/ops/postgres"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
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
		case "postgres-resource-exec":
			runPostgresResourceExec(os.Args[2:])
			return
		case "mysql-resource-exec":
			runMySQLResourceExec(os.Args[2:])
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

func runPostgresResourceExec(args []string) {
	fs := flag.NewFlagSet("torque-agent postgres-resource-exec", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	requestB64 := fs.String("request-b64", "", "Base64 encoded PostgreSQL resource request")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		os.Exit(2)
	}
	if strings.TrimSpace(*requestB64) == "" {
		fmt.Fprintln(os.Stderr, "Error: --request-b64 is required")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	result, err := opspostgres.ExecuteFromBase64(ctx, *requestB64)
	if writeErr := opspostgres.WriteResult(os.Stdout, result); writeErr != nil {
		fmt.Fprintf(os.Stderr, "Error: write PostgreSQL resource result: %v\n", writeErr)
		os.Exit(1)
	}
	if err != nil {
		os.Exit(1)
	}
}

func runMySQLResourceExec(args []string) {
	fs := flag.NewFlagSet("torque-agent mysql-resource-exec", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	requestB64 := fs.String("request-b64", "", "Base64 encoded MySQL resource request")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "Error: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		os.Exit(2)
	}
	if strings.TrimSpace(*requestB64) == "" {
		fmt.Fprintln(os.Stderr, "Error: --request-b64 is required")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	result, err := opsmysql.ExecuteFromBase64(ctx, *requestB64)
	if writeErr := opsmysql.WriteResult(os.Stdout, result); writeErr != nil {
		fmt.Fprintf(os.Stderr, "Error: write MySQL resource result: %v\n", writeErr)
		os.Exit(1)
	}
	if err != nil {
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
	hostname, _ := os.Hostname()
	defaultServer := firstNonEmptyAgent(getenv("TORQUE_NATS_URL"), getenv("TORQUE_NATS_SERVER"))
	defaultSubject := firstNonEmptyAgent(getenv("TORQUE_NATS_SUBJECT"), getenv("TORQUE_NATS_WORKER_SUBJECT"))
	defaultQueue := firstNonEmptyAgent(getenv("TORQUE_NATS_QUEUE"), getenv("TORQUE_NATS_WORKER_QUEUE"))
	defaultDelivery := firstNonEmptyAgent(getenv("TORQUE_NATS_DELIVERY"), getenv("TORQUE_NATS_WORKER_DELIVERY"), natstransport.DeliveryRequestReply)
	defaultAssignmentStream := firstNonEmptyAgent(getenv("TORQUE_NATS_ASSIGNMENT_STREAM"), natstransport.DefaultAssignmentStream)
	defaultReceiptStream := firstNonEmptyAgent(getenv("TORQUE_NATS_RECEIPT_STREAM"), natstransport.DefaultReceiptStream)
	defaultDurable := firstNonEmptyAgent(getenv("TORQUE_NATS_DURABLE"), getenv("TORQUE_NATS_WORKER_DURABLE"))
	defaultLedgerPath := strings.TrimSpace(getenv("TORQUE_AGENT_ASSIGNMENT_LEDGER"))
	defaultAgentID := firstNonEmptyAgent(getenv("TORQUE_AGENT_ID"), hostname)
	defaultWorkerID := firstNonEmptyAgent(getenv("TORQUE_AGENT_WORKER_ID"), getenv("TORQUE_NATS_WORKER_ID"))
	defaultTenant := firstNonEmptyAgent(getenv("TORQUE_AGENT_TENANT"), "default")
	defaultTargetID := firstNonEmptyAgent(getenv("TORQUE_AGENT_TARGET_ID"), defaultAgentID)
	defaultHostname := firstNonEmptyAgent(getenv("TORQUE_AGENT_HOSTNAME"), hostname)
	defaultCapabilities := parseCSV(getenv("TORQUE_AGENT_CAPABILITIES"))
	defaultDiscoverCapabilities := true
	if raw := strings.TrimSpace(getenv("TORQUE_AGENT_DISCOVER_CAPABILITIES")); raw != "" {
		parsed, err := parseBoolDefault(raw, true)
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse TORQUE_AGENT_DISCOVER_CAPABILITIES: %w", err)
		}
		defaultDiscoverCapabilities = parsed
	}
	defaultTimeout := 30 * time.Second
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_TIMEOUT"), getenv("TORQUE_NATS_WORKER_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse NATS worker timeout env: %w", err)
		}
		defaultTimeout = parsed
	}
	defaultMaxDeliver := 3
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_MAX_DELIVER"), getenv("TORQUE_NATS_WORKER_MAX_DELIVER")); raw != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse NATS worker max deliver env: %w", err)
		}
		defaultMaxDeliver = parsed
	}
	defaultAckWait := 30 * time.Second
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_ACK_WAIT"), getenv("TORQUE_NATS_WORKER_ACK_WAIT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse NATS worker ack wait env: %w", err)
		}
		defaultAckWait = parsed
	}
	defaultNakDelay := time.Duration(0)
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_NAK_DELAY"), getenv("TORQUE_NATS_WORKER_NAK_DELAY")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse NATS worker nak delay env: %w", err)
		}
		defaultNakDelay = parsed
	}
	defaultBackoff, err := parseDurationCSV(firstNonEmptyAgent(getenv("TORQUE_NATS_BACKOFF"), getenv("TORQUE_NATS_WORKER_BACKOFF")))
	if err != nil {
		return natsworker.Config{}, fmt.Errorf("parse NATS worker backoff env: %w", err)
	}
	defaultOnExhausted := firstNonEmptyAgent(getenv("TORQUE_NATS_ON_EXHAUSTED"), getenv("TORQUE_NATS_WORKER_ON_EXHAUSTED"), "block")
	defaultVerifyAssignments := false
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_VERIFY_ASSIGNMENTS"), getenv("TORQUE_NATS_WORKER_VERIFY_ASSIGNMENTS")); raw != "" {
		parsed, err := parseBoolDefault(raw, false)
		if err != nil {
			return natsworker.Config{}, fmt.Errorf("parse NATS worker assignment verification env: %w", err)
		}
		defaultVerifyAssignments = parsed
	}
	defaultTrustedIssuerKey := firstNonEmptyAgent(getenv("TORQUE_NATS_TRUSTED_ISSUER_KEY"), getenv("TORQUE_NATS_ASSIGNMENT_TRUSTED_ISSUER_KEY"))
	defaultAssignmentPolicyDigest := strings.TrimSpace(getenv("TORQUE_NATS_ASSIGNMENT_POLICY_DIGEST"))
	fs := flag.NewFlagSet("torque-agent nats worker", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("nats-url", defaultServer, "NATS server URL (also TORQUE_NATS_URL or TORQUE_NATS_SERVER)")
	subject := fs.String("subject", defaultSubject, "NATS assignment subject to serve (also TORQUE_NATS_SUBJECT or TORQUE_NATS_WORKER_SUBJECT)")
	queue := fs.String("queue", defaultQueue, "Optional NATS queue group (also TORQUE_NATS_QUEUE or TORQUE_NATS_WORKER_QUEUE)")
	delivery := fs.String("delivery", defaultDelivery, "Assignment delivery mode: requestReply or jetstream (also TORQUE_NATS_DELIVERY)")
	assignmentStream := fs.String("assignment-stream", defaultAssignmentStream, "JetStream assignment stream for durable delivery (also TORQUE_NATS_ASSIGNMENT_STREAM)")
	receiptStream := fs.String("receipt-stream", defaultReceiptStream, "JetStream receipt stream for durable delivery (also TORQUE_NATS_RECEIPT_STREAM)")
	durable := fs.String("durable", defaultDurable, "JetStream durable consumer name (also TORQUE_NATS_DURABLE or TORQUE_NATS_WORKER_DURABLE)")
	ledgerPath := fs.String("ledger-path", defaultLedgerPath, "SQLite assignment idempotency ledger path (also TORQUE_AGENT_ASSIGNMENT_LEDGER)")
	maxDeliver := fs.Int("max-deliver", defaultMaxDeliver, "JetStream maximum deliveries before dead-letter receipt (also TORQUE_NATS_MAX_DELIVER)")
	ackWait := fs.Duration("ack-wait", defaultAckWait, "JetStream ack wait for assignment redelivery (also TORQUE_NATS_ACK_WAIT)")
	nakDelay := fs.Duration("nak-delay", defaultNakDelay, "Delay before redelivering a retryable failed assignment (also TORQUE_NATS_NAK_DELAY)")
	onExhausted := fs.String("on-exhausted", defaultOnExhausted, "Retry exhaustion behavior: block or continue (also TORQUE_NATS_ON_EXHAUSTED)")
	verifyAssignments := fs.Bool("verify-assignments", defaultVerifyAssignments, "Require signed assignment envelopes before execution (also TORQUE_NATS_VERIFY_ASSIGNMENTS)")
	trustedIssuerKey := fs.String("trusted-issuer-key", defaultTrustedIssuerKey, "Trusted ed25519 public key file for signed assignment envelopes (also TORQUE_NATS_TRUSTED_ISSUER_KEY)")
	assignmentPolicyDigest := fs.String("policy-digest", defaultAssignmentPolicyDigest, "Expected signed assignment policy digest (also TORQUE_NATS_ASSIGNMENT_POLICY_DIGEST)")
	creds := fs.String("creds", strings.TrimSpace(getenv("TORQUE_NATS_CREDS")), "NATS user credentials file (also TORQUE_NATS_CREDS)")
	nkey := fs.String("nkey", strings.TrimSpace(getenv("TORQUE_NATS_NKEY")), "NATS NKey seed file (also TORQUE_NATS_NKEY)")
	timeout := fs.Duration("timeout", defaultTimeout, "Per-assignment execution timeout (also TORQUE_NATS_TIMEOUT or TORQUE_NATS_WORKER_TIMEOUT)")
	shell := fs.String("shell", strings.TrimSpace(getenv("TORQUE_AGENT_SHELL")), "Shell binary for local command execution (default sh)")
	agentID := fs.String("agent-id", defaultAgentID, "Stable worker agent identity for receipts (also TORQUE_AGENT_ID)")
	workerID := fs.String("worker-id", defaultWorkerID, "Stable local worker process identity for receipts (also TORQUE_AGENT_WORKER_ID or TORQUE_NATS_WORKER_ID)")
	tenant := fs.String("tenant", defaultTenant, "Tenant namespace for worker receipts (also TORQUE_AGENT_TENANT)")
	targetID := fs.String("target-id", defaultTargetID, "TargetGraph target ID represented by this worker (also TORQUE_AGENT_TARGET_ID)")
	hostnameFlag := fs.String("hostname", defaultHostname, "Hostname to include in worker receipts (also TORQUE_AGENT_HOSTNAME)")
	discoverCapabilities := fs.Bool("discover-capabilities", defaultDiscoverCapabilities, "Discover local worker capabilities before accepting assignments (also TORQUE_AGENT_DISCOVER_CAPABILITIES)")
	capabilities := append([]string(nil), defaultCapabilities...)
	backoff := append([]time.Duration(nil), defaultBackoff...)
	fs.Func("backoff", "JetStream retry backoff duration (repeatable; also TORQUE_NATS_BACKOFF comma list)", func(raw string) error {
		parsed, err := time.ParseDuration(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		if parsed <= 0 {
			return fmt.Errorf("backoff must be greater than zero")
		}
		backoff = append(backoff, parsed)
		return nil
	})
	fs.Func("capability", "Worker capability string (repeatable; also TORQUE_AGENT_CAPABILITIES)", func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fmt.Errorf("capability must not be empty")
		}
		capabilities = append(capabilities, raw)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return natsworker.Config{}, err
	}
	if fs.NArg() != 0 {
		return natsworker.Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*subject) == "" {
		return natsworker.Config{}, fmt.Errorf("--subject is required")
	}
	if normalized := natstransport.NormalizeDelivery(*delivery); normalized != natstransport.DeliveryRequestReply && normalized != natstransport.DeliveryJetStream {
		return natsworker.Config{}, fmt.Errorf("--delivery must be requestReply or jetstream")
	} else {
		*delivery = normalized
	}
	if *timeout <= 0 {
		return natsworker.Config{}, fmt.Errorf("--timeout must be greater than zero")
	}
	if *maxDeliver < 1 {
		return natsworker.Config{}, fmt.Errorf("--max-deliver must be >= 1")
	}
	if *ackWait <= 0 {
		return natsworker.Config{}, fmt.Errorf("--ack-wait must be greater than zero")
	}
	if *nakDelay < 0 {
		return natsworker.Config{}, fmt.Errorf("--nak-delay must be >= 0")
	}
	switch strings.ToLower(strings.TrimSpace(*onExhausted)) {
	case "block", "continue":
	default:
		return natsworker.Config{}, fmt.Errorf("--on-exhausted must be block or continue")
	}
	if *verifyAssignments && strings.TrimSpace(*trustedIssuerKey) == "" {
		return natsworker.Config{}, fmt.Errorf("--trusted-issuer-key is required when --verify-assignments is set")
	}
	return natsworker.Config{
		Server:                     strings.TrimSpace(*server),
		Subject:                    strings.TrimSpace(*subject),
		Queue:                      strings.TrimSpace(*queue),
		Delivery:                   strings.TrimSpace(*delivery),
		AssignmentStream:           strings.TrimSpace(*assignmentStream),
		ReceiptStream:              strings.TrimSpace(*receiptStream),
		Durable:                    strings.TrimSpace(*durable),
		LedgerPath:                 strings.TrimSpace(*ledgerPath),
		MaxDeliver:                 *maxDeliver,
		AckWait:                    *ackWait,
		Backoff:                    backoff,
		NakDelay:                   *nakDelay,
		OnExhausted:                strings.ToLower(strings.TrimSpace(*onExhausted)),
		VerifyAssignments:          *verifyAssignments,
		TrustedIssuerKey:           strings.TrimSpace(*trustedIssuerKey),
		AssignmentPolicyDigest:     strings.TrimSpace(*assignmentPolicyDigest),
		Creds:                      strings.TrimSpace(*creds),
		NKey:                       strings.TrimSpace(*nkey),
		Timeout:                    *timeout,
		ShellBinary:                strings.TrimSpace(*shell),
		Capabilities:               capabilities,
		DisableCapabilityDiscovery: !*discoverCapabilities,
		AgentID:                    strings.TrimSpace(*agentID),
		WorkerID:                   strings.TrimSpace(*workerID),
		Tenant:                     strings.TrimSpace(*tenant),
		TargetID:                   strings.TrimSpace(*targetID),
		Hostname:                   strings.TrimSpace(*hostnameFlag),
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
	fmt.Fprintln(out, "  torque-agent nats worker --subject <assignment-subject> [--nats-url nats://127.0.0.1:4222] [--delivery requestReply|jetstream] [--ledger-path .torque/agent/assignments.sqlite] [--verify-assignments --trusted-issuer-key ./assignment-pub.json] [--max-deliver 3] [--ack-wait 30s] [--nak-delay 1s] [--queue workers] [--agent-id host-141] [--worker-id host-141-a] [--discover-capabilities=false]")
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

func flagSetWasSet(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	seen := false
	fs.Visit(func(f *flag.Flag) {
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

func parseDurationCSV(raw string) ([]time.Duration, error) {
	parts := parseCSV(raw)
	if len(parts) == 0 {
		return nil, nil
	}
	out := make([]time.Duration, 0, len(parts))
	for _, part := range parts {
		parsed, err := time.ParseDuration(part)
		if err != nil {
			return nil, err
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("duration %q must be greater than zero", part)
		}
		out = append(out, parsed)
	}
	return out, nil
}
