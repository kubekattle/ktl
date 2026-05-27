package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agentcapability "github.com/ingresslabs/torque/internal/ops/agent/capability"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
)

func TestParseNATSWorkerConfig(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_URL":               "nats://127.0.0.1:4222",
		"TORQUE_NATS_WORKER_SUBJECT":    "torque.lab.assign.mysql",
		"TORQUE_NATS_WORKER_QUEUE":      "mysql-workers",
		"TORQUE_NATS_CREDS":             "/tmp/nats.creds",
		"TORQUE_NATS_WORKER_TIMEOUT":    "9s",
		"TORQUE_NATS_DELIVERY":          "jetstream",
		"TORQUE_NATS_DURABLE":           "mysql-targets",
		"TORQUE_NATS_ASSIGNMENT_STREAM": "TORQUE_ASSIGNMENTS_TEST",
		"TORQUE_NATS_RECEIPT_STREAM":    "TORQUE_RECEIPTS_TEST",
		"TORQUE_AGENT_ID":               "agent-mysql-01",
		"TORQUE_AGENT_TENANT":           "lab",
		"TORQUE_AGENT_TARGET_ID":        "host/mysql-01",
		"TORQUE_AGENT_HOSTNAME":         "mysql-01",
	}
	config, err := parseNATSWorkerConfig([]string{"--timeout", "3s"}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseNATSWorkerConfig: %v", err)
	}
	if config.Server != "nats://127.0.0.1:4222" || config.Subject != "torque.lab.assign.mysql" || config.Queue != "mysql-workers" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if config.Creds != "/tmp/nats.creds" || config.Timeout != 3*time.Second {
		t.Fatalf("unexpected creds/timeout: %#v", config)
	}
	if config.Delivery != natstransport.DeliveryJetStream || config.Durable != "mysql-targets" || config.AssignmentStream != "TORQUE_ASSIGNMENTS_TEST" || config.ReceiptStream != "TORQUE_RECEIPTS_TEST" {
		t.Fatalf("durable delivery not parsed: %#v", config)
	}
	if config.AgentID != "agent-mysql-01" || config.Tenant != "lab" || config.TargetID != "host/mysql-01" || config.Hostname != "mysql-01" {
		t.Fatalf("identity not parsed: %#v", config)
	}
	if config.DisableCapabilityDiscovery {
		t.Fatalf("DisableCapabilityDiscovery = true, want default discovery enabled")
	}
}

func TestParseNATSWorkerConfigTimeoutFromEnv(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_SUBJECT": "torque.lab.assign.mysql",
		"TORQUE_NATS_TIMEOUT": "7s",
	}
	config, err := parseNATSWorkerConfig(nil, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseNATSWorkerConfig: %v", err)
	}
	if config.Timeout != 7*time.Second {
		t.Fatalf("Timeout = %s, want 7s", config.Timeout)
	}
}

func TestParseNATSWorkerConfigRequiresSubject(t *testing.T) {
	_, err := parseNATSWorkerConfig(nil, func(key string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "--subject is required") {
		t.Fatalf("expected subject error, got %v", err)
	}
}

func TestParseNATSWorkerConfigIdentityFlags(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_SUBJECT": "torque.lab.assign.mysql",
	}
	config, err := parseNATSWorkerConfig([]string{
		"--agent-id", "agent-flag",
		"--tenant", "lab",
		"--target-id", "host/flag",
		"--hostname", "host-flag",
	}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseNATSWorkerConfig: %v", err)
	}
	if config.AgentID != "agent-flag" || config.Tenant != "lab" || config.TargetID != "host/flag" || config.Hostname != "host-flag" {
		t.Fatalf("identity flags not parsed: %#v", config)
	}
}

func TestParseNATSWorkerConfigCapabilities(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_SUBJECT":                "torque.lab.assign.mysql",
		"TORQUE_AGENT_CAPABILITIES":          "mysql.replication.verify",
		"TORQUE_AGENT_DISCOVER_CAPABILITIES": "false",
	}
	config, err := parseNATSWorkerConfig([]string{"--capability", "host.command.run"}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseNATSWorkerConfig: %v", err)
	}
	if !config.DisableCapabilityDiscovery {
		t.Fatalf("DisableCapabilityDiscovery = false, want true")
	}
	if strings.Join(config.Capabilities, ",") != "mysql.replication.verify,host.command.run" {
		t.Fatalf("Capabilities = %#v", config.Capabilities)
	}
}

func TestParseNATSHeartbeatConfig(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_URL":                    "nats://127.0.0.1:4222",
		"TORQUE_AGENT_ID":                    "host-141",
		"TORQUE_AGENT_TENANT":                "lab",
		"TORQUE_AGENT_LABELS":                "role=mysql,site=lab",
		"TORQUE_AGENT_CAPABILITIES":          "host.file.ensure,mysql.replication.verify",
		"TORQUE_AGENT_DISCOVER_CAPABILITIES": "false",
		"TORQUE_AGENT_HEARTBEAT_INTERVAL":    "1s",
	}
	config, err := parseNATSHeartbeatConfig([]string{"--once", "--label", "zone=a", "--capability", "host.systemd.unit"}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseNATSHeartbeatConfig: %v", err)
	}
	if config.NATS.Server != "nats://127.0.0.1:4222" || config.Options.AgentID != "host-141" || config.Options.Tenant != "lab" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if !config.Once || config.Interval != time.Second {
		t.Fatalf("unexpected once/interval: %#v", config)
	}
	if config.Options.Labels["role"] != "mysql" || config.Options.Labels["zone"] != "a" {
		t.Fatalf("labels not parsed: %#v", config.Options.Labels)
	}
	if len(config.Options.Capabilities) != 3 {
		t.Fatalf("capabilities not parsed: %#v", config.Options.Capabilities)
	}
	if config.Options.CapabilityDigest == "" {
		t.Fatalf("capability digest was not set")
	}
}

func TestParseNATSHeartbeatConfigDiscoversCapabilitiesByDefault(t *testing.T) {
	env := map[string]string{
		"TORQUE_AGENT_ID": "host-141",
	}
	config, err := parseNATSHeartbeatConfig([]string{"--once"}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseNATSHeartbeatConfig: %v", err)
	}
	if len(config.Options.Capabilities) == 0 {
		t.Fatalf("expected discovered capabilities: %#v", config.Options.Capabilities)
	}
	if config.Options.CapabilityDigest == "" {
		t.Fatalf("capability digest was not set")
	}
}

func TestParseCapabilityReportConfig(t *testing.T) {
	env := map[string]string{
		"TORQUE_AGENT_HOSTNAME": "agent-01",
		"TORQUE_AGENT_VERSION":  "dev",
	}
	config, err := parseCapabilityReportConfig([]string{"--adapter", "host.command.run", "--format", "json"}, func(key string) string {
		return env[key]
	})
	if err != nil {
		t.Fatalf("parseCapabilityReportConfig: %v", err)
	}
	if config.Options.Hostname != "agent-01" || config.Options.AgentVersion != "dev" || config.Format != "json" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if len(config.Options.Adapters) != 1 || config.Options.Adapters[0] != "host.command.run" {
		t.Fatalf("adapters not parsed: %#v", config.Options.Adapters)
	}
}

func TestRunCapabilityReportWritesJSON(t *testing.T) {
	var out bytes.Buffer
	err := runCapabilityReport(t.Context(), capabilityReportConfig{
		Options: agentcapability.Options{
			Adapters:     []string{"host.command.run"},
			AgentVersion: "dev",
			Hostname:     "agent-01",
			Now:          time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		},
		Format: "json",
	}, &out)
	if err != nil {
		t.Fatalf("runCapabilityReport: %v", err)
	}
	var report agentcapability.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out.String())
	}
	if report.Kind != agentcapability.ReportKind || report.Digest == "" || len(report.Capabilities) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestParseNATSHeartbeatConfigRejectsBadState(t *testing.T) {
	env := map[string]string{
		"TORQUE_AGENT_ID": "host-141",
	}
	_, err := parseNATSHeartbeatConfig([]string{"--state", "broken"}, func(key string) string {
		return env[key]
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported heartbeat state") {
		t.Fatalf("expected state error, got %v", err)
	}
}
