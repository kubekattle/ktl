package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"
	"github.com/spf13/cobra"
)

const (
	opsAgentBootstrapAPIVersion = "torque.dev/ops/agent-bootstrap/v1alpha1"
	opsAgentBootstrapKind       = "OpsAgentBootstrapResult"
	opsAgentEnrollAPIVersion    = "torque.dev/ops/agent-enroll/v1alpha1"
	opsAgentEnrollKind          = "OpsAgentEnrollApproveResult"
)

type opsAgentSSHClient interface {
	Connect(ctx context.Context) transport.OperationResult
	Run(ctx context.Context, command string) transport.OperationResult
	Upload(ctx context.Context, localPath string, remotePath string) transport.OperationResult
}

var newOpsAgentBootstrapSSHClient = func(config sshtransport.Config) (opsAgentSSHClient, error) {
	return sshtransport.New(config)
}

var buildOpsAgentBootstrapBinary = resolveOpsAgentBootstrapBinary

type opsAgentBootstrapOptions struct {
	Targets          string
	TargetID         string
	Target           string
	Tenant           string
	NATSURL          string
	AgentID          string
	WorkerID         string
	Labels           []string
	AgentBinary      string
	BuildAgentBinary bool
	NATSCredsFile    string
	NATSNKeyFile     string
	ServicePrefix    string
	RemoteBin        string
	RemoteEnv        string
	RemoteStateDir   string
	RemoteCreds      string
	RemoteNKey       string
	SSHIdentity      string
	SSHExtraArgs     []string
	Timeout          time.Duration
	Ensure           string
	Format           string
	Out              string
}

type opsAgentBootstrapResult struct {
	APIVersion        string                      `json:"apiVersion"`
	Kind              string                      `json:"kind"`
	Status            string                      `json:"status"`
	Ensure            string                      `json:"ensure"`
	TargetID          string                      `json:"targetId"`
	AgentID           string                      `json:"agentId"`
	WorkerID          string                      `json:"workerId"`
	Tenant            string                      `json:"tenant"`
	RemoteTarget      string                      `json:"remoteTarget"`
	AssignmentSubject string                      `json:"assignmentSubject,omitempty"`
	Labels            map[string]string           `json:"labels,omitempty"`
	Platform          opsAgentRemotePlatform      `json:"platform,omitempty"`
	Paths             opsAgentBootstrapPaths      `json:"paths"`
	Receipts          []transport.OperationResult `json:"receipts"`
}

type opsAgentRemotePlatform struct {
	OS     string `json:"os,omitempty"`
	Arch   string `json:"arch,omitempty"`
	GOOS   string `json:"goos,omitempty"`
	GOARCH string `json:"goarch,omitempty"`
}

type opsAgentBootstrapPaths struct {
	AgentBinary   string `json:"agentBinary,omitempty"`
	EnvFile       string `json:"envFile,omitempty"`
	HeartbeatUnit string `json:"heartbeatUnit,omitempty"`
	WorkerUnit    string `json:"workerUnit,omitempty"`
	StateDir      string `json:"stateDir,omitempty"`
	NATSCreds     string `json:"natsCreds,omitempty"`
	NATSNKey      string `json:"natsNKey,omitempty"`
}

type opsAgentBootstrapPlan struct {
	SSHIdentity string
	SSHTarget   string
	TargetID    string
	AgentID     string
	WorkerID    string
	Tenant      string
	Labels      map[string]string
}

type opsAgentBinaryRef struct {
	Path    string
	Cleanup func()
}

type opsAgentEnrollApproveOptions struct {
	Targets     string
	Out         string
	TargetID    string
	Tenant      string
	NATSURL     string
	Format      string
	UpdateStore bool
	Store       opsAgentStoreFlags
}

type opsAgentEnrollApproveResult struct {
	APIVersion          string                      `json:"apiVersion"`
	Kind                string                      `json:"kind"`
	Status              string                      `json:"status"`
	AgentID             string                      `json:"agentId"`
	TargetID            string                      `json:"targetId"`
	Tenant              string                      `json:"tenant"`
	GraphPath           string                      `json:"graphPath"`
	OutputPath          string                      `json:"outputPath"`
	DurableTransportRef string                      `json:"durableTransportRef"`
	AssignmentSubject   string                      `json:"assignmentSubject"`
	Transport           targetgraph.Transport       `json:"transport"`
	Enrollment          *heartbeat.EnrollmentStatus `json:"enrollment,omitempty"`
}

func newOpsAgentBootstrapCommand() *cobra.Command {
	opts := opsAgentBootstrapOptions{
		Tenant:           firstNonEmpty(os.Getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant),
		NATSURL:          firstNonEmpty(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER")),
		BuildAgentBinary: true,
		RemoteBin:        "/usr/local/bin/torque-agent",
		RemoteEnv:        "/etc/torque/ops-agent.env",
		RemoteStateDir:   "/var/lib/torque/agent",
		RemoteCreds:      "/etc/torque/nats.creds",
		RemoteNKey:       "/etc/torque/nats.nkey",
		ServicePrefix:    "torque-agent",
		SSHIdentity:      strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_IDENTITY")),
		Timeout:          2 * time.Minute,
		Ensure:           "present",
		Format:           "table",
	}
	if sshOpts := strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_OPTS")); sshOpts != "" {
		opts.SSHExtraArgs = strings.Fields(sshOpts)
	}
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Install or remove torque-agent durable services on a host over SSH",
		Example: `  torque ops agent bootstrap --targets ./targetgraph.yaml --target-id host/mysql-01 --nats-url nats://141.105.65.227:4222
  torque ops agent bootstrap --target ssh://root@141.105.65.227 --target-id host/mysql-01 --agent-binary ./bin/linux-amd64/torque-agent
  torque ops agent bootstrap --targets ./targetgraph.yaml --target-id host/mysql-01 --ensure absent`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops agent bootstrap does not accept positional arguments")
			}
			result, err := runOpsAgentBootstrap(cmd.Context(), opts)
			if result != nil {
				if strings.TrimSpace(opts.Out) != "" {
					if writeErr := writeJSONFileEnsured(opts.Out, result); writeErr != nil {
						return writeErr
					}
				}
				if err := renderOpsAgentBootstrapOutput(cmd.OutOrStdout(), *result, opts.Format); err != nil {
					return err
				}
			}
			if err != nil {
				return err
			}
			if result != nil && strings.TrimSpace(result.Status) != "succeeded" {
				return fmt.Errorf("agent bootstrap %s failed", strings.TrimSpace(result.Ensure))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Targets, "targets", "", "TargetGraph YAML file to resolve the SSH bootstrap transport from")
	cmd.Flags().StringVar(&opts.TargetID, "target-id", "", "Stable target ID represented by the agent")
	cmd.Flags().StringVar(&opts.Target, "target", "", "Explicit SSH bootstrap target such as ssh://root@host")
	cmd.Flags().StringVar(&opts.Tenant, "tenant", opts.Tenant, "Tenant namespace for heartbeat and assignment subjects")
	cmd.Flags().StringVar(&opts.NATSURL, "nats-url", opts.NATSURL, "NATS server URL that the remote agent should connect to")
	cmd.Flags().StringVar(&opts.AgentID, "agent-id", "", "Stable agent identity to publish (defaults from target-id)")
	cmd.Flags().StringVar(&opts.WorkerID, "worker-id", "", "Stable worker process identity (defaults from agent-id)")
	cmd.Flags().StringArrayVar(&opts.Labels, "label", nil, "Agent label as key=value (repeatable)")
	cmd.Flags().StringVar(&opts.AgentBinary, "agent-binary", "", "Local torque-agent binary to upload to the host")
	cmd.Flags().BoolVar(&opts.BuildAgentBinary, "build-agent-binary", opts.BuildAgentBinary, "Build a matching linux torque-agent binary from the current repo when --agent-binary is not supplied")
	cmd.Flags().StringVar(&opts.NATSCredsFile, "nats-creds-file", "", "Optional local NATS credentials file to upload to the host")
	cmd.Flags().StringVar(&opts.NATSNKeyFile, "nats-nkey-file", "", "Optional local NATS NKey seed file to upload to the host")
	cmd.Flags().StringVar(&opts.ServicePrefix, "service-prefix", opts.ServicePrefix, "Systemd service name prefix used for the heartbeat and worker units")
	cmd.Flags().StringVar(&opts.RemoteBin, "remote-bin", opts.RemoteBin, "Remote torque-agent binary path")
	cmd.Flags().StringVar(&opts.RemoteEnv, "remote-env", opts.RemoteEnv, "Remote environment file path")
	cmd.Flags().StringVar(&opts.RemoteStateDir, "remote-state-dir", opts.RemoteStateDir, "Remote working/state directory for the agent")
	cmd.Flags().StringVar(&opts.RemoteCreds, "remote-creds", opts.RemoteCreds, "Remote path for uploaded NATS credentials")
	cmd.Flags().StringVar(&opts.RemoteNKey, "remote-nkey", opts.RemoteNKey, "Remote path for uploaded NATS NKey seed")
	cmd.Flags().StringVar(&opts.SSHIdentity, "ssh-identity", opts.SSHIdentity, "SSH identity file for direct bootstrap access")
	cmd.Flags().StringArrayVar(&opts.SSHExtraArgs, "ssh-opt", opts.SSHExtraArgs, "Extra ssh options/flags passed through to OpenSSH (repeatable)")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", opts.Timeout, "SSH command timeout")
	cmd.Flags().StringVar(&opts.Ensure, "ensure", opts.Ensure, "Lifecycle state to enforce: present or absent")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: table or json")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write the JSON result to this path")
	_ = cmd.RegisterFlagCompletionFunc("ensure", cobra.FixedCompletions([]string{"present", "absent"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Ops Agent Bootstrap Flags")
	return cmd
}

func newOpsAgentEnrollCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Approve and promote agent enrollment",
	}
	cmd.AddCommand(newOpsAgentEnrollApproveCommand())
	decorateCommandHelp(cmd, "Ops Agent Enroll Flags")
	return cmd
}

func newOpsAgentEnrollApproveCommand() *cobra.Command {
	opts := opsAgentEnrollApproveOptions{
		Tenant:  firstNonEmpty(os.Getenv("TORQUE_AGENT_TENANT"), heartbeat.DefaultTenant),
		NATSURL: firstNonEmpty(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER")),
		Format:  "table",
	}
	opts.Store = opsAgentStoreFlags{
		store:         firstNonEmpty(os.Getenv("TORQUE_AGENT_REGISTRY_STORE"), "file"),
		storePath:     firstNonEmpty(os.Getenv("TORQUE_AGENT_REGISTRY_STORE_PATH"), filepathDefaultAgentRegistry()),
		etcdEndpoints: heartbeat.ParseEtcdEndpoints(os.Getenv("TORQUE_ETCD_ENDPOINTS")),
		etcdPrefix:    firstNonEmpty(os.Getenv("TORQUE_ETCD_PREFIX"), heartbeat.DefaultStorePrefix),
		storeTimeout:  5 * time.Second,
	}
	cmd := &cobra.Command{
		Use:   "approve <agent-id>",
		Short: "Approve one enrolled agent and promote its target toward durable NATS execution",
		Args:  cobra.ExactArgs(1),
		Example: `  torque ops agent enroll approve agent/mysql-01 --targets ./targetgraph.yaml --target host/mysql-01 --nats-url nats://141.105.65.227:4222
  torque ops agent enroll approve agent/mysql-01 --targets ./targetgraph.yaml --target host/mysql-01 --update-store --store file --store-path ./.torque/ops/agent-registry.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := runOpsAgentEnrollApprove(cmd.Context(), args[0], opts)
			if result != nil {
				if err := renderOpsAgentEnrollApproveOutput(cmd.OutOrStdout(), *result, opts.Format); err != nil {
					return err
				}
			}
			if err != nil {
				return err
			}
			if result != nil && strings.TrimSpace(result.Status) != "succeeded" {
				return fmt.Errorf("agent enrollment approval failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Targets, "targets", "", "TargetGraph YAML file to promote in place")
	cmd.Flags().StringVar(&opts.Out, "out", "", "Write the promoted TargetGraph to this path instead of overwriting --targets")
	cmd.Flags().StringVar(&opts.TargetID, "target", "", "Stable target ID to bind the approved agent to")
	cmd.Flags().StringVar(&opts.Tenant, "tenant", opts.Tenant, "Tenant namespace for the durable transport subject")
	cmd.Flags().StringVar(&opts.NATSURL, "nats-url", opts.NATSURL, "NATS server URL to record in the durable transport when creating one")
	cmd.Flags().BoolVar(&opts.UpdateStore, "update-store", false, "Update the compact registry enrollment state to approved")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: table or json")
	addOpsAgentStoreFlags(cmd, &opts.Store)
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Ops Agent Enroll Approve Flags")
	return cmd
}

func runOpsAgentBootstrap(ctx context.Context, opts opsAgentBootstrapOptions) (*opsAgentBootstrapResult, error) {
	ensure := strings.ToLower(strings.TrimSpace(opts.Ensure))
	if ensure == "" {
		ensure = "present"
	}
	if ensure != "present" && ensure != "absent" {
		return nil, fmt.Errorf("--ensure must be present or absent")
	}
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("--timeout must be greater than zero")
	}
	plan, err := resolveOpsAgentBootstrapPlan(opts)
	if err != nil {
		return nil, err
	}
	result := &opsAgentBootstrapResult{
		APIVersion:        opsAgentBootstrapAPIVersion,
		Kind:              opsAgentBootstrapKind,
		Status:            "succeeded",
		Ensure:            ensure,
		TargetID:          plan.TargetID,
		AgentID:           plan.AgentID,
		WorkerID:          plan.WorkerID,
		Tenant:            plan.Tenant,
		RemoteTarget:      plan.SSHTarget,
		AssignmentSubject: natstransport.AssignmentSubject(plan.Tenant, plan.TargetID),
		Labels:            plan.Labels,
		Paths: opsAgentBootstrapPaths{
			AgentBinary:   strings.TrimSpace(opts.RemoteBin),
			EnvFile:       strings.TrimSpace(opts.RemoteEnv),
			HeartbeatUnit: opsAgentHeartbeatUnitPath(opts.ServicePrefix),
			WorkerUnit:    opsAgentWorkerUnitPath(opts.ServicePrefix),
			StateDir:      strings.TrimSpace(opts.RemoteStateDir),
			NATSCreds:     strings.TrimSpace(opts.RemoteCreds),
			NATSNKey:      strings.TrimSpace(opts.RemoteNKey),
		},
	}
	client, err := newOpsAgentBootstrapSSHClient(sshtransport.Config{
		Target:       plan.SSHTarget,
		IdentityFile: plan.SSHIdentity,
		ExtraArgs:    append([]string(nil), opts.SSHExtraArgs...),
		Timeout:      opts.Timeout,
		RedactValues: []string{opts.NATSURL},
	})
	if err != nil {
		return result, err
	}
	if err := opsAgentAppendReceipt(result, client.Connect(ctx), "connect"); err != nil {
		return result, err
	}
	if ensure == "absent" {
		if err := runOpsAgentBootstrapAbsent(ctx, client, result, opts); err != nil {
			result.Status = "failed"
			return result, err
		}
		return result, nil
	}
	if strings.TrimSpace(opts.NATSURL) == "" {
		return result, fmt.Errorf("--nats-url is required when --ensure=present")
	}
	platform, err := detectOpsAgentRemotePlatform(ctx, client)
	if err != nil {
		result.Status = "failed"
		return result, err
	}
	result.Platform = platform
	binary, err := buildOpsAgentBootstrapBinary(ctx, opts, platform)
	if err != nil {
		result.Status = "failed"
		return result, err
	}
	if binary.Cleanup != nil {
		defer binary.Cleanup()
	}
	if err := runOpsAgentBootstrapPresent(ctx, client, result, opts, plan, binary.Path); err != nil {
		result.Status = "failed"
		return result, err
	}
	return result, nil
}

func resolveOpsAgentBootstrapPlan(opts opsAgentBootstrapOptions) (opsAgentBootstrapPlan, error) {
	targetID := strings.TrimSpace(opts.TargetID)
	sshTarget := strings.TrimSpace(opts.Target)
	sshIdentity := strings.TrimSpace(opts.SSHIdentity)
	labels := map[string]string{}
	if strings.TrimSpace(opts.Targets) != "" {
		if targetID == "" {
			return opsAgentBootstrapPlan{}, fmt.Errorf("--target-id is required when --targets is set")
		}
		graph, err := targetgraph.LoadFile(opts.Targets)
		if err != nil {
			return opsAgentBootstrapPlan{}, err
		}
		target, ok := opsTargetByID(graph, targetID)
		if !ok {
			return opsAgentBootstrapPlan{}, fmt.Errorf("target %q was not found in %s", targetID, opts.Targets)
		}
		directTarget, err := resolveOpsExecDirectTarget(graph, target)
		if err != nil {
			return opsAgentBootstrapPlan{}, err
		}
		if directTarget.TransportKind != "ssh" {
			return opsAgentBootstrapPlan{}, fmt.Errorf("target %s transport kind %q does not support SSH bootstrap", targetID, directTarget.TransportKind)
		}
		if sshTarget == "" {
			sshTarget = directTarget.TargetValue
		}
		if sshIdentity == "" {
			sshIdentity = strings.TrimSpace(directTarget.SSHIdentityHint)
		}
		for key, value := range target.Labels {
			labels[key] = value
		}
	}
	extraLabels, err := inventory.ParseSelector(opts.Labels)
	if err != nil {
		return opsAgentBootstrapPlan{}, err
	}
	for key, value := range extraLabels {
		labels[key] = value
	}
	if sshTarget == "" {
		return opsAgentBootstrapPlan{}, fmt.Errorf("--target or --targets is required")
	}
	if targetID == "" {
		targetID = opsAgentDefaultTargetID(sshTarget)
	}
	agentID := firstNonEmpty(strings.TrimSpace(opts.AgentID), "agent/"+opsExecNodeToken(targetID))
	workerID := firstNonEmpty(strings.TrimSpace(opts.WorkerID), agentID+"-worker")
	return opsAgentBootstrapPlan{
		SSHIdentity: sshIdentity,
		SSHTarget:   sshTarget,
		TargetID:    targetID,
		AgentID:     agentID,
		WorkerID:    workerID,
		Tenant:      firstNonEmpty(strings.TrimSpace(opts.Tenant), heartbeat.DefaultTenant),
		Labels:      labels,
	}, nil
}

func detectOpsAgentRemotePlatform(ctx context.Context, client opsAgentSSHClient) (opsAgentRemotePlatform, error) {
	rec := client.Run(ctx, "uname -s && uname -m")
	if rec.Status != "succeeded" {
		return opsAgentRemotePlatform{}, fmt.Errorf("detect remote platform: %s", firstNonEmpty(strings.TrimSpace(rec.Error), strings.TrimSpace(rec.Stderr), rec.Status))
	}
	lines := strings.Fields(strings.TrimSpace(rec.Stdout))
	if len(lines) < 2 {
		return opsAgentRemotePlatform{}, fmt.Errorf("detect remote platform: expected uname output, got %q", rec.Stdout)
	}
	osName := strings.ToLower(strings.TrimSpace(lines[0]))
	archName := strings.ToLower(strings.TrimSpace(lines[1]))
	goos, ok := map[string]string{
		"linux": "linux",
	}[osName]
	if !ok {
		return opsAgentRemotePlatform{}, fmt.Errorf("unsupported remote operating system %q", osName)
	}
	goarch, ok := map[string]string{
		"x86_64":  "amd64",
		"amd64":   "amd64",
		"aarch64": "arm64",
		"arm64":   "arm64",
	}[archName]
	if !ok {
		return opsAgentRemotePlatform{}, fmt.Errorf("unsupported remote architecture %q", archName)
	}
	return opsAgentRemotePlatform{
		OS:     osName,
		Arch:   archName,
		GOOS:   goos,
		GOARCH: goarch,
	}, nil
}

func resolveOpsAgentBootstrapBinary(ctx context.Context, opts opsAgentBootstrapOptions, platform opsAgentRemotePlatform) (opsAgentBinaryRef, error) {
	if path := strings.TrimSpace(opts.AgentBinary); path != "" {
		if _, err := os.Stat(path); err != nil {
			return opsAgentBinaryRef{}, fmt.Errorf("stat --agent-binary: %w", err)
		}
		return opsAgentBinaryRef{Path: path}, nil
	}
	if !opts.BuildAgentBinary {
		return opsAgentBinaryRef{}, fmt.Errorf("--agent-binary is required when --build-agent-binary=false")
	}
	if _, err := os.Stat("cmd/torque-agent"); err != nil {
		return opsAgentBinaryRef{}, fmt.Errorf("build torque-agent from source: %w", err)
	}
	dir, err := os.MkdirTemp("", "torque-agent-bootstrap-build-*")
	if err != nil {
		return opsAgentBinaryRef{}, err
	}
	outPath := filepath.Join(dir, "torque-agent")
	cmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, "./cmd/torque-agent")
	cmd.Env = append(os.Environ(),
		"GOOS="+platform.GOOS,
		"GOARCH="+platform.GOARCH,
		"CGO_ENABLED=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(dir)
		return opsAgentBinaryRef{}, fmt.Errorf("build torque-agent for %s/%s: %w: %s", platform.GOOS, platform.GOARCH, err, strings.TrimSpace(string(output)))
	}
	return opsAgentBinaryRef{
		Path: outPath,
		Cleanup: func() {
			_ = os.RemoveAll(dir)
		},
	}, nil
}

func runOpsAgentBootstrapPresent(ctx context.Context, client opsAgentSSHClient, result *opsAgentBootstrapResult, opts opsAgentBootstrapOptions, plan opsAgentBootstrapPlan, localBinary string) error {
	if err := opsAgentAppendReceipt(result, client.Run(ctx, fmt.Sprintf(
		"mkdir -p %s %s %s && touch %s && chmod 0700 %s",
		sshtransport.ShellQuote(filepath.Dir(opts.RemoteEnv)),
		sshtransport.ShellQuote(filepath.Dir(opts.RemoteBin)),
		sshtransport.ShellQuote(opts.RemoteStateDir),
		sshtransport.ShellQuote(filepath.Join(opts.RemoteStateDir, ".bootstrap")),
		sshtransport.ShellQuote(opts.RemoteStateDir),
	)), "prepare-remote-paths"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Upload(ctx, localBinary, opts.RemoteBin), "upload-agent-binary"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Run(ctx, "chmod 0755 "+sshtransport.ShellQuote(opts.RemoteBin)), "chmod-agent-binary"); err != nil {
		return err
	}
	if err := maybeUploadOpsAgentAuthMaterial(ctx, client, result, opts.NATSCredsFile, opts.RemoteCreds, "upload-nats-creds"); err != nil {
		return err
	}
	if err := maybeUploadOpsAgentAuthMaterial(ctx, client, result, opts.NATSNKeyFile, opts.RemoteNKey, "upload-nats-nkey"); err != nil {
		return err
	}
	stageDir, err := os.MkdirTemp("", "torque-agent-bootstrap-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	envPath := filepath.Join(stageDir, "ops-agent.env")
	if err := os.WriteFile(envPath, []byte(renderOpsAgentBootstrapEnv(opts, plan, result.AssignmentSubject)), 0o600); err != nil {
		return err
	}
	heartbeatUnitPath := filepath.Join(stageDir, "torque-agent-heartbeat.service")
	if err := os.WriteFile(heartbeatUnitPath, []byte(renderOpsAgentHeartbeatService(opts)), 0o644); err != nil {
		return err
	}
	workerUnitPath := filepath.Join(stageDir, "torque-agent-worker.service")
	if err := os.WriteFile(workerUnitPath, []byte(renderOpsAgentWorkerService(opts)), 0o644); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Upload(ctx, envPath, opts.RemoteEnv), "upload-agent-env"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Run(ctx, "chmod 0600 "+sshtransport.ShellQuote(opts.RemoteEnv)), "chmod-agent-env"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Upload(ctx, heartbeatUnitPath, result.Paths.HeartbeatUnit), "upload-heartbeat-unit"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Upload(ctx, workerUnitPath, result.Paths.WorkerUnit), "upload-worker-unit"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Run(ctx, "chmod 0644 "+sshtransport.ShellQuote(result.Paths.HeartbeatUnit)+" "+sshtransport.ShellQuote(result.Paths.WorkerUnit)), "chmod-systemd-units"); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Run(ctx, "systemctl daemon-reload"), "systemd-daemon-reload"); err != nil {
		return err
	}
	heartbeatService, workerService := opsAgentServiceNames(opts.ServicePrefix)
	if err := opsAgentAppendReceipt(result, client.Run(ctx, "systemctl enable --now "+heartbeatService+" "+workerService), "enable-and-start-services"); err != nil {
		return err
	}
	if err := waitForOpsAgentServicesActive(ctx, client, result, opts.ServicePrefix, opts.Timeout); err != nil {
		return err
	}
	return opsAgentAppendReceipt(result, client.Run(ctx, "systemctl is-enabled "+heartbeatService+" "+workerService), "verify-services-enabled")
}

func runOpsAgentBootstrapAbsent(ctx context.Context, client opsAgentSSHClient, result *opsAgentBootstrapResult, opts opsAgentBootstrapOptions) error {
	heartbeatService, workerService := opsAgentServiceNames(opts.ServicePrefix)
	stop := client.Run(ctx, "systemctl disable --now "+heartbeatService+" "+workerService+" || true")
	result.Receipts = append(result.Receipts, stop)
	remove := client.Run(ctx, fmt.Sprintf(
		"rm -f %s %s %s %s %s && systemctl daemon-reload && systemctl reset-failed %s %s >/dev/null 2>&1 || true && rm -rf %s",
		sshtransport.ShellQuote(result.Paths.HeartbeatUnit),
		sshtransport.ShellQuote(result.Paths.WorkerUnit),
		sshtransport.ShellQuote(opts.RemoteEnv),
		sshtransport.ShellQuote(opts.RemoteCreds),
		sshtransport.ShellQuote(opts.RemoteNKey),
		heartbeatService,
		workerService,
		sshtransport.ShellQuote(opts.RemoteStateDir),
	))
	result.Receipts = append(result.Receipts, remove)
	if remove.Status != "succeeded" {
		return fmt.Errorf("remove remote agent files: %s", firstNonEmpty(strings.TrimSpace(remove.Error), strings.TrimSpace(remove.Stderr), remove.Status))
	}
	return nil
}

func maybeUploadOpsAgentAuthMaterial(ctx context.Context, client opsAgentSSHClient, result *opsAgentBootstrapResult, localPath string, remotePath string, operation string) error {
	localPath = strings.TrimSpace(localPath)
	remotePath = strings.TrimSpace(remotePath)
	if localPath == "" || remotePath == "" {
		return nil
	}
	if _, err := os.Stat(localPath); err != nil {
		return err
	}
	if err := opsAgentAppendReceipt(result, client.Upload(ctx, localPath, remotePath), operation); err != nil {
		return err
	}
	return opsAgentAppendReceipt(result, client.Run(ctx, "chmod 0600 "+sshtransport.ShellQuote(remotePath)), operation+"-chmod")
}

func waitForOpsAgentServicesActive(ctx context.Context, client opsAgentSSHClient, result *opsAgentBootstrapResult, prefix string, timeout time.Duration) error {
	heartbeatService, workerService := opsAgentServiceNames(prefix)
	deadline := time.Now().Add(timeout)
	var last transport.OperationResult
	for {
		last = client.Run(ctx, "systemctl is-active "+heartbeatService+" "+workerService)
		last.Operation = "verify-services-active"
		if strings.TrimSpace(last.Status) == "succeeded" {
			result.Receipts = append(result.Receipts, last)
			return nil
		}
		if time.Now().After(deadline) {
			result.Receipts = append(result.Receipts, last)
			return fmt.Errorf("verify-services-active: %s", firstNonEmpty(strings.TrimSpace(last.Error), strings.TrimSpace(last.Stdout), strings.TrimSpace(last.Stderr), last.Status))
		}
		select {
		case <-ctx.Done():
			result.Receipts = append(result.Receipts, last)
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func opsAgentAppendReceipt(result *opsAgentBootstrapResult, rec transport.OperationResult, action string) error {
	if result == nil {
		return fmt.Errorf("%s: missing result sink", action)
	}
	rec.Operation = action
	result.Receipts = append(result.Receipts, rec)
	if rec.Status != "succeeded" {
		return fmt.Errorf("%s: %s", action, firstNonEmpty(strings.TrimSpace(rec.Error), strings.TrimSpace(rec.Stderr), rec.Status))
	}
	return nil
}

func renderOpsAgentBootstrapEnv(opts opsAgentBootstrapOptions, plan opsAgentBootstrapPlan, assignmentSubject string) string {
	values := map[string]string{
		"TORQUE_NATS_URL":                 strings.TrimSpace(opts.NATSURL),
		"TORQUE_NATS_JETSTREAM":           "true",
		"TORQUE_NATS_DELIVERY":            natstransport.DeliveryJetStream,
		"TORQUE_NATS_SUBJECT":             assignmentSubject,
		"TORQUE_NATS_DURABLE":             "torque-agent-" + natstransport.NormalizeSubjectToken(plan.TargetID, "target"),
		"TORQUE_AGENT_ASSIGNMENT_LEDGER":  filepath.Join(opts.RemoteStateDir, "assignments.sqlite"),
		"TORQUE_AGENT_ID":                 plan.AgentID,
		"TORQUE_AGENT_WORKER_ID":          plan.WorkerID,
		"TORQUE_AGENT_TENANT":             plan.Tenant,
		"TORQUE_AGENT_TARGET_ID":          plan.TargetID,
		"TORQUE_AGENT_LABELS":             formatOpsAgentLabels(plan.Labels),
		"TORQUE_AGENT_HEARTBEAT_INTERVAL": "15s",
		"TORQUE_AGENT_SLOTS":              "1",
		"TORQUE_AGENT_WORKER_SLOTS":       "1",
	}
	if creds := strings.TrimSpace(opts.RemoteCreds); creds != "" && strings.TrimSpace(opts.NATSCredsFile) != "" {
		values["TORQUE_NATS_CREDS"] = creds
	}
	if nkey := strings.TrimSpace(opts.RemoteNKey); nkey != "" && strings.TrimSpace(opts.NATSNKeyFile) != "" {
		values["TORQUE_NATS_NKEY"] = nkey
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(values[key]) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(values[key])
		b.WriteByte('\n')
	}
	return b.String()
}

func renderOpsAgentHeartbeatService(opts opsAgentBootstrapOptions) string {
	return strings.Join([]string{
		"[Unit]",
		"Description=Torque Ops Agent Heartbeat",
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"EnvironmentFile=" + opts.RemoteEnv,
		"WorkingDirectory=" + opts.RemoteStateDir,
		"ExecStart=" + opts.RemoteBin + " nats heartbeat",
		"Restart=always",
		"RestartSec=5",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	}, "\n")
}

func renderOpsAgentWorkerService(opts opsAgentBootstrapOptions) string {
	heartbeatService, _ := opsAgentServiceNames(opts.ServicePrefix)
	return strings.Join([]string{
		"[Unit]",
		"Description=Torque Ops Agent Worker",
		"After=network-online.target " + heartbeatService,
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"EnvironmentFile=" + opts.RemoteEnv,
		"WorkingDirectory=" + opts.RemoteStateDir,
		"ExecStart=" + opts.RemoteBin + " nats worker",
		"Restart=always",
		"RestartSec=5",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	}, "\n")
}

func renderOpsAgentBootstrapOutput(out io.Writer, result opsAgentBootstrapResult, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "STATUS\t%s\n", result.Status)
		fmt.Fprintf(tw, "ENSURE\t%s\n", result.Ensure)
		fmt.Fprintf(tw, "TARGET\t%s\n", result.TargetID)
		fmt.Fprintf(tw, "AGENT\t%s\n", result.AgentID)
		fmt.Fprintf(tw, "WORKER\t%s\n", result.WorkerID)
		fmt.Fprintf(tw, "SSH\t%s\n", result.RemoteTarget)
		if result.AssignmentSubject != "" {
			fmt.Fprintf(tw, "SUBJECT\t%s\n", result.AssignmentSubject)
		}
		if result.Platform.GOOS != "" || result.Platform.GOARCH != "" {
			fmt.Fprintf(tw, "PLATFORM\t%s/%s\n", result.Platform.GOOS, result.Platform.GOARCH)
		}
		fmt.Fprintf(tw, "RECEIPTS\t%d\n", len(result.Receipts))
		return tw.Flush()
	case "json":
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(raw))
		return err
	default:
		return fmt.Errorf("--format must be table or json")
	}
}

func runOpsAgentEnrollApprove(ctx context.Context, agentID string, opts opsAgentEnrollApproveOptions) (*opsAgentEnrollApproveResult, error) {
	if strings.TrimSpace(opts.Targets) == "" {
		return nil, fmt.Errorf("--targets is required")
	}
	if strings.TrimSpace(opts.TargetID) == "" {
		return nil, fmt.Errorf("--target is required")
	}
	graph, err := targetgraph.LoadFile(opts.Targets)
	if err != nil {
		return nil, err
	}
	targetIndex := -1
	for i, target := range graph.Targets {
		if target.ID == opts.TargetID {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		return nil, fmt.Errorf("target %q was not found in %s", opts.TargetID, opts.Targets)
	}
	target := graph.Targets[targetIndex]
	transportID, transportCfg, err := opsAgentPromotedTransport(graph, target, strings.TrimSpace(agentID), opts)
	if err != nil {
		return nil, err
	}
	graph.Targets[targetIndex].DurableTransportRef = transportID
	if !opsAgentGraphHasTransport(graph, transportID) {
		graph.Transports = append(graph.Transports, transportCfg)
	} else {
		for i, transport := range graph.Transports {
			if transport.ID == transportID {
				graph.Transports[i] = transportCfg
				break
			}
		}
	}
	outputPath := firstNonEmpty(strings.TrimSpace(opts.Out), strings.TrimSpace(opts.Targets))
	if err := graph.WriteFile(outputPath); err != nil {
		return nil, err
	}
	var enrollment *heartbeat.EnrollmentStatus
	if opts.UpdateStore {
		status, err := opsAgentApproveEnrollmentInStore(ctx, opts, agentID, opts.TargetID)
		if err != nil {
			return nil, err
		}
		enrollment = status
	}
	result := &opsAgentEnrollApproveResult{
		APIVersion:          opsAgentEnrollAPIVersion,
		Kind:                opsAgentEnrollKind,
		Status:              "succeeded",
		AgentID:             strings.TrimSpace(agentID),
		TargetID:            strings.TrimSpace(opts.TargetID),
		Tenant:              firstNonEmpty(strings.TrimSpace(opts.Tenant), heartbeat.DefaultTenant),
		GraphPath:           strings.TrimSpace(opts.Targets),
		OutputPath:          outputPath,
		DurableTransportRef: transportID,
		AssignmentSubject:   natstransport.AssignmentSubject(firstNonEmpty(strings.TrimSpace(opts.Tenant), heartbeat.DefaultTenant), opts.TargetID),
		Transport:           transportCfg,
		Enrollment:          enrollment,
	}
	return result, nil
}

func opsAgentPromotedTransport(graph *targetgraph.TargetGraph, target targetgraph.Target, agentID string, opts opsAgentEnrollApproveOptions) (string, targetgraph.Transport, error) {
	tenant := firstNonEmpty(strings.TrimSpace(opts.Tenant), heartbeat.DefaultTenant)
	transportID := strings.TrimSpace(target.DurableTransportRef)
	if transportID == "" {
		if transport, ok := opsTransportByID(graph, target.TransportRef); ok && strings.EqualFold(strings.TrimSpace(transport.Kind), "nats") {
			transportID = strings.TrimSpace(target.TransportRef)
		}
	}
	if transportID == "" {
		transportID = "nats/" + opsExecNodeToken(target.ID)
	}
	transportCfg, ok := opsTransportByID(graph, transportID)
	if ok && !strings.EqualFold(strings.TrimSpace(transportCfg.Kind), "nats") {
		return "", targetgraph.Transport{}, fmt.Errorf("durable transport %q exists with kind %q, want nats", transportID, transportCfg.Kind)
	}
	if !ok {
		if strings.TrimSpace(opts.NATSURL) == "" {
			return "", targetgraph.Transport{}, fmt.Errorf("--nats-url is required when creating durable transport %q", transportID)
		}
		transportCfg = targetgraph.Transport{
			ID:   transportID,
			Kind: "nats",
			URL:  strings.TrimSpace(opts.NATSURL),
			Config: map[string]any{
				"subject":  natstransport.AssignmentSubject(tenant, target.ID),
				"targetId": target.ID,
				"tenant":   tenant,
				"agentId":  strings.TrimSpace(agentID),
				"delivery": natstransport.DeliveryJetStream,
			},
		}
		return transportID, transportCfg, nil
	}
	if strings.TrimSpace(transportCfg.URL) == "" && strings.TrimSpace(opts.NATSURL) != "" {
		transportCfg.URL = strings.TrimSpace(opts.NATSURL)
	}
	if transportCfg.Config == nil {
		transportCfg.Config = map[string]any{}
	}
	transportCfg.Config["subject"] = natstransport.AssignmentSubject(tenant, target.ID)
	transportCfg.Config["targetId"] = target.ID
	transportCfg.Config["tenant"] = tenant
	transportCfg.Config["agentId"] = strings.TrimSpace(agentID)
	transportCfg.Config["delivery"] = natstransport.DeliveryJetStream
	return transportID, transportCfg, nil
}

func opsAgentApproveEnrollmentInStore(ctx context.Context, opts opsAgentEnrollApproveOptions, agentID string, targetID string) (*heartbeat.EnrollmentStatus, error) {
	store, err := openOpsAgentStore(ctx, opts.Store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	records, err := store.List(ctx, opts.Tenant)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if strings.TrimSpace(record.AgentID) != strings.TrimSpace(agentID) {
			continue
		}
		if strings.TrimSpace(record.Heartbeat.TargetID) != "" && strings.TrimSpace(record.Heartbeat.TargetID) != strings.TrimSpace(targetID) {
			return nil, fmt.Errorf("agent %s is bound to target %s in the compact registry, not %s", agentID, record.Heartbeat.TargetID, targetID)
		}
		record.Status.Enrollment = heartbeat.EnrollmentStatus{
			State:      heartbeat.EnrollmentStateApproved,
			ApprovedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := store.Put(ctx, record); err != nil {
			return nil, err
		}
		enrollment := record.Status.Enrollment
		return &enrollment, nil
	}
	return nil, fmt.Errorf("agent %s was not found in the compact registry store", agentID)
}

func renderOpsAgentEnrollApproveOutput(out io.Writer, result opsAgentEnrollApproveResult, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table":
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "STATUS\t%s\n", result.Status)
		fmt.Fprintf(tw, "AGENT\t%s\n", result.AgentID)
		fmt.Fprintf(tw, "TARGET\t%s\n", result.TargetID)
		fmt.Fprintf(tw, "DURABLE_REF\t%s\n", result.DurableTransportRef)
		fmt.Fprintf(tw, "SUBJECT\t%s\n", result.AssignmentSubject)
		fmt.Fprintf(tw, "OUTPUT\t%s\n", result.OutputPath)
		if result.Enrollment != nil {
			fmt.Fprintf(tw, "ENROLLMENT\t%s\n", result.Enrollment.State)
		}
		return tw.Flush()
	case "json":
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(raw))
		return err
	default:
		return fmt.Errorf("--format must be table or json")
	}
}

func opsAgentGraphHasTransport(graph *targetgraph.TargetGraph, transportID string) bool {
	_, ok := opsTransportByID(graph, transportID)
	return ok
}

func formatOpsAgentLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(labels[key])
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func opsAgentDefaultTargetID(sshTarget string) string {
	sshTarget = strings.TrimPrefix(strings.TrimSpace(sshTarget), "ssh://")
	sshTarget = strings.TrimPrefix(sshTarget, "root@")
	if at := strings.LastIndex(sshTarget, "@"); at >= 0 {
		sshTarget = sshTarget[at+1:]
	}
	token := natstransport.NormalizeSubjectToken(strings.NewReplacer(":", "-", ".", "-", "/", "-").Replace(sshTarget), "host")
	return "host/" + token
}

func opsAgentServiceNames(prefix string) (string, string) {
	prefix = natstransport.NormalizeSubjectToken(strings.TrimSpace(prefix), "torque-agent")
	return prefix + "-heartbeat.service", prefix + "-worker.service"
}

func opsAgentHeartbeatUnitPath(prefix string) string {
	heartbeatService, _ := opsAgentServiceNames(prefix)
	return "/etc/systemd/system/" + heartbeatService
}

func opsAgentWorkerUnitPath(prefix string) string {
	_, workerService := opsAgentServiceNames(prefix)
	return "/etc/systemd/system/" + workerService
}
