package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseNATSWorkerConfig(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_URL":            "nats://127.0.0.1:4222",
		"TORQUE_NATS_WORKER_SUBJECT": "torque.lab.assign.mysql",
		"TORQUE_NATS_WORKER_QUEUE":   "mysql-workers",
		"TORQUE_NATS_CREDS":          "/tmp/nats.creds",
		"TORQUE_NATS_WORKER_TIMEOUT": "9s",
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

func TestParseNATSHeartbeatConfig(t *testing.T) {
	env := map[string]string{
		"TORQUE_NATS_URL":                 "nats://127.0.0.1:4222",
		"TORQUE_AGENT_ID":                 "host-141",
		"TORQUE_AGENT_TENANT":             "lab",
		"TORQUE_AGENT_LABELS":             "role=mysql,site=lab",
		"TORQUE_AGENT_CAPABILITIES":       "host.file.ensure,mysql.replication.verify",
		"TORQUE_AGENT_HEARTBEAT_INTERVAL": "1s",
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
