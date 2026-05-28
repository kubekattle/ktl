package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	"github.com/ingresslabs/torque/internal/stack"
	"github.com/spf13/cobra"
)

const (
	opsExecAPIVersion = "torque.dev/ops/exec/v1alpha1"
	opsExecKind       = "OpsExecResult"
)

type opsExecOptions struct {
	Targets      string
	Selectors    []string
	Groups       []string
	Limit        int
	Command      string
	Transport    string
	Format       string
	OutDir       string
	Timeout      time.Duration
	Parallel     int
	Durable      bool
	Tenant       string
	StaleAfter   time.Duration
	NATSURL      string
	NATSCreds    string
	NATSNKey     string
	MaxDeliver   int
	AckWait      time.Duration
	SSHIdentity  string
	SSHExtraArgs []string
	Store        opsAgentStoreFlags
}

type opsExecResult struct {
	APIVersion         string                      `json:"apiVersion"`
	Kind               string                      `json:"kind"`
	Status             string                      `json:"status"`
	GraphName          string                      `json:"graphName,omitempty"`
	RequestedTransport string                      `json:"requestedTransport"`
	Durable            bool                        `json:"durable"`
	Command            string                      `json:"command"`
	CommandDigest      string                      `json:"commandDigest"`
	Selection          targetgraph.SelectionResult `json:"selection"`
	Runs               []opsExecRunRecord          `json:"runs,omitempty"`
	Results            []opsExecTargetResult       `json:"results"`
	Summary            opsExecSummary              `json:"summary"`
}

type opsExecRunRecord struct {
	RunID      string   `json:"runId,omitempty"`
	Transport  string   `json:"transport"`
	Delivery   string   `json:"delivery,omitempty"`
	Status     string   `json:"status,omitempty"`
	TargetIDs  []string `json:"targetIds,omitempty"`
	BundlePath string   `json:"bundlePath,omitempty"`
}

type opsExecTargetResult struct {
	TargetID        string `json:"targetId"`
	Transport       string `json:"transport"`
	Delivery        string `json:"delivery,omitempty"`
	Status          string `json:"status"`
	RunID           string `json:"runId,omitempty"`
	NodeID          string `json:"nodeId,omitempty"`
	AgentID         string `json:"agentId,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	ExitCode        int    `json:"exitCode,omitempty"`
	TimedOut        bool   `json:"timedOut,omitempty"`
	DurationMillis  int64  `json:"durationMillis,omitempty"`
	Stdout          string `json:"stdout,omitempty"`
	Stderr          string `json:"stderr,omitempty"`
	StdoutDigest    string `json:"stdoutDigest,omitempty"`
	StderrDigest    string `json:"stderrDigest,omitempty"`
	TargetDigest    string `json:"targetDigest,omitempty"`
	CommandDigest   string `json:"commandDigest,omitempty"`
	Error           string `json:"error,omitempty"`
	BundlePath      string `json:"bundlePath,omitempty"`
	ResolutionError string `json:"resolutionError,omitempty"`
}

type opsExecSummary struct {
	Selected    int `json:"selected"`
	Succeeded   int `json:"succeeded"`
	Failed      int `json:"failed"`
	TimedOut    int `json:"timedOut"`
	Blocked     int `json:"blocked"`
	SSHTargets  int `json:"sshTargets"`
	NATSTargets int `json:"natsTargets"`
	Runs        int `json:"runs"`
}

type opsExecResolvedTarget struct {
	Target          targetgraph.Target
	TransportKind   string
	TargetValue     string
	SSHIdentityHint string
	Agent           *heartbeat.AgentStatus
}

type opsExecDirectArtifact struct {
	TargetID string `json:"targetId,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Plan     struct {
		TargetID      string `json:"targetId,omitempty"`
		CommandDigest string `json:"commandDigest,omitempty"`
	} `json:"plan,omitempty"`
	Execute transport.OperationResult `json:"execute"`
}

type opsExecFanoutArtifact struct {
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Results []struct {
		AgentID   string                    `json:"agentId,omitempty"`
		TargetID  string                    `json:"targetId,omitempty"`
		Hostname  string                    `json:"hostname,omitempty"`
		Status    string                    `json:"status,omitempty"`
		Error     string                    `json:"error,omitempty"`
		Receipt   transport.OperationResult `json:"receipt"`
		WorkerSub string                    `json:"workerSubject,omitempty"`
	} `json:"results,omitempty"`
}

func newOpsExecCommand() *cobra.Command {
	opts := opsExecOptions{
		Transport:   "auto",
		Format:      "table",
		Timeout:     30 * time.Second,
		Parallel:    8,
		Durable:     true,
		Tenant:      firstNonEmpty(os.Getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant),
		StaleAfter:  45 * time.Second,
		NATSURL:     firstNonEmpty(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER")),
		NATSCreds:   strings.TrimSpace(os.Getenv("TORQUE_NATS_CREDS")),
		NATSNKey:    strings.TrimSpace(os.Getenv("TORQUE_NATS_NKEY")),
		MaxDeliver:  3,
		AckWait:     30 * time.Second,
		SSHIdentity: strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_IDENTITY")),
	}
	if sshOpts := strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_OPTS")); sshOpts != "" {
		opts.SSHExtraArgs = strings.Fields(sshOpts)
	}

	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Run a bounded ad-hoc host command across selected ops targets",
		Example: `  torque ops exec --targets ./targetgraph.yaml --selector role=db --command 'uptime'
  torque ops exec --targets ./targetgraph.yaml --selector role=mysql --command 'mysqladmin ping' --transport auto --durable --out-dir ./runs/mysql-ping
  torque ops exec --targets ./targetgraph.yaml --group web --command 'hostname' --transport ssh --format text`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops exec does not accept positional arguments")
			}
			if strings.TrimSpace(opts.Targets) == "" {
				return fmt.Errorf("--targets is required")
			}
			if strings.TrimSpace(opts.Command) == "" {
				return fmt.Errorf("--command is required")
			}
			switch strings.ToLower(strings.TrimSpace(opts.Transport)) {
			case "auto", "ssh", "nats":
			default:
				return fmt.Errorf("--transport must be auto, ssh, or nats")
			}
			if opts.Timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than zero")
			}
			if opts.Parallel <= 0 {
				return fmt.Errorf("--parallel must be greater than zero")
			}
			if opts.StaleAfter <= 0 {
				return fmt.Errorf("--stale-after must be greater than zero")
			}
			if opts.AckWait <= 0 {
				return fmt.Errorf("--ack-wait must be greater than zero")
			}
			if opts.MaxDeliver <= 0 {
				return fmt.Errorf("--max-deliver must be greater than zero")
			}
			result, err := runOpsExec(cmd.Context(), opts)
			if result != nil {
				if strings.TrimSpace(opts.OutDir) != "" {
					if writeErr := writeOpsExecOutput(opts.OutDir, result); writeErr != nil {
						return writeErr
					}
				}
				switch strings.ToLower(strings.TrimSpace(opts.Format)) {
				case "json":
					raw, marshalErr := json.MarshalIndent(result, "", "  ")
					if marshalErr != nil {
						return marshalErr
					}
					if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), string(raw)); writeErr != nil {
						return writeErr
					}
				case "text":
					if renderErr := renderOpsExecText(cmd.OutOrStdout(), *result); renderErr != nil {
						return renderErr
					}
				case "table", "":
					if renderErr := renderOpsExecTable(cmd.OutOrStdout(), *result); renderErr != nil {
						return renderErr
					}
				default:
					return fmt.Errorf("--format must be table, text, or json")
				}
				if statusErr := opsExecStatusError(*result); statusErr != nil {
					return statusErr
				}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&opts.Targets, "targets", "", "TargetGraph YAML file")
	cmd.Flags().StringArrayVar(&opts.Selectors, "selector", nil, "Target label selector as key=value (repeatable)")
	cmd.Flags().StringArrayVar(&opts.Groups, "group", nil, "Target group to include (repeatable)")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "Maximum selected targets to execute")
	cmd.Flags().StringVar(&opts.Command, "command", "", "Bounded command to execute on each selected target")
	cmd.Flags().StringVar(&opts.Transport, "transport", opts.Transport, "Transport preference: auto, ssh, or nats")
	cmd.Flags().BoolVar(&opts.Durable, "durable", opts.Durable, "Use durable JetStream fan-out for NATS-selected targets")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Per-target command timeout")
	cmd.Flags().IntVar(&opts.Parallel, "parallel", opts.Parallel, "Maximum parallel targets per transport partition")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: table, text, or json")
	cmd.Flags().StringVar(&opts.OutDir, "out-dir", "", "Write result.json and exported stack run bundles to a directory")
	cmd.Flags().StringVar(&opts.Tenant, "tenant", opts.Tenant, "Tenant namespace to read ready NATS agents from")
	cmd.Flags().DurationVar(&opts.StaleAfter, "stale-after", opts.StaleAfter, "Mark agents stale after this age when resolving automatic NATS execution")
	cmd.Flags().StringVar(&opts.NATSURL, "nats-url", opts.NATSURL, "NATS server URL for NATS-backed execution")
	cmd.Flags().StringVar(&opts.NATSCreds, "creds", opts.NATSCreds, "NATS user credentials file")
	cmd.Flags().StringVar(&opts.NATSNKey, "nkey", opts.NATSNKey, "NATS NKey seed file")
	cmd.Flags().IntVar(&opts.MaxDeliver, "max-deliver", opts.MaxDeliver, "Maximum JetStream deliveries for durable NATS execution")
	cmd.Flags().DurationVar(&opts.AckWait, "ack-wait", opts.AckWait, "JetStream receipt wait and ACK deadline for durable NATS execution")
	cmd.Flags().StringVar(&opts.SSHIdentity, "ssh-identity", opts.SSHIdentity, "SSH identity file for direct SSH execution")
	cmd.Flags().StringArrayVar(&opts.SSHExtraArgs, "ssh-opt", opts.SSHExtraArgs, "Extra ssh options/flags passed through to OpenSSH (repeatable)")
	addOpsAgentStoreFlags(cmd, &opts.Store)
	_ = cmd.RegisterFlagCompletionFunc("transport", cobra.FixedCompletions([]string{"auto", "ssh", "nats"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "text", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Ops Exec Flags")
	return cmd
}

func runOpsExec(ctx context.Context, opts opsExecOptions) (*opsExecResult, error) {
	selector, err := inventory.ParseSelector(opts.Selectors)
	if err != nil {
		return nil, err
	}
	graph, err := targetgraph.LoadFile(opts.Targets)
	if err != nil {
		return nil, err
	}
	selection, err := graph.ResolveSelection(targetgraph.SelectionRequest{
		Selector: selector,
		Groups:   opts.Groups,
		Limit:    opts.Limit,
	})
	if err != nil {
		return nil, err
	}
	result := &opsExecResult{
		APIVersion:         opsExecAPIVersion,
		Kind:               opsExecKind,
		Status:             "succeeded",
		GraphName:          strings.TrimSpace(graph.Metadata.Name),
		RequestedTransport: strings.ToLower(strings.TrimSpace(opts.Transport)),
		Durable:            opts.Durable,
		Command:            strings.TrimSpace(opts.Command),
		CommandDigest:      opsExecDigestString(opts.Command),
		Selection:          selection,
	}
	if len(selection.MatchedTargetIDs) == 0 {
		result.Status = "failed"
		return result, fmt.Errorf("target selection matched 0 targets")
	}

	resolved, err := resolveOpsExecTargets(ctx, graph, selection, opts)
	if err != nil {
		result.Status = "failed"
		return result, err
	}
	result.Summary.Selected = len(selection.MatchedTargetIDs)
	result.Summary.SSHTargets = len(resolved.direct)
	result.Summary.NATSTargets = len(resolved.fleet)

	if len(resolved.direct) > 0 {
		runRecord, targetResults, runErr := executeOpsExecDirect(ctx, opts, resolved.direct)
		result.Runs = append(result.Runs, runRecord)
		result.Results = append(result.Results, targetResults...)
		if runErr != nil {
			result.Status = "failed"
			return finalizeOpsExecResult(result), runErr
		}
	}
	if len(resolved.fleet) > 0 {
		runRecord, targetResults, runErr := executeOpsExecFleet(ctx, opts, resolved.fleet)
		result.Runs = append(result.Runs, runRecord)
		result.Results = append(result.Results, targetResults...)
		if runErr != nil {
			result.Status = "failed"
			return finalizeOpsExecResult(result), runErr
		}
	}
	return finalizeOpsExecResult(result), nil
}

type opsExecResolution struct {
	direct []opsExecResolvedTarget
	fleet  []opsExecResolvedTarget
}

func resolveOpsExecTargets(ctx context.Context, graph *targetgraph.TargetGraph, selection targetgraph.SelectionResult, opts opsExecOptions) (opsExecResolution, error) {
	targetsByID := make(map[string]targetgraph.Target, len(graph.Targets))
	for _, target := range graph.Targets {
		targetsByID[target.ID] = target
	}
	readyAgents := map[string]heartbeat.AgentStatus{}
	if strings.ToLower(strings.TrimSpace(opts.Transport)) != "ssh" {
		snapshot, err := collectOpsAgentStatusSnapshot(ctx, opsAgentStatusRequest{
			source:     "store",
			tenant:     opts.Tenant,
			selector:   nil,
			timeout:    opts.Store.storeTimeout,
			staleAfter: opts.StaleAfter,
			store:      opts.Store,
		})
		if err != nil {
			return opsExecResolution{}, err
		}
		for _, agent := range snapshot.Agents {
			if !strings.EqualFold(strings.TrimSpace(agent.Health), "ready") {
				continue
			}
			if !opsExecAgentHasCapability(agent, stack.NodeKindHostCommandRun) {
				continue
			}
			targetID := strings.TrimSpace(agent.TargetID)
			if targetID == "" {
				continue
			}
			if _, exists := readyAgents[targetID]; !exists {
				readyAgents[targetID] = agent
			}
		}
	}

	requestedTransport := strings.ToLower(strings.TrimSpace(opts.Transport))
	var resolution opsExecResolution
	for _, targetID := range selection.MatchedTargetIDs {
		target, ok := targetsByID[targetID]
		if !ok {
			return opsExecResolution{}, fmt.Errorf("target selection referenced unknown target %q", targetID)
		}
		if strings.TrimSpace(target.Type) != "host" {
			return opsExecResolution{}, fmt.Errorf("target %s is type %q, but torque ops exec currently supports host targets only", target.ID, target.Type)
		}
		if !opsExecTargetAllowsCapability(target, stack.NodeKindHostCommandRun) {
			return opsExecResolution{}, fmt.Errorf("target %s does not allow capability %s", target.ID, stack.NodeKindHostCommandRun)
		}
		durableEligible, err := opsExecTargetSupportsDurableTransport(graph, target)
		if err != nil {
			return opsExecResolution{}, err
		}
		if agent, ok := readyAgents[target.ID]; ok && requestedTransport != "ssh" {
			if durableEligible {
				resolution.fleet = append(resolution.fleet, opsExecResolvedTarget{
					Target:        target,
					TransportKind: "nats",
					Agent:         &agent,
				})
				if requestedTransport == "nats" || requestedTransport == "auto" {
					continue
				}
			}
		}
		if requestedTransport == "nats" {
			if !durableEligible {
				return opsExecResolution{}, fmt.Errorf("target %s has no durable nats transport approved in TargetGraph", target.ID)
			}
			return opsExecResolution{}, fmt.Errorf("target %s has no ready NATS agent in tenant %s", target.ID, opts.Tenant)
		}
		direct, err := resolveOpsExecDirectTarget(graph, target)
		if err != nil {
			return opsExecResolution{}, err
		}
		resolution.direct = append(resolution.direct, direct)
	}
	return resolution, nil
}

func opsExecTargetSupportsDurableTransport(graph *targetgraph.TargetGraph, target targetgraph.Target) (bool, error) {
	durableRef := strings.TrimSpace(target.DurableTransportRef)
	if durableRef == "" {
		transportCfg, ok := opsTransportByID(graph, strings.TrimSpace(target.TransportRef))
		if !ok {
			return false, nil
		}
		return strings.EqualFold(strings.TrimSpace(transportCfg.Kind), "nats"), nil
	}
	transportCfg, ok := opsTransportByID(graph, durableRef)
	if !ok {
		return false, fmt.Errorf("target %s durableTransportRef %q was not found in TargetGraph", target.ID, durableRef)
	}
	if !strings.EqualFold(strings.TrimSpace(transportCfg.Kind), "nats") {
		return false, fmt.Errorf("target %s durableTransportRef %q has kind %q, want nats", target.ID, durableRef, transportCfg.Kind)
	}
	return true, nil
}

func resolveOpsExecDirectTarget(graph *targetgraph.TargetGraph, target targetgraph.Target) (opsExecResolvedTarget, error) {
	transportRef := strings.TrimSpace(target.TransportRef)
	transportCfg, ok := opsTransportByID(graph, transportRef)
	if !ok {
		return opsExecResolvedTarget{}, fmt.Errorf("target %s transportRef %q was not found in TargetGraph", target.ID, transportRef)
	}
	switch strings.ToLower(strings.TrimSpace(transportCfg.Kind)) {
	case "local", "localhost":
		targetValue := strings.TrimSpace(transportCfg.URL)
		if targetValue == "" {
			targetValue = "local://localhost"
		}
		if !strings.HasPrefix(targetValue, "local://") {
			targetValue = "local://" + strings.TrimPrefix(targetValue, "local://")
		}
		return opsExecResolvedTarget{
			Target:        target,
			TransportKind: "local",
			TargetValue:   targetValue,
		}, nil
	case "ssh":
		targetValue, err := resolveOpsExecSSHTarget(transportCfg)
		if err != nil {
			return opsExecResolvedTarget{}, fmt.Errorf("target %s: %w", target.ID, err)
		}
		return opsExecResolvedTarget{
			Target:          target,
			TransportKind:   "ssh",
			TargetValue:     targetValue,
			SSHIdentityHint: normalizeOpsExecIdentityHint(transportCfg.KeyRef),
		}, nil
	default:
		return opsExecResolvedTarget{}, fmt.Errorf("target %s transport kind %q does not support direct execution fallback", target.ID, transportCfg.Kind)
	}
}

func resolveOpsExecSSHTarget(transportCfg targetgraph.Transport) (string, error) {
	if rawTarget := opsExecConfigString(transportCfg.Config, "target"); rawTarget != "" {
		if !strings.HasPrefix(rawTarget, "ssh://") {
			rawTarget = "ssh://" + strings.TrimPrefix(rawTarget, "ssh://")
		}
		return rawTarget, nil
	}
	if rawURL := strings.TrimSpace(transportCfg.URL); rawURL != "" {
		if strings.HasPrefix(rawURL, "ssh://") {
			return rawURL, nil
		}
	}
	host := strings.TrimSpace(transportCfg.Host)
	if host == "" {
		return "", fmt.Errorf("ssh transport %q requires host or config.target", transportCfg.ID)
	}
	user := strings.TrimSpace(transportCfg.User)
	if user == "" {
		return "ssh://" + host, nil
	}
	return "ssh://" + user + "@" + host, nil
}

func normalizeOpsExecIdentityHint(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "file://")
	if strings.HasPrefix(value, "secret://") {
		return ""
	}
	return value
}

func opsExecConfigString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	raw, ok := config[key]
	if !ok {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func opsExecTargetAllowsCapability(target targetgraph.Target, capability string) bool {
	if len(target.AllowedCapabilities) == 0 {
		return true
	}
	for _, candidate := range target.AllowedCapabilities {
		if strings.TrimSpace(candidate) == capability || strings.TrimSpace(candidate) == "*" {
			return true
		}
	}
	return false
}

func opsExecAgentHasCapability(agent heartbeat.AgentStatus, capability string) bool {
	for _, candidate := range agent.Capabilities {
		if strings.TrimSpace(candidate) == capability {
			return true
		}
	}
	return false
}

func executeOpsExecDirect(ctx context.Context, opts opsExecOptions, targets []opsExecResolvedTarget) (opsExecRunRecord, []opsExecTargetResult, error) {
	root, err := os.MkdirTemp("", "torque-ops-exec-direct-*")
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	defer os.RemoveAll(root)

	nodes := make([]*stack.ResolvedRelease, 0, len(targets))
	for _, target := range targets {
		nodeID := "host.command.run/" + opsExecNodeToken(target.Target.ID)
		timeout := opts.Timeout
		nodes = append(nodes, &stack.ResolvedRelease{
			ID:   nodeID,
			Kind: stack.NodeKindHostCommandRun,
			Name: "ops-exec-" + opsExecNodeToken(target.Target.ID),
			Dir:  root,
			Host: stack.HostCommandSpec{
				Transport: target.TransportKind,
				TargetID:  target.Target.ID,
				Target:    target.TargetValue,
				Command:   opts.Command,
				Timeout:   &timeout,
			},
		})
	}
	plan := buildOpsExecPlan(root, "ops-exec-direct", nodes, stack.RunnerResolved{})
	runOpts := stack.RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: opsExecMin(opts.Parallel, len(nodes)),
		FailFast:    false,
		Lock:        false,
	}
	var runErr error
	errOut := &bytes.Buffer{}
	err = withOpsExecEnv(opsExecEnvConfig{
		SSHIdentity: resolveOpsExecIdentityFile(opts, targets),
		SSHOpts:     opts.SSHExtraArgs,
	}, func() error {
		runErr = stack.Run(ctx, runOpts, io.Discard, errOut)
		return nil
	})
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	audit, err := opsExecRunAudit(ctx, root)
	if err != nil {
		if runErr != nil {
			return opsExecRunRecord{}, nil, fmt.Errorf("%w (load direct run audit: %v)", runErr, err)
		}
		return opsExecRunRecord{}, nil, err
	}
	bundlePath, err := exportOpsExecBundle(ctx, opts.OutDir, "direct", root, audit.RunID)
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	results, err := buildOpsExecDirectResults(audit, bundlePath)
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	record := opsExecRunRecord{
		RunID:      audit.RunID,
		Transport:  "ssh",
		Status:     audit.Status,
		TargetIDs:  opsExecTargetIDs(targets),
		BundlePath: bundlePath,
	}
	return record, results, runErr
}

func executeOpsExecFleet(ctx context.Context, opts opsExecOptions, targets []opsExecResolvedTarget) (opsExecRunRecord, []opsExecTargetResult, error) {
	root, err := os.MkdirTemp("", "torque-ops-exec-fleet-*")
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	defer os.RemoveAll(root)

	registryPath := filepath.Join(root, ".torque", "ops", "filtered-agent-registry.json")
	if err := writeOpsExecRegistry(registryPath, opts.Tenant, opts.StaleAfter, targets); err != nil {
		return opsExecRunRecord{}, nil, err
	}
	timeout := opts.Timeout
	node := &stack.ResolvedRelease{
		ID:   "host.command.run/fleet",
		Kind: stack.NodeKindHostCommandRun,
		Name: "ops-exec-fleet",
		Dir:  root,
		Host: stack.HostCommandSpec{
			Transport: "nats",
			Command:   opts.Command,
			Timeout:   &timeout,
		},
	}
	delivery := stack.RunnerFanoutDeliveryRequestReply
	if opts.Durable {
		delivery = stack.RunnerFanoutDeliveryJetStream
	}
	plan := buildOpsExecPlan(root, "ops-exec-fleet", []*stack.ResolvedRelease{node}, stack.RunnerResolved{
		Mode: stack.RunnerModeFleet,
		Readiness: stack.RunnerReadinessResolved{
			Enabled:             true,
			Source:              "store",
			Store:               "file",
			StorePath:           registryPath,
			Tenant:              opts.Tenant,
			RequireAgents:       true,
			MinReadyPercent:     100,
			FailureBudget:       0,
			StaleAfter:          opts.StaleAfter,
			OnInsufficientReady: "block",
		},
		Fanout: stack.RunnerFanoutResolved{
			MaxParallel:         opsExecMin(opts.Parallel, len(targets)),
			MaxFailed:           0,
			MinSucceededPercent: 100,
			OnPartialFailure:    stack.RunnerFanoutOnBlock,
			Delivery:            delivery,
			Retry: stack.RunnerFanoutRetryResolved{
				MaxDeliver:  opts.MaxDeliver,
				AckWait:     opts.AckWait,
				OnExhausted: stack.RunnerFanoutRetryOnBlock,
			},
		},
	})
	runOpts := stack.RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		FailFast:    false,
		Lock:        false,
	}
	var runErr error
	errOut := &bytes.Buffer{}
	err = withOpsExecEnv(opsExecEnvConfig{
		NATSURL:   opts.NATSURL,
		NATSCreds: opts.NATSCreds,
		NATSNKey:  opts.NATSNKey,
	}, func() error {
		runErr = stack.Run(ctx, runOpts, io.Discard, errOut)
		return nil
	})
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	audit, err := opsExecRunAudit(ctx, root)
	if err != nil {
		if runErr != nil {
			return opsExecRunRecord{}, nil, fmt.Errorf("%w (load fleet run audit: %v)", runErr, err)
		}
		return opsExecRunRecord{}, nil, err
	}
	bundlePath, err := exportOpsExecBundle(ctx, opts.OutDir, "nats", root, audit.RunID)
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	results, err := buildOpsExecFleetResults(audit, delivery, bundlePath)
	if err != nil {
		return opsExecRunRecord{}, nil, err
	}
	record := opsExecRunRecord{
		RunID:      audit.RunID,
		Transport:  "nats",
		Delivery:   delivery,
		Status:     audit.Status,
		TargetIDs:  opsExecTargetIDs(targets),
		BundlePath: bundlePath,
	}
	return record, results, runErr
}

func resolveOpsExecIdentityFile(opts opsExecOptions, targets []opsExecResolvedTarget) string {
	if strings.TrimSpace(opts.SSHIdentity) != "" {
		return strings.TrimSpace(opts.SSHIdentity)
	}
	seen := map[string]struct{}{}
	var values []string
	for _, target := range targets {
		if strings.TrimSpace(target.SSHIdentityHint) == "" {
			continue
		}
		if _, ok := seen[target.SSHIdentityHint]; ok {
			continue
		}
		seen[target.SSHIdentityHint] = struct{}{}
		values = append(values, target.SSHIdentityHint)
	}
	sort.Strings(values)
	if len(values) == 1 {
		return values[0]
	}
	return ""
}

func writeOpsExecRegistry(path string, tenant string, staleAfter time.Duration, targets []opsExecResolvedTarget) error {
	store, err := heartbeat.NewFileStore(path)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	for _, target := range targets {
		if target.Agent == nil {
			continue
		}
		agent := *target.Agent
		observedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(agent.LastSeen))
		if err != nil {
			observedAt = time.Now().UTC()
		}
		hb := heartbeat.New(heartbeat.Options{
			AgentID:          agent.AgentID,
			Tenant:           firstNonEmpty(strings.TrimSpace(agent.Tenant), tenant),
			TargetID:         agent.TargetID,
			Hostname:         agent.Hostname,
			Version:          agent.Version,
			Labels:           agent.Labels,
			Capabilities:     agent.Capabilities,
			CapabilityDigest: agent.CapabilityDigest,
			Slots:            agent.Slots,
			WorkerSlots:      agent.WorkerSlots,
			Offsets:          agent.Offsets,
			Resources:        agent.Resources,
			State:            agent.State,
			ObservedAt:       observedAt,
		})
		record, err := heartbeat.NewCompactRecord(hb, heartbeat.StreamOffset{}, observedAt, staleAfter)
		if err != nil {
			return err
		}
		if err := store.Put(context.Background(), record); err != nil {
			return err
		}
	}
	return nil
}

func buildOpsExecPlan(root string, name string, nodes []*stack.ResolvedRelease, runner stack.RunnerResolved) *stack.Plan {
	byID := make(map[string]*stack.ResolvedRelease, len(nodes))
	order := make([]string, 0, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
		order = append(order, node.ID)
	}
	sort.Strings(order)
	return &stack.Plan{
		StackRoot: root,
		StackName: name,
		Nodes:     nodes,
		Order:     order,
		Runner:    runner,
		ByID:      byID,
		ByCluster: map[string][]*stack.ResolvedRelease{},
	}
}

func opsExecRunAudit(ctx context.Context, root string) (*stack.RunAudit, error) {
	runID, err := stack.LoadMostRecentRun(root)
	if err != nil {
		return nil, err
	}
	return stack.GetRunAudit(ctx, stack.RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludePlan:      true,
		IncludeArtifacts: true,
	})
}

func exportOpsExecBundle(ctx context.Context, outDir string, prefix string, root string, runID string) (string, error) {
	if strings.TrimSpace(outDir) == "" {
		return "", nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	outPath := filepath.Join(outDir, prefix+"-"+opsExecNodeToken(runID)+".tgz")
	return stack.ExportRunBundle(ctx, root, runID, outPath)
}

func buildOpsExecDirectResults(audit *stack.RunAudit, bundlePath string) ([]opsExecTargetResult, error) {
	if audit == nil || audit.Plan == nil {
		return nil, fmt.Errorf("direct execution audit is missing plan data")
	}
	artifacts := make(map[string]stack.RunArtifact, len(audit.Artifacts))
	for _, artifact := range audit.Artifacts {
		artifacts[artifact.NodeID+"\x00"+artifact.Name] = artifact
	}
	var out []opsExecTargetResult
	for _, node := range audit.Plan.Nodes {
		if node == nil || strings.TrimSpace(node.Kind) != stack.NodeKindHostCommandRun {
			continue
		}
		result := opsExecTargetResult{
			TargetID:      strings.TrimSpace(node.Host.TargetID),
			Transport:     strings.TrimSpace(node.Host.Transport),
			RunID:         audit.RunID,
			NodeID:        strings.TrimSpace(node.ID),
			Status:        opsExecNodeStatus(audit, node.ID),
			CommandDigest: opsExecDigestString(node.Host.Command),
			BundlePath:    bundlePath,
		}
		if artifact, ok := artifacts[node.ID+"\x00"+"host-command.json"]; ok {
			var payload opsExecDirectArtifact
			if err := json.Unmarshal([]byte(artifact.Body), &payload); err != nil {
				return nil, fmt.Errorf("decode %s host-command.json: %w", node.ID, err)
			}
			if strings.TrimSpace(result.TargetID) == "" {
				result.TargetID = strings.TrimSpace(firstNonEmpty(payload.TargetID, payload.Plan.TargetID))
			}
			result = populateOpsExecTargetResult(result, payload.Execute)
			if strings.TrimSpace(payload.Plan.CommandDigest) != "" {
				result.CommandDigest = strings.TrimSpace(payload.Plan.CommandDigest)
			}
		}
		if summary := opsExecNodeSummary(audit, node.ID); summary != nil && strings.TrimSpace(summary.Error) != "" && strings.TrimSpace(result.Error) == "" {
			result.Error = strings.TrimSpace(summary.Error)
		}
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TargetID < out[j].TargetID })
	return out, nil
}

func buildOpsExecFleetResults(audit *stack.RunAudit, delivery string, bundlePath string) ([]opsExecTargetResult, error) {
	if audit == nil {
		return nil, fmt.Errorf("fleet execution audit is missing")
	}
	var fanout stack.RunArtifact
	found := false
	for _, artifact := range audit.Artifacts {
		if artifact.Name == "host-command-fanout.json" {
			fanout = artifact
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("fleet execution audit is missing host-command-fanout.json")
	}
	var payload opsExecFanoutArtifact
	if err := json.Unmarshal([]byte(fanout.Body), &payload); err != nil {
		return nil, fmt.Errorf("decode host-command-fanout.json: %w", err)
	}
	out := make([]opsExecTargetResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		result := opsExecTargetResult{
			TargetID:      strings.TrimSpace(item.TargetID),
			Transport:     "nats",
			Delivery:      delivery,
			RunID:         audit.RunID,
			NodeID:        strings.TrimSpace(fanout.NodeID),
			AgentID:       strings.TrimSpace(item.AgentID),
			Hostname:      strings.TrimSpace(item.Hostname),
			Status:        strings.TrimSpace(firstNonEmpty(item.Status, item.Receipt.Status)),
			CommandDigest: opsExecDigestString(firstNonEmpty(strings.Join(item.Receipt.Command, " "), item.Receipt.Metadata["assignmentCommand"])),
			BundlePath:    bundlePath,
		}
		result = populateOpsExecTargetResult(result, item.Receipt)
		if strings.TrimSpace(result.Error) == "" {
			result.Error = strings.TrimSpace(item.Error)
		}
		out = append(out, result)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TargetID < out[j].TargetID })
	return out, nil
}

func populateOpsExecTargetResult(result opsExecTargetResult, receipt transport.OperationResult) opsExecTargetResult {
	if strings.TrimSpace(receipt.Status) != "" {
		result.Status = strings.TrimSpace(receipt.Status)
	}
	result.ExitCode = receipt.ExitCode
	result.TimedOut = receipt.TimedOut
	result.DurationMillis = receipt.DurationMillis
	result.Stdout = receipt.Stdout
	result.Stderr = receipt.Stderr
	result.StdoutDigest = opsExecDigestString(receipt.Stdout)
	result.StderrDigest = opsExecDigestString(receipt.Stderr)
	result.TargetDigest = strings.TrimSpace(receipt.TargetDigest)
	if strings.TrimSpace(receipt.Error) != "" {
		result.Error = strings.TrimSpace(receipt.Error)
	}
	return result
}

func opsExecNodeSummary(audit *stack.RunAudit, nodeID string) *stack.RunNodeSummary {
	if audit == nil || audit.Summary == nil {
		return nil
	}
	summary, ok := audit.Summary.Nodes[strings.TrimSpace(nodeID)]
	if !ok {
		return nil
	}
	return &summary
}

func opsExecNodeStatus(audit *stack.RunAudit, nodeID string) string {
	summary := opsExecNodeSummary(audit, nodeID)
	if summary == nil {
		return ""
	}
	return strings.TrimSpace(summary.Status)
}

func finalizeOpsExecResult(result *opsExecResult) *opsExecResult {
	if result == nil {
		return nil
	}
	sort.Slice(result.Runs, func(i, j int) bool { return result.Runs[i].Transport < result.Runs[j].Transport })
	sort.Slice(result.Results, func(i, j int) bool { return result.Results[i].TargetID < result.Results[j].TargetID })
	result.Summary.Runs = len(result.Runs)
	for _, target := range result.Results {
		switch strings.ToLower(strings.TrimSpace(target.Status)) {
		case "succeeded", "success":
			result.Summary.Succeeded++
		case "blocked":
			result.Summary.Blocked++
		case "timeout":
			result.Summary.TimedOut++
		default:
			result.Summary.Failed++
		}
	}
	switch {
	case result.Summary.Failed > 0 || result.Summary.TimedOut > 0:
		result.Status = "failed"
	case result.Summary.Blocked > 0:
		result.Status = "blocked"
	default:
		result.Status = "succeeded"
	}
	return result
}

func writeOpsExecOutput(outDir string, result *opsExecResult) error {
	if result == nil {
		return nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "result.json"), append(raw, '\n'), 0o644)
}

func renderOpsExecTable(out io.Writer, result opsExecResult) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TARGET\tTRANSPORT\tSTATUS\tEXIT\tDURATION\tRUN"); err != nil {
		return err
	}
	for _, target := range result.Results {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			target.TargetID,
			opsExecFormatTransport(target),
			emptyDash(target.Status),
			opsExecFormatExitCode(target),
			opsExecFormatDuration(target.DurationMillis),
			emptyDash(target.RunID),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func renderOpsExecText(out io.Writer, result opsExecResult) error {
	for idx, target := range result.Results {
		if idx > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(out, "[%s] %s %s\n", target.TargetID, opsExecFormatTransport(target), target.Status); err != nil {
			return err
		}
		if target.Stdout != "" {
			if _, err := fmt.Fprintf(out, "stdout:\n%s\n", strings.TrimRight(target.Stdout, "\n")); err != nil {
				return err
			}
		}
		if target.Stderr != "" {
			if _, err := fmt.Fprintf(out, "stderr:\n%s\n", strings.TrimRight(target.Stderr, "\n")); err != nil {
				return err
			}
		}
		if target.Error != "" {
			if _, err := fmt.Fprintf(out, "error: %s\n", target.Error); err != nil {
				return err
			}
		}
	}
	return nil
}

func opsExecFormatTransport(target opsExecTargetResult) string {
	if strings.TrimSpace(target.Delivery) == "" {
		return target.Transport
	}
	return target.Transport + "/" + target.Delivery
}

func opsExecFormatExitCode(target opsExecTargetResult) string {
	if strings.TrimSpace(target.Status) == "succeeded" || target.ExitCode != 0 || target.TimedOut {
		return fmt.Sprintf("%d", target.ExitCode)
	}
	return "-"
}

func opsExecFormatDuration(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return (time.Duration(ms) * time.Millisecond).String()
}

func opsExecStatusError(result opsExecResult) error {
	if result.Summary.Failed == 0 && result.Summary.TimedOut == 0 && result.Summary.Blocked == 0 {
		return nil
	}
	return fmt.Errorf(
		"ops exec incomplete: %d failed, %d timed out, %d blocked",
		result.Summary.Failed,
		result.Summary.TimedOut,
		result.Summary.Blocked,
	)
}

type opsExecEnvConfig struct {
	NATSURL     string
	NATSCreds   string
	NATSNKey    string
	SSHIdentity string
	SSHOpts     []string
}

func withOpsExecEnv(cfg opsExecEnvConfig, fn func() error) error {
	restore := map[string]*string{
		"TORQUE_NATS_URL":         opsExecSavedEnv("TORQUE_NATS_URL"),
		"TORQUE_NATS_CREDS":       opsExecSavedEnv("TORQUE_NATS_CREDS"),
		"TORQUE_NATS_NKEY":        opsExecSavedEnv("TORQUE_NATS_NKEY"),
		"TORQUE_LAB_SSH_IDENTITY": opsExecSavedEnv("TORQUE_LAB_SSH_IDENTITY"),
		"TORQUE_LAB_SSH_OPTS":     opsExecSavedEnv("TORQUE_LAB_SSH_OPTS"),
	}
	defer opsExecRestoreEnv(restore)

	if err := opsExecSetEnv("TORQUE_NATS_URL", cfg.NATSURL); err != nil {
		return err
	}
	if err := opsExecSetEnv("TORQUE_NATS_CREDS", cfg.NATSCreds); err != nil {
		return err
	}
	if err := opsExecSetEnv("TORQUE_NATS_NKEY", cfg.NATSNKey); err != nil {
		return err
	}
	if err := opsExecSetEnv("TORQUE_LAB_SSH_IDENTITY", cfg.SSHIdentity); err != nil {
		return err
	}
	if err := opsExecSetEnv("TORQUE_LAB_SSH_OPTS", strings.Join(cfg.SSHOpts, " ")); err != nil {
		return err
	}
	return fn()
}

func opsExecSavedEnv(key string) *string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	copied := value
	return &copied
}

func opsExecRestoreEnv(values map[string]*string) {
	for key, value := range values {
		if value == nil {
			_ = os.Unsetenv(key)
			continue
		}
		_ = os.Setenv(key, *value)
	}
}

func opsExecSetEnv(key string, value string) error {
	if strings.TrimSpace(value) == "" {
		return os.Unsetenv(key)
	}
	return os.Setenv(key, value)
}

func opsExecTargetIDs(targets []opsExecResolvedTarget) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, target.Target.ID)
	}
	sort.Strings(out)
	return out
}

func opsExecNodeToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "target"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	token := strings.Trim(b.String(), "-")
	if token == "" {
		return "target"
	}
	return token
}

func opsExecMin(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func opsExecDigestString(value string) string {
	return transport.ValueDigest(value)
}
