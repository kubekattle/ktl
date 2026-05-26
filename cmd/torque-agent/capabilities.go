package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	agentcapability "github.com/ingresslabs/torque/internal/ops/agent/capability"
	"github.com/ingresslabs/torque/internal/version"
)

type capabilityReportConfig struct {
	Options agentcapability.Options
	Format  string
}

func runCapabilitiesCommand(args []string) {
	if len(args) == 0 {
		printCapabilitiesUsage(os.Stderr)
		os.Exit(2)
	}
	if strings.TrimSpace(args[0]) == "-h" || strings.TrimSpace(args[0]) == "--help" {
		printCapabilitiesUsage(os.Stdout)
		os.Exit(0)
	}
	switch strings.TrimSpace(args[0]) {
	case "report":
		config, err := parseCapabilityReportConfig(args[1:], os.Getenv)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				os.Exit(0)
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			printCapabilityReportUsage(os.Stderr)
			os.Exit(2)
		}
		if err := runCapabilityReport(context.Background(), config, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown capabilities command %q\n\n", args[0])
		printCapabilitiesUsage(os.Stderr)
		os.Exit(2)
	}
}

func parseCapabilityReportConfig(args []string, getenv func(string) string) (capabilityReportConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	hostname, _ := os.Hostname()
	defaultHostname := firstNonEmptyAgent(getenv("TORQUE_AGENT_HOSTNAME"), hostname)
	defaultVersion := firstNonEmptyAgent(getenv("TORQUE_AGENT_VERSION"), version.Get().Version)
	defaultFormat := firstNonEmptyAgent(getenv("TORQUE_AGENT_CAPABILITY_FORMAT"), "json")
	adapters := parseCSV(getenv("TORQUE_AGENT_CAPABILITY_ADAPTERS"))

	fs := flag.NewFlagSet("torque-agent capabilities report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	hostnameFlag := fs.String("hostname", defaultHostname, "Hostname to include in the report (also TORQUE_AGENT_HOSTNAME)")
	versionFlag := fs.String("agent-version", defaultVersion, "Agent version to include in the report (also TORQUE_AGENT_VERSION)")
	format := fs.String("format", defaultFormat, "Output format: json")
	fs.Func("adapter", "Adapter capability to report (repeatable; defaults to built-ins)", func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fmt.Errorf("adapter must not be empty")
		}
		adapters = append(adapters, raw)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return capabilityReportConfig{}, err
	}
	if fs.NArg() != 0 {
		return capabilityReportConfig{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.ToLower(strings.TrimSpace(*format)) != "json" {
		return capabilityReportConfig{}, fmt.Errorf("unsupported --format %q", *format)
	}
	return capabilityReportConfig{
		Options: agentcapability.Options{
			Adapters:     adapters,
			AgentVersion: strings.TrimSpace(*versionFlag),
			Hostname:     strings.TrimSpace(*hostnameFlag),
		},
		Format: strings.ToLower(strings.TrimSpace(*format)),
	}, nil
}

func runCapabilityReport(ctx context.Context, config capabilityReportConfig, out io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if out == nil {
		out = io.Discard
	}
	opts := config.Options
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	report := agentcapability.Discover(opts)
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func printCapabilitiesUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  torque-agent capabilities report [--format json]")
}

func printCapabilityReportUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  torque-agent capabilities report [--format json] [--adapter host.command.run]")
}
