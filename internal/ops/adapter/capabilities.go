package adapter

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"
)

const (
	CapabilityAPIVersion = "torque.dev/ops/adapter-capabilities/v1"
	CapabilityListKind   = "AdapterCapabilityList"
)

type CapabilityList struct {
	APIVersion  string            `json:"apiVersion"`
	Kind        string            `json:"kind"`
	GeneratedAt string            `json:"generatedAt"`
	Summary     CapabilitySummary `json:"summary"`
	Adapters    []Capability      `json:"adapters"`
}

type CapabilitySummary struct {
	Total          int `json:"total"`
	Implemented    int `json:"implemented"`
	Planned        int `json:"planned"`
	Probed         int `json:"probed,omitempty"`
	ProbeSucceeded int `json:"probeSucceeded,omitempty"`
	ProbeFailed    int `json:"probeFailed,omitempty"`
	ProbeSkipped   int `json:"probeSkipped,omitempty"`
}

type Capability struct {
	Adapter             string       `json:"adapter"`
	Status              string       `json:"status"`
	Classification      string       `json:"classification"`
	TargetTypes         []string     `json:"targetTypes"`
	Transports          []string     `json:"transports"`
	Mutating            bool         `json:"mutating"`
	RequiredPrivilege   string       `json:"requiredPrivilege"`
	Idempotence         string       `json:"idempotence"`
	CheckMode           string       `json:"checkMode"`
	DiffQuality         string       `json:"diffQuality"`
	SupportedPhases     []string     `json:"supportedPhases"`
	EvidenceArtifacts   []string     `json:"evidenceArtifacts"`
	RequiredPolicy      []string     `json:"requiredPolicy,omitempty"`
	Touches             []string     `json:"touches,omitempty"`
	SecretInputs        []string     `json:"secretInputs,omitempty"`
	NetworkDestinations []string     `json:"networkDestinations,omitempty"`
	Description         string       `json:"description"`
	Probe               *ProbeResult `json:"probe,omitempty"`
}

type ProbeOptions struct {
	Target       string
	Transport    string
	Timeout      time.Duration
	IdentityFile string
	SSHExtraArgs []string
}

type ProbeResult struct {
	Status       string       `json:"status"`
	Transport    string       `json:"transport"`
	TargetDigest string       `json:"targetDigest,omitempty"`
	CheckedAt    string       `json:"checkedAt"`
	Checks       []ProbeCheck `json:"checks"`
	Reason       string       `json:"reason,omitempty"`
}

type ProbeCheck struct {
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	Status        string `json:"status"`
	CommandDigest string `json:"commandDigest,omitempty"`
	ExitCode      int    `json:"exitCode,omitempty"`
	TimedOut      bool   `json:"timedOut,omitempty"`
	Stdout        string `json:"stdout,omitempty"`
	Stderr        string `json:"stderr,omitempty"`
	Error         string `json:"error,omitempty"`
}

type probeCommand struct {
	Name     string
	Command  string
	Required bool
}

type probeClient interface {
	TargetDigest() string
	Connect(ctx context.Context) transport.OperationResult
	Run(ctx context.Context, command string) transport.OperationResult
}

func BuildCapabilityList(ctx context.Context, adapterName string, opts ProbeOptions) (*CapabilityList, error) {
	caps, err := Select(adapterName)
	if err != nil {
		return nil, err
	}
	out := &CapabilityList{
		APIVersion:  CapabilityAPIVersion,
		Kind:        CapabilityListKind,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Adapters:    caps,
	}
	if strings.TrimSpace(opts.Target) != "" {
		for i := range out.Adapters {
			probe := Probe(ctx, out.Adapters[i], opts)
			out.Adapters[i].Probe = &probe
		}
	}
	out.Summary = summarize(out.Adapters)
	return out, nil
}

func Select(adapterName string) ([]Capability, error) {
	adapterName = strings.TrimSpace(adapterName)
	all := Definitions()
	if adapterName == "" {
		return all, nil
	}
	for _, cap := range all {
		if cap.Adapter == adapterName {
			return []Capability{cap}, nil
		}
	}
	return nil, fmt.Errorf("unknown adapter %q (known: %s)", adapterName, strings.Join(KnownAdapterNames(), ", "))
}

func Definitions() []Capability {
	out := []Capability{
		{
			Adapter:           "host.command.run",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "command execution as configured target user",
			Idempotence:       "operator-declared",
			CheckMode:         "bounded",
			DiffQuality:       "unsupported",
			SupportedPhases:   []string{"observe", "plan", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-command-observe.json", "host-command-plan.json", "host-command-execute.json", "host-command-verify.json", "host-command.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"processes", "files and services reachable from command"},
			SecretInputs:      []string{"secret refs redacted when emitted by transport"},
			Description:       "Run one bounded command through local or SSH transport with observe/plan/execute/verify receipts.",
		},
		planned("host.file.render", "guarded", "conditional", "exact", []string{"file path"}, []string{"host-file-observe.json", "host-file-plan.json", "host-file-diff.json", "host-file-apply.json", "host-file-verify.json"}),
		planned("host.file.copy", "guarded", "conditional", "exact", []string{"source file", "target file path"}, []string{"host-file-copy-observe.json", "host-file-copy-plan.json", "host-file-copy-apply.json", "host-file-copy-verify.json"}),
		planned("host.package.install", "guarded", "conditional", "bounded", []string{"package database", "package manager"}, []string{"host-package-observe.json", "host-package-plan.json", "host-package-apply.json", "host-package-verify.json"}),
		planned("host.service.manage", "guarded", "conditional", "bounded", []string{"service manager", "unit state"}, []string{"host-service-observe.json", "host-service-plan.json", "host-service-apply.json", "host-service-verify.json"}),
		planned("host.user.manage", "guarded", "conditional", "bounded", []string{"passwd/group database"}, []string{"host-user-observe.json", "host-user-plan.json", "host-user-apply.json", "host-user-verify.json"}),
		planned("host.cron.manage", "guarded", "conditional", "exact", []string{"crontab", "cron.d files"}, []string{"host-cron-observe.json", "host-cron-plan.json", "host-cron-diff.json", "host-cron-apply.json", "host-cron-verify.json"}),
		planned("host.systemd.unit", "guarded", "conditional", "exact", []string{"systemd unit files", "systemd manager"}, []string{"host-systemd-observe.json", "host-systemd-plan.json", "host-systemd-diff.json", "host-systemd-apply.json", "host-systemd-verify.json", "journal-evidence.json"}),
	}
	for i := range out {
		out[i] = cloneCapability(out[i])
	}
	return out
}

func KnownAdapterNames() []string {
	defs := Definitions()
	names := make([]string, 0, len(defs))
	for _, cap := range defs {
		names = append(names, cap.Adapter)
	}
	sort.Strings(names)
	return names
}

func Probe(ctx context.Context, cap Capability, opts ProbeOptions) ProbeResult {
	transportKind := inferTransport(opts)
	result := ProbeResult{
		Status:    "skipped",
		Transport: transportKind,
		CheckedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(cap.Status) != "implemented" {
		result.Reason = "adapter is not implemented yet"
		return result
	}
	client, err := newProbeClient(opts, transportKind)
	if err != nil {
		result.Status = "failed"
		result.Reason = err.Error()
		return result
	}
	result.TargetDigest = client.TargetDigest()
	connect := resultFromOperation("connect", true, "", client.Connect(ctx))
	result.Checks = append(result.Checks, connect)
	if connect.Status != "succeeded" {
		result.Status = "failed"
		result.Reason = "target connection failed"
		return result
	}
	for _, probe := range probeCommandsFor(cap.Adapter) {
		op := client.Run(ctx, probe.Command)
		result.Checks = append(result.Checks, resultFromOperation(probe.Name, probe.Required, probe.Command, op))
	}
	result.Status = "succeeded"
	for _, check := range result.Checks {
		if check.Required && check.Status != "succeeded" {
			result.Status = "failed"
			if result.Reason == "" {
				result.Reason = check.Name + " failed"
			}
		}
	}
	return result
}

func planned(adapterName, classification, idempotence, diffQuality string, touches []string, artifacts []string) Capability {
	return Capability{
		Adapter:             adapterName,
		Status:              "planned",
		Classification:      classification,
		TargetTypes:         []string{"host"},
		Transports:          []string{"ssh", "nats-agent"},
		Mutating:            true,
		RequiredPrivilege:   plannedPrivilege(adapterName),
		Idempotence:         idempotence,
		CheckMode:           "deterministic-plan",
		DiffQuality:         diffQuality,
		SupportedPhases:     []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
		EvidenceArtifacts:   artifacts,
		RequiredPolicy:      []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
		Touches:             touches,
		SecretInputs:        []string{"secret refs only"},
		NetworkDestinations: []string{"selected host target"},
		Description:         "Planned host adapter contract for OPS-HOST backlog implementation.",
	}
}

func plannedPrivilege(adapterName string) string {
	switch adapterName {
	case "host.package.install", "host.service.manage", "host.user.manage", "host.systemd.unit":
		return "root or delegated sudo"
	default:
		return "target file ownership or delegated sudo"
	}
}

func inferTransport(opts ProbeOptions) string {
	if v := strings.ToLower(strings.TrimSpace(opts.Transport)); v != "" {
		return v
	}
	target := strings.TrimSpace(opts.Target)
	if strings.HasPrefix(target, "ssh://") || strings.Contains(target, "@") {
		return "ssh"
	}
	return "local"
}

func newProbeClient(opts ProbeOptions, transportKind string) (probeClient, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	target := strings.TrimSpace(opts.Target)
	switch strings.ToLower(strings.TrimSpace(transportKind)) {
	case "local", "localhost":
		if target == "" {
			target = "local://localhost"
		}
		return localtransport.New(localtransport.Config{
			Target:  target,
			Timeout: timeout,
		})
	case "ssh":
		if target == "" {
			return nil, fmt.Errorf("ssh probe requires --target")
		}
		identity := strings.TrimSpace(opts.IdentityFile)
		if identity == "" {
			identity = strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_IDENTITY"))
		}
		extra := append([]string(nil), opts.SSHExtraArgs...)
		if len(extra) == 0 {
			extra = strings.Fields(strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_OPTS")))
		}
		return sshtransport.New(sshtransport.Config{
			Target:       target,
			IdentityFile: identity,
			ExtraArgs:    extra,
			Timeout:      timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported probe transport %q", transportKind)
	}
}

func probeCommandsFor(adapterName string) []probeCommand {
	switch adapterName {
	case "host.command.run":
		return []probeCommand{
			{Name: "shell", Command: "printf torque-adapter-probe", Required: true},
			{Name: "redaction", Command: "printf 'password=torque-adapter-probe-secret\\n'", Required: true},
		}
	default:
		return nil
	}
}

func resultFromOperation(name string, required bool, command string, op transport.OperationResult) ProbeCheck {
	check := ProbeCheck{
		Name:          name,
		Required:      required,
		Status:        strings.TrimSpace(op.Status),
		CommandDigest: "",
		ExitCode:      op.ExitCode,
		TimedOut:      op.TimedOut,
		Stdout:        strings.TrimSpace(op.Stdout),
		Stderr:        strings.TrimSpace(op.Stderr),
		Error:         strings.TrimSpace(op.Error),
	}
	if strings.TrimSpace(command) != "" {
		check.CommandDigest = transport.ValueDigest(command)
	}
	return check
}

func summarize(caps []Capability) CapabilitySummary {
	var out CapabilitySummary
	out.Total = len(caps)
	for _, cap := range caps {
		switch strings.TrimSpace(cap.Status) {
		case "implemented":
			out.Implemented++
		case "planned":
			out.Planned++
		}
		if cap.Probe != nil {
			out.Probed++
			switch cap.Probe.Status {
			case "succeeded":
				out.ProbeSucceeded++
			case "failed":
				out.ProbeFailed++
			case "skipped":
				out.ProbeSkipped++
			}
		}
	}
	return out
}

func cloneCapability(in Capability) Capability {
	in.TargetTypes = append([]string(nil), in.TargetTypes...)
	in.Transports = append([]string(nil), in.Transports...)
	in.SupportedPhases = append([]string(nil), in.SupportedPhases...)
	in.EvidenceArtifacts = append([]string(nil), in.EvidenceArtifacts...)
	in.RequiredPolicy = append([]string(nil), in.RequiredPolicy...)
	in.Touches = append([]string(nil), in.Touches...)
	in.SecretInputs = append([]string(nil), in.SecretInputs...)
	in.NetworkDestinations = append([]string(nil), in.NetworkDestinations...)
	return in
}
