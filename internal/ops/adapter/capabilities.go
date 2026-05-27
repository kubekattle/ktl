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
			EvidenceArtifacts: []string{"host-command-observe.json", "host-command-plan.json", "host-command-execute.json", "host-command-fanout.json", "host-command-verify.json", "host-command.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"processes", "files and services reachable from command"},
			SecretInputs:      []string{"secret refs redacted when emitted by transport"},
			Description:       "Run one bounded command through local or SSH transport with observe/plan/execute/verify receipts.",
		},
		{
			Adapter:           "host.file.render",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "target file ownership or delegated sudo",
			Idempotence:       "content-digest",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-file-observe.json", "host-file-plan.json", "host-file-diff.json", "host-file-apply.json", "host-file-verify.json", "host-file-render.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"file path", "file mode", "file owner", "file group"},
			SecretInputs:      []string{"rendered content digest only in evidence"},
			Description:       "Render one file through local or SSH transport with exact digest diff, validation, owner/mode, and verify receipts.",
		},
		{
			Adapter:           "host.file.copy",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "target file ownership or delegated sudo",
			Idempotence:       "content-digest",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-file-copy-observe.json", "host-file-copy-plan.json", "host-file-copy-diff.json", "host-file-copy-apply.json", "host-file-copy-verify.json", "host-file-copy.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"source file", "target file path", "file mode", "file owner", "file group", "backup path"},
			SecretInputs:      []string{"copied content digest only in evidence"},
			Description:       "Copy one file through local or SSH transport with exact digest diff, validation, backup/restore, and verify receipts.",
		},
		{
			Adapter:           "host.package.install",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "root or delegated sudo",
			Idempotence:       "package-version-state",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-package-observe.json", "host-package-plan.json", "host-package-diff.json", "host-package-apply.json", "host-package-verify.json", "host-package.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"package database", "package manager", "package files", "service hooks"},
			SecretInputs:      []string{"package command output digests only in evidence"},
			Description:       "Install, upgrade, or remove one package through local or SSH transport with exact before/after package evidence.",
		},
		{
			Adapter:           "host.service.manage",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "root or delegated sudo for systemd units",
			Idempotence:       "service-state-enablement",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-service-observe.json", "host-service-plan.json", "host-service-diff.json", "host-service-apply.json", "host-service-verify.json", "host-service.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"service manager", "unit runtime state", "unit enablement"},
			SecretInputs:      []string{"service command output digests only in evidence"},
			Description:       "Start, stop, restart, enable, or disable one systemd unit through local or SSH transport with exact before/after service evidence.",
		},
		{
			Adapter:           "host.user.manage",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "root or delegated sudo for passwd/group databases",
			Idempotence:       "user-group-uid-gid-state",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-user-observe.json", "host-user-plan.json", "host-user-diff.json", "host-user-apply.json", "host-user-verify.json", "host-user.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"passwd database", "group database", "home directory"},
			SecretInputs:      []string{"user management command output digests only in evidence"},
			Description:       "Create, update, or delete one Linux user and optional group with UID/GID before/after evidence.",
		},
		{
			Adapter:           "host.cron.manage",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "target cron path ownership or delegated sudo",
			Idempotence:       "cron-file-content-digest",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-cron-observe.json", "host-cron-plan.json", "host-cron-diff.json", "host-cron-apply.json", "host-cron-verify.json", "host-cron.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"cron.d file", "cron schedule", "cron command digest"},
			SecretInputs:      []string{"cron command digest only in evidence"},
			Description:       "Create, update, or delete one cron.d entry through local or SSH transport with exact digest diff evidence.",
		},
		{
			Adapter:           "host.systemd.unit",
			Status:            "implemented",
			Classification:    "guarded",
			TargetTypes:       []string{"host", "local"},
			Transports:        []string{"local", "ssh"},
			Mutating:          true,
			RequiredPrivilege: "root or delegated sudo for systemd unit files and manager",
			Idempotence:       "unit-file-content-digest-runtime-state",
			CheckMode:         "deterministic-plan",
			DiffQuality:       "exact",
			SupportedPhases:   []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts: []string{"host-systemd-observe.json", "host-systemd-plan.json", "host-systemd-diff.json", "host-systemd-apply.json", "host-systemd-verify.json", "host-systemd-journal.json", "journal-evidence.json", "host-systemd.json", "decision.json"},
			RequiredPolicy:    []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:           []string{"systemd unit file", "systemd daemon-reload", "unit runtime state", "unit enablement", "journal digest"},
			SecretInputs:      []string{"unit content and journal output digests only in evidence"},
			Description:       "Render, reload, verify, and delete one systemd unit through local or SSH transport with exact unit and journal evidence.",
		},
		{
			Adapter:             "mysql.replication.verify",
			Status:              "implemented",
			Classification:      "guarded",
			TargetTypes:         []string{"host", "database"},
			Transports:          []string{"local", "ssh", "nats-agent"},
			Mutating:            false,
			RequiredPrivilege:   "MySQL replication read access and SSH access to declared MySQL nodes",
			Idempotence:         "read-only-verification",
			CheckMode:           "bounded",
			DiffQuality:         "unsupported",
			SupportedPhases:     []string{"observe", "plan", "apply", "verify", "export"},
			EvidenceArtifacts:   []string{"mysql-replication-observe.json", "mysql-replication-plan.json", "mysql-replication-execute.json", "mysql-replication-verify.json", "mysql-replication.json", "decision.json"},
			RequiredPolicy:      []string{"target graph selection", "fresh facts", "target lock", "allow policy decision"},
			Touches:             []string{"MySQL replication status", "optional probe table when configured"},
			SecretInputs:        []string{"database credentials and probe payloads are emitted as digests only"},
			NetworkDestinations: []string{"declared MySQL node addresses"},
			Description:         "Verify MySQL replication topology and synchronized state with typed receipts.",
		},
		{
			Adapter:             "k8s.manifest.apply",
			Status:              "implemented",
			Classification:      "guarded",
			TargetTypes:         []string{"kubernetes", "local", "host"},
			Transports:          []string{"local", "ssh"},
			Mutating:            true,
			RequiredPrivilege:   "Kubernetes RBAC to server-side diff, apply, get, and delete declared objects",
			Idempotence:         "server-side-field-manager-object-state",
			CheckMode:           "server-side-diff",
			DiffQuality:         "server-side",
			SupportedPhases:     []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts:   []string{"k8s-manifest-observe.json", "k8s-manifest-plan.json", "k8s-manifest-diff.json", "k8s-manifest-apply.json", "k8s-manifest-verify.json", "k8s-manifest.json", "decision.json"},
			RequiredPolicy:      []string{"target graph selection", "Kubernetes RBAC scope", "allow policy decision"},
			Touches:             []string{"Kubernetes API objects declared by manifest", "managedFields ownership for configured field manager"},
			SecretInputs:        []string{"manifest content, kubectl output, and live object bodies are emitted as digests only"},
			NetworkDestinations: []string{"Kubernetes API through selected kubectl target"},
			Description:         "Server-side diff, apply, ownership verify, no-op repeat, and cleanup for Kubernetes manifest objects through local or SSH kubectl execution.",
		},
		{
			Adapter:             "k8s.manifest.delete",
			Status:              "implemented",
			Classification:      "guarded",
			TargetTypes:         []string{"kubernetes", "local", "host"},
			Transports:          []string{"local", "ssh"},
			Mutating:            true,
			RequiredPrivilege:   "Kubernetes RBAC to get and delete declared objects",
			Idempotence:         "ownership-gated-listed-object-state",
			CheckMode:           "ownership-gated-plan",
			DiffQuality:         "ownership-gated-listed-only",
			SupportedPhases:     []string{"observe", "plan", "diff", "apply", "verify", "delete", "export"},
			EvidenceArtifacts:   []string{"k8s-manifest-delete-observe.json", "k8s-manifest-delete-plan.json", "k8s-manifest-delete-diff.json", "k8s-manifest-delete-apply.json", "k8s-manifest-delete-verify.json", "k8s-manifest-delete.json", "decision.json"},
			RequiredPolicy:      []string{"target graph selection", "Kubernetes RBAC scope", "allow policy decision"},
			Touches:             []string{"Kubernetes API objects listed by manifest"},
			SecretInputs:        []string{"manifest content, kubectl output, and live object bodies are emitted as digests only"},
			NetworkDestinations: []string{"Kubernetes API through selected kubectl target"},
			Description:         "Delete only manifest-listed Kubernetes objects after field-manager ownership checks and prove unrelated objects survive.",
		},
		{
			Adapter:             "k8s.resource.wait",
			Status:              "implemented",
			Classification:      "guarded",
			TargetTypes:         []string{"kubernetes", "local", "host"},
			Transports:          []string{"local", "ssh"},
			Mutating:            false,
			RequiredPrivilege:   "Kubernetes RBAC to get watched resources and list related events",
			Idempotence:         "readiness-state",
			CheckMode:           "readiness-plan",
			DiffQuality:         "readiness-state-with-events",
			SupportedPhases:     []string{"observe", "plan", "apply", "verify", "delete", "export"},
			EvidenceArtifacts:   []string{"k8s-resource-wait-observe.json", "k8s-resource-wait-plan.json", "k8s-resource-wait-apply.json", "k8s-resource-wait-events.json", "k8s-resource-wait-verify.json", "k8s-resource-wait.json", "decision.json"},
			RequiredPolicy:      []string{"target graph selection", "Kubernetes RBAC scope", "allow policy decision"},
			Touches:             []string{"Kubernetes API readiness state for one declared resource", "Kubernetes events for the declared resource"},
			SecretInputs:        []string{"kubectl output, live object bodies, and event messages are emitted as digests only"},
			NetworkDestinations: []string{"Kubernetes API through selected kubectl target"},
			Description:         "Wait for a declared Kubernetes resource readiness condition and record redacted event evidence for success or timeout.",
		},
		{
			Adapter:             "k8s.logs.capture",
			Status:              "implemented",
			Classification:      "guarded",
			TargetTypes:         []string{"kubernetes", "local", "host"},
			Transports:          []string{"local", "ssh"},
			Mutating:            false,
			RequiredPrivilege:   "Kubernetes RBAC to get declared resources and read pod logs",
			Idempotence:         "bounded-log-snapshot",
			CheckMode:           "log-capture-plan",
			DiffQuality:         "bounded-redacted-log-evidence",
			SupportedPhases:     []string{"observe", "plan", "apply", "verify", "delete", "export"},
			EvidenceArtifacts:   []string{"k8s-logs-capture-observe.json", "k8s-logs-capture-plan.json", "k8s-logs-capture-logs.json", "k8s-logs-capture-verify.json", "k8s-logs-capture.json", "decision.json"},
			RequiredPolicy:      []string{"target graph selection", "Kubernetes RBAC scope", "allow policy decision"},
			Touches:             []string{"Kubernetes pod logs for the declared resource or selector"},
			SecretInputs:        []string{"captured log lines are bounded and redacted; command output receipts store digests and byte counts"},
			NetworkDestinations: []string{"Kubernetes API through selected kubectl target"},
			Description:         "Capture bounded Kubernetes pod/container logs with redaction proof, line/byte limits, and digest-backed evidence.",
		},
		{
			Adapter:             "k8s.events.capture",
			Status:              "implemented",
			Classification:      "guarded",
			TargetTypes:         []string{"kubernetes", "local", "host"},
			Transports:          []string{"local", "ssh"},
			Mutating:            false,
			RequiredPrivilege:   "Kubernetes RBAC to get namespaces and list namespace events",
			Idempotence:         "bounded-event-snapshot",
			CheckMode:           "event-capture-plan",
			DiffQuality:         "filtered-redacted-event-evidence",
			SupportedPhases:     []string{"observe", "plan", "apply", "verify", "delete", "export"},
			EvidenceArtifacts:   []string{"k8s-events-capture-observe.json", "k8s-events-capture-plan.json", "k8s-events-capture-events.json", "k8s-events-capture-verify.json", "k8s-events-capture.json", "decision.json"},
			RequiredPolicy:      []string{"target graph selection", "Kubernetes RBAC scope", "allow policy decision"},
			Touches:             []string{"Kubernetes namespace events matching declared filters"},
			SecretInputs:        []string{"event messages are emitted as digests and scanned for redaction; command output receipts store digests and byte counts"},
			NetworkDestinations: []string{"Kubernetes API through selected kubectl target"},
			Description:         "Capture namespace Kubernetes events with type, reason, time, and involved-object filters plus digest-backed redaction proof.",
		},
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
	case "host.package.install":
		return []probeCommand{
			{Name: "package-manager", Command: "command -v apt-get >/dev/null 2>&1 || command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1 || command -v apk >/dev/null 2>&1", Required: true},
		}
	case "host.service.manage":
		return []probeCommand{
			{Name: "service-manager", Command: "command -v systemctl >/dev/null 2>&1 && systemctl --version >/dev/null 2>&1", Required: true},
		}
	case "host.user.manage":
		return []probeCommand{
			{Name: "user-manager", Command: "command -v getent >/dev/null 2>&1 && command -v useradd >/dev/null 2>&1 && command -v usermod >/dev/null 2>&1 && command -v userdel >/dev/null 2>&1 && command -v groupadd >/dev/null 2>&1 && command -v groupmod >/dev/null 2>&1 && command -v groupdel >/dev/null 2>&1", Required: true},
		}
	case "host.cron.manage":
		return []probeCommand{
			{Name: "cron-path", Command: "test -d /etc/cron.d || test -d /var/spool/cron || test -d /var/spool/cron/crontabs", Required: true},
		}
	case "host.systemd.unit":
		return []probeCommand{
			{Name: "systemd-manager", Command: "command -v systemctl >/dev/null 2>&1 && systemctl --version >/dev/null 2>&1", Required: true},
			{Name: "journal", Command: "command -v journalctl >/dev/null 2>&1", Required: true},
			{Name: "unit-path", Command: "test -d /etc/systemd/system", Required: true},
		}
	case "mysql.replication.verify":
		return []probeCommand{
			{Name: "bash", Command: "command -v bash >/dev/null 2>&1", Required: true},
			{Name: "ssh", Command: "command -v ssh >/dev/null 2>&1", Required: true},
			{Name: "mysql-client", Command: "command -v mysql >/dev/null 2>&1 || command -v mariadb >/dev/null 2>&1", Required: true},
		}
	case "k8s.manifest.apply", "k8s.manifest.delete":
		return []probeCommand{
			{Name: "kubectl", Command: "command -v kubectl >/dev/null 2>&1 || command -v k3s >/dev/null 2>&1", Required: true},
			{Name: "python3", Command: "command -v python3 >/dev/null 2>&1", Required: true},
		}
	case "k8s.resource.wait", "k8s.logs.capture", "k8s.events.capture":
		return []probeCommand{
			{Name: "kubectl", Command: "command -v kubectl >/dev/null 2>&1 || command -v k3s >/dev/null 2>&1", Required: true},
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
