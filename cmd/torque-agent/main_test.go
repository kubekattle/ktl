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
