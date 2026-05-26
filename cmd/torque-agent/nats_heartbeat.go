package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	agentcapability "github.com/ingresslabs/torque/internal/ops/agent/capability"
	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/version"
)

type natsHeartbeatConfig struct {
	NATS     heartbeat.NATSConfig
	Options  heartbeat.Options
	Interval time.Duration
	Once     bool
	Shards   int
}

func parseNATSHeartbeatConfig(args []string, getenv func(string) string) (natsHeartbeatConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	hostname, _ := os.Hostname()
	defaultServer := firstNonEmptyAgent(getenv("TORQUE_NATS_URL"), getenv("TORQUE_NATS_SERVER"))
	defaultAgentID := firstNonEmptyAgent(getenv("TORQUE_AGENT_ID"), hostname)
	defaultTenant := firstNonEmptyAgent(getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant)
	defaultTargetID := firstNonEmptyAgent(getenv("TORQUE_AGENT_TARGET_ID"), defaultAgentID)
	defaultVersion := firstNonEmptyAgent(getenv("TORQUE_AGENT_VERSION"), version.Get().Version)
	defaultState := firstNonEmptyAgent(getenv("TORQUE_AGENT_STATE"), heartbeat.StateReady)
	defaultHostname := firstNonEmptyAgent(getenv("TORQUE_AGENT_HOSTNAME"), hostname)
	defaultLabels, err := parseKeyValueCSV(getenv("TORQUE_AGENT_LABELS"))
	if err != nil {
		return natsHeartbeatConfig{}, fmt.Errorf("parse TORQUE_AGENT_LABELS: %w", err)
	}
	defaultCapabilities := parseCSV(getenv("TORQUE_AGENT_CAPABILITIES"))
	defaultDiscoverCapabilities := true
	if raw := strings.TrimSpace(getenv("TORQUE_AGENT_DISCOVER_CAPABILITIES")); raw != "" {
		parsed, err := parseBoolDefault(raw, true)
		if err != nil {
			return natsHeartbeatConfig{}, fmt.Errorf("parse TORQUE_AGENT_DISCOVER_CAPABILITIES: %w", err)
		}
		defaultDiscoverCapabilities = parsed
	}
	defaultJetStream, err := parseBoolDefault(getenv("TORQUE_NATS_JETSTREAM"), false)
	if err != nil {
		return natsHeartbeatConfig{}, fmt.Errorf("parse TORQUE_NATS_JETSTREAM: %w", err)
	}
	defaultStream := firstNonEmptyAgent(getenv("TORQUE_NATS_STREAM"), heartbeat.DefaultEventStream)
	defaultTimeout := 30 * time.Second
	if raw := firstNonEmptyAgent(getenv("TORQUE_NATS_TIMEOUT"), getenv("TORQUE_NATS_HEARTBEAT_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return natsHeartbeatConfig{}, fmt.Errorf("parse NATS heartbeat timeout env: %w", err)
		}
		defaultTimeout = parsed
	}
	defaultInterval := 15 * time.Second
	if raw := getenv("TORQUE_AGENT_HEARTBEAT_INTERVAL"); strings.TrimSpace(raw) != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return natsHeartbeatConfig{}, fmt.Errorf("parse TORQUE_AGENT_HEARTBEAT_INTERVAL: %w", err)
		}
		defaultInterval = parsed
	}
	defaultSlots := 1
	if raw := strings.TrimSpace(getenv("TORQUE_AGENT_SLOTS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return natsHeartbeatConfig{}, fmt.Errorf("parse TORQUE_AGENT_SLOTS: %w", err)
		}
		defaultSlots = parsed
	}

	fs := flag.NewFlagSet("torque-agent nats heartbeat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	server := fs.String("nats-url", defaultServer, "NATS server URL (also TORQUE_NATS_URL or TORQUE_NATS_SERVER)")
	agentID := fs.String("agent-id", defaultAgentID, "Stable agent identity (also TORQUE_AGENT_ID)")
	tenant := fs.String("tenant", defaultTenant, "Tenant namespace for heartbeat subjects (also TORQUE_AGENT_TENANT)")
	targetID := fs.String("target-id", defaultTargetID, "TargetGraph target ID represented by this agent (also TORQUE_AGENT_TARGET_ID)")
	hostnameFlag := fs.String("hostname", defaultHostname, "Hostname to publish in the heartbeat (also TORQUE_AGENT_HOSTNAME)")
	versionFlag := fs.String("version", defaultVersion, "Agent version to publish (also TORQUE_AGENT_VERSION)")
	state := fs.String("state", defaultState, "Agent state: ready, degraded, draining, or offline")
	creds := fs.String("creds", strings.TrimSpace(getenv("TORQUE_NATS_CREDS")), "NATS user credentials file (also TORQUE_NATS_CREDS)")
	nkey := fs.String("nkey", strings.TrimSpace(getenv("TORQUE_NATS_NKEY")), "NATS NKey seed file (also TORQUE_NATS_NKEY)")
	timeout := fs.Duration("timeout", defaultTimeout, "NATS connection timeout (also TORQUE_NATS_TIMEOUT or TORQUE_NATS_HEARTBEAT_TIMEOUT)")
	interval := fs.Duration("interval", defaultInterval, "Heartbeat interval for continuous mode (also TORQUE_AGENT_HEARTBEAT_INTERVAL)")
	jetStream := fs.Bool("jetstream", defaultJetStream, "Publish heartbeats through JetStream with server ack (also TORQUE_NATS_JETSTREAM)")
	stream := fs.String("stream", defaultStream, "JetStream stream for durable agent events (also TORQUE_NATS_STREAM)")
	streamMaxAge := fs.Duration("stream-max-age", 24*time.Hour, "JetStream stream retention when auto-created")
	once := fs.Bool("once", false, "Publish one heartbeat and exit")
	shards := fs.Int("shards", heartbeat.DefaultShardCount, "Heartbeat subject shard count")
	slots := fs.Int("slots", defaultSlots, "Total local job slots to advertise")
	inUse := fs.Int("in-use", 0, "Currently used local job slots")
	discoverCapabilities := fs.Bool("discover-capabilities", defaultDiscoverCapabilities, "Discover local agent capabilities and include available adapters by default (also TORQUE_AGENT_DISCOVER_CAPABILITIES)")
	labels := copyStringMap(defaultLabels)
	capabilities := append([]string(nil), defaultCapabilities...)
	fs.Func("label", "Agent label as key=value (repeatable)", func(raw string) error {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return fmt.Errorf("label %q must be key=value", raw)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return fmt.Errorf("label %q must have non-empty key and value", raw)
		}
		labels[key] = value
		return nil
	})
	fs.Func("capability", "Agent capability string (repeatable)", func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fmt.Errorf("capability must not be empty")
		}
		capabilities = append(capabilities, raw)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return natsHeartbeatConfig{}, err
	}
	if fs.NArg() != 0 {
		return natsHeartbeatConfig{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*agentID) == "" {
		return natsHeartbeatConfig{}, fmt.Errorf("--agent-id is required")
	}
	if *timeout <= 0 {
		return natsHeartbeatConfig{}, fmt.Errorf("--timeout must be greater than zero")
	}
	if *interval <= 0 {
		return natsHeartbeatConfig{}, fmt.Errorf("--interval must be greater than zero")
	}
	if *shards <= 0 {
		return natsHeartbeatConfig{}, fmt.Errorf("--shards must be greater than zero")
	}
	if *slots < 0 || *inUse < 0 {
		return natsHeartbeatConfig{}, fmt.Errorf("--slots and --in-use must not be negative")
	}
	if *inUse > *slots {
		return natsHeartbeatConfig{}, fmt.Errorf("--in-use must not exceed --slots")
	}
	capabilityDigest := ""
	if *discoverCapabilities {
		report := agentcapability.Discover(agentcapability.Options{
			AgentVersion: strings.TrimSpace(*versionFlag),
			Hostname:     strings.TrimSpace(*hostnameFlag),
		})
		capabilities = append(capabilities, agentcapability.AvailableAdapters(report)...)
		capabilityDigest = report.Digest
	} else if len(capabilities) > 0 {
		capabilityDigest = agentcapability.DigestNames(capabilities)
	}
	opts := heartbeat.Options{
		AgentID:          strings.TrimSpace(*agentID),
		Tenant:           strings.TrimSpace(*tenant),
		TargetID:         strings.TrimSpace(*targetID),
		Hostname:         strings.TrimSpace(*hostnameFlag),
		Version:          strings.TrimSpace(*versionFlag),
		Labels:           labels,
		Capabilities:     capabilities,
		CapabilityDigest: capabilityDigest,
		Slots: heartbeat.Slots{
			Total: *slots,
			InUse: *inUse,
		},
		State: strings.TrimSpace(*state),
	}
	if err := heartbeat.New(opts).Validate(); err != nil {
		return natsHeartbeatConfig{}, err
	}
	return natsHeartbeatConfig{
		NATS: heartbeat.NATSConfig{
			Server:       strings.TrimSpace(*server),
			Creds:        strings.TrimSpace(*creds),
			NKey:         strings.TrimSpace(*nkey),
			Timeout:      *timeout,
			Name:         "torque-agent-heartbeat",
			JetStream:    *jetStream,
			Stream:       strings.TrimSpace(*stream),
			StreamMaxAge: *streamMaxAge,
		},
		Options:  opts,
		Interval: *interval,
		Once:     *once,
		Shards:   *shards,
	}, nil
}

func runNATSHeartbeat(ctx context.Context, config natsHeartbeatConfig) error {
	publisher, err := heartbeat.NewPublisher(ctx, config.NATS)
	if err != nil {
		return err
	}
	defer publisher.Close()
	published := 0
	publish := func() error {
		opts := config.Options
		opts.ObservedAt = time.Now()
		subject, err := publisher.Publish(ctx, heartbeat.New(opts), config.Shards)
		if err != nil {
			return err
		}
		if published == 0 {
			fmt.Fprintf(os.Stderr, "nats heartbeat published subject=%s\n", subject)
		}
		published++
		return nil
	}
	if err := publish(); err != nil {
		return err
	}
	if config.Once {
		return nil
	}
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := publish(); err != nil {
				return err
			}
		}
	}
}

func printNATSHeartbeatUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  torque-agent nats heartbeat --agent-id <id> [--nats-url nats://127.0.0.1:4222] [--label role=mysql] [--jetstream] [--discover-capabilities=false]")
}

func parseKeyValueCSV(raw string) (map[string]string, error) {
	labels := map[string]string{}
	for _, item := range parseCSV(raw) {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("entry %q must be key=value", item)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return nil, fmt.Errorf("entry %q must have non-empty key and value", item)
		}
		labels[key] = value
	}
	return labels, nil
}

func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func parseBoolDefault(raw string, fallback bool) (bool, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return fallback, nil
	}
	switch raw {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("must be a boolean")
	}
}
