package capability

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestDiscoverReportsAvailableAndMissingCapabilities(t *testing.T) {
	report := Discover(Options{
		Adapters:     []string{"host.command.run", "mysql.replication.verify"},
		AgentVersion: "test",
		Hostname:     "agent-01",
		GOOS:         "linux",
		GOARCH:       "amd64",
		Now:          time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		LookupPath: func(name string) (string, error) {
			switch name {
			case "sh", "bash", "ssh":
				return "/usr/bin/" + name, nil
			default:
				return "", errors.New("missing")
			}
		},
	})

	if report.APIVersion != ReportAPIVersion || report.Kind != ReportKind || report.Digest == "" {
		t.Fatalf("unexpected report identity: %#v", report)
	}
	if report.Summary.Total != 2 || report.Summary.Available != 1 || report.Summary.Unavailable != 1 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if got := AvailableAdapters(report); len(got) != 1 || got[0] != "host.command.run" {
		t.Fatalf("available adapters = %#v", got)
	}
	mysql := findCapability(t, report, "mysql.replication.verify")
	if mysql.Status != StatusUnavailable || len(mysql.MissingDependencies) != 1 || mysql.MissingDependencies[0] != "mysql-client" {
		t.Fatalf("mysql capability = %#v", mysql)
	}
}

func TestDiscoverSystemdUnitRequiresUnitPath(t *testing.T) {
	report := Discover(Options{
		Adapters: []string{"host.systemd.unit"},
		GOOS:     "linux",
		GOARCH:   "amd64",
		LookupPath: func(name string) (string, error) {
			switch name {
			case "systemctl", "journalctl":
				return "/usr/bin/" + name, nil
			default:
				return "", errors.New("missing")
			}
		},
		Stat: func(string) (os.FileInfo, error) {
			return nil, errors.New("missing")
		},
	})
	systemd := findCapability(t, report, "host.systemd.unit")
	if systemd.Status != StatusUnavailable || len(systemd.MissingDependencies) != 1 || systemd.MissingDependencies[0] != "/etc/systemd/system" {
		t.Fatalf("systemd capability = %#v", systemd)
	}
}

func TestDigestNamesIsStable(t *testing.T) {
	a := DigestNames([]string{"mysql.replication.verify", "host.command.run", "host.command.run"})
	b := DigestNames([]string{"host.command.run", "mysql.replication.verify"})
	if a == "" || a != b {
		t.Fatalf("digest mismatch: %q %q", a, b)
	}
}

func findCapability(t *testing.T, report Report, adapter string) Capability {
	t.Helper()
	for _, capability := range report.Capabilities {
		if capability.Adapter == adapter {
			return capability
		}
	}
	t.Fatalf("missing capability %s in %#v", adapter, report.Capabilities)
	return Capability{}
}
