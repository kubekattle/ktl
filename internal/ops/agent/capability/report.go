package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/ops/adapter"
)

const (
	ReportAPIVersion = "torque.dev/agent-capability-report/v1"
	ReportKind       = "AgentCapabilityReport"

	StatusAvailable   = "available"
	StatusUnavailable = "unavailable"
)

var defaultAdapters = []string{
	"host.command.run",
	"host.file.render",
	"host.package.install",
	"host.service.manage",
	"host.systemd.unit",
	"mysql.replication.verify",
	"postgres.role.ensure",
	"postgres.database.ensure",
	"postgres.grant.ensure",
	"postgres.schema.ensure",
	"postgres.extension.ensure",
	"postgres.replication.verify",
	"postgres.backup.run",
	"postgres.backup.verify",
	"postgres.restore.drill",
	"postgres.config.ensure",
	"postgres.maintenance.run",
}

type Options struct {
	Adapters     []string
	AgentVersion string
	Hostname     string
	GOOS         string
	GOARCH       string
	Now          time.Time
	LookupPath   func(string) (string, error)
	Stat         func(string) (os.FileInfo, error)
}

type Report struct {
	APIVersion   string       `json:"apiVersion"`
	Kind         string       `json:"kind"`
	GeneratedAt  string       `json:"generatedAt"`
	Hostname     string       `json:"hostname,omitempty"`
	Platform     Platform     `json:"platform"`
	AgentVersion string       `json:"agentVersion,omitempty"`
	Digest       string       `json:"digest"`
	Summary      Summary      `json:"summary"`
	Capabilities []Capability `json:"capabilities"`
}

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type Summary struct {
	Total       int `json:"total"`
	Available   int `json:"available"`
	Unavailable int `json:"unavailable"`
}

type Capability struct {
	Adapter             string   `json:"adapter"`
	AdapterKind         string   `json:"adapterKind"`
	Version             string   `json:"version"`
	Status              string   `json:"status"`
	Transports          []string `json:"transports,omitempty"`
	RequiredPrivilege   string   `json:"requiredPrivilege,omitempty"`
	MissingDependencies []string `json:"missingDependencies,omitempty"`
	Reason              string   `json:"reason,omitempty"`
}

type dependencyCheck struct {
	Name   string
	Kind   string
	AnyOf  []string
	Reason string
}

type digestPayload struct {
	Platform     Platform     `json:"platform"`
	AgentVersion string       `json:"agentVersion,omitempty"`
	Capabilities []Capability `json:"capabilities"`
}

func DefaultAdapters() []string {
	return append([]string(nil), defaultAdapters...)
}

func Discover(opts Options) Report {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	hostname := strings.TrimSpace(opts.Hostname)
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	goos := strings.TrimSpace(opts.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := strings.TrimSpace(opts.GOARCH)
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	lookup := opts.LookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	stat := opts.Stat
	if stat == nil {
		stat = os.Stat
	}
	adapters := normalizeAdapters(opts.Adapters)
	definitions := adapterDefinitions()
	report := Report{
		APIVersion:   ReportAPIVersion,
		Kind:         ReportKind,
		GeneratedAt:  now.UTC().Format(time.RFC3339Nano),
		Hostname:     hostname,
		Platform:     Platform{OS: goos, Arch: goarch},
		AgentVersion: strings.TrimSpace(opts.AgentVersion),
		Capabilities: make([]Capability, 0, len(adapters)),
	}
	for _, name := range adapters {
		capability := buildCapability(name, definitions[name], lookup, stat)
		report.Capabilities = append(report.Capabilities, capability)
		switch capability.Status {
		case StatusAvailable:
			report.Summary.Available++
		case StatusUnavailable:
			report.Summary.Unavailable++
		}
	}
	report.Summary.Total = len(report.Capabilities)
	report.Digest = Digest(report.Platform, report.AgentVersion, report.Capabilities)
	return report
}

func AvailableAdapters(report Report) []string {
	out := make([]string, 0, len(report.Capabilities))
	for _, capability := range report.Capabilities {
		if capability.Status == StatusAvailable {
			out = append(out, capability.Adapter)
		}
	}
	sort.Strings(out)
	return out
}

func Digest(platform Platform, agentVersion string, capabilities []Capability) string {
	payload := digestPayload{
		Platform:     platform,
		AgentVersion: strings.TrimSpace(agentVersion),
		Capabilities: cloneCapabilities(capabilities),
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func DigestNames(names []string) string {
	names = normalizeAdapters(names)
	raw, _ := json.Marshal(names)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func buildCapability(name string, definition adapter.Capability, lookup func(string) (string, error), stat func(string) (os.FileInfo, error)) Capability {
	capability := Capability{
		Adapter:           name,
		AdapterKind:       "builtin",
		Version:           "v1",
		Status:            StatusAvailable,
		Transports:        append([]string(nil), definition.Transports...),
		RequiredPrivilege: strings.TrimSpace(definition.RequiredPrivilege),
	}
	missing := missingDependencies(name, lookup, stat)
	if len(missing) > 0 {
		capability.Status = StatusUnavailable
		capability.MissingDependencies = missing
		capability.Reason = "missing required dependency: " + strings.Join(missing, ", ")
	}
	return capability
}

func missingDependencies(name string, lookup func(string) (string, error), stat func(string) (os.FileInfo, error)) []string {
	var checks []dependencyCheck
	switch name {
	case "host.command.run":
		checks = []dependencyCheck{{Name: "sh", Kind: "binary"}}
	case "host.file.render":
		checks = []dependencyCheck{{Name: "sh", Kind: "binary"}}
	case "host.package.install":
		checks = []dependencyCheck{{Name: "package-manager", Kind: "any-binary", AnyOf: []string{"apt-get", "dnf", "yum", "apk", "brew"}}}
	case "host.service.manage":
		checks = []dependencyCheck{{Name: "systemctl", Kind: "binary"}}
	case "host.systemd.unit":
		checks = []dependencyCheck{
			{Name: "systemctl", Kind: "binary"},
			{Name: "journalctl", Kind: "binary"},
			{Name: "/etc/systemd/system", Kind: "dir"},
		}
	case "mysql.replication.verify":
		checks = []dependencyCheck{
			{Name: "bash", Kind: "binary"},
			{Name: "ssh", Kind: "binary"},
			{Name: "mysql-client", Kind: "any-binary", AnyOf: []string{"mysql", "mariadb"}},
		}
	case "postgres.backup.run":
		checks = []dependencyCheck{
			{Name: "bash", Kind: "binary"},
			{Name: "psql", Kind: "binary"},
			{Name: "pg_dump", Kind: "binary"},
			{Name: "sha256sum", Kind: "binary"},
		}
	case "postgres.backup.verify", "postgres.restore.drill":
		checks = []dependencyCheck{
			{Name: "bash", Kind: "binary"},
			{Name: "psql", Kind: "binary"},
			{Name: "pg_restore", Kind: "binary"},
			{Name: "sha256sum", Kind: "binary"},
		}
	case "postgres.role.ensure",
		"postgres.database.ensure",
		"postgres.grant.ensure",
		"postgres.schema.ensure",
		"postgres.extension.ensure",
		"postgres.replication.verify",
		"postgres.config.ensure",
		"postgres.maintenance.run":
		checks = []dependencyCheck{
			{Name: "bash", Kind: "binary"},
			{Name: "psql", Kind: "binary"},
		}
	default:
		checks = []dependencyCheck{{Name: name, Kind: "unsupported", Reason: "unsupported built-in capability"}}
	}
	var missing []string
	for _, check := range checks {
		if dependencyAvailable(check, lookup, stat) {
			continue
		}
		if strings.TrimSpace(check.Reason) != "" {
			missing = append(missing, check.Name+" ("+strings.TrimSpace(check.Reason)+")")
		} else {
			missing = append(missing, check.Name)
		}
	}
	sort.Strings(missing)
	return missing
}

func dependencyAvailable(check dependencyCheck, lookup func(string) (string, error), stat func(string) (os.FileInfo, error)) bool {
	switch check.Kind {
	case "binary":
		_, err := lookup(check.Name)
		return err == nil
	case "any-binary":
		for _, candidate := range check.AnyOf {
			if _, err := lookup(candidate); err == nil {
				return true
			}
		}
		return false
	case "dir":
		info, err := stat(check.Name)
		return err == nil && info.IsDir()
	default:
		return false
	}
}

func normalizeAdapters(names []string) []string {
	if len(names) == 0 {
		names = defaultAdapters
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func adapterDefinitions() map[string]adapter.Capability {
	out := map[string]adapter.Capability{}
	for _, definition := range adapter.Definitions() {
		out[definition.Adapter] = definition
	}
	return out
}

func cloneCapabilities(in []Capability) []Capability {
	out := append([]Capability(nil), in...)
	for i := range out {
		out[i].Transports = append([]string(nil), out[i].Transports...)
		out[i].MissingDependencies = append([]string(nil), out[i].MissingDependencies...)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Adapter < out[j].Adapter
	})
	return out
}
