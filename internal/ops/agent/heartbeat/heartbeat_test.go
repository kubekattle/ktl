package heartbeat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatSubjectAndParse(t *testing.T) {
	observedAt := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	heartbeat := New(Options{
		AgentID:          "host.141",
		Tenant:           "lab.prod",
		TargetID:         "host/mysql-01",
		Hostname:         "mysql-01",
		Version:          "dev",
		Labels:           map[string]string{"role": "mysql", "site": "lab"},
		Capabilities:     []string{"host.file.ensure", "mysql.replication.verify", "host.file.ensure"},
		CapabilityDigest: "sha256:test",
		State:            StateReady,
		ObservedAt:       observedAt,
	})
	raw, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatalf("marshal heartbeat: %v", err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse heartbeat: %v", err)
	}
	if parsed.AgentID != "host.141" || parsed.Tenant != "lab_prod" || parsed.ObservedAt != observedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected parsed heartbeat: %#v", parsed)
	}
	if len(parsed.Capabilities) != 2 || parsed.Capabilities[0] != "host.file.ensure" {
		t.Fatalf("capabilities were not normalized: %#v", parsed.Capabilities)
	}
	if parsed.CapabilityDigest != "sha256:test" {
		t.Fatalf("capability digest was not preserved: %#v", parsed)
	}
	subject := Subject(parsed.Tenant, 16, parsed.AgentID)
	if !strings.HasPrefix(subject, "torque.v1.agent.heartbeat.lab_prod.") || strings.Contains(subject, ".host.141") {
		t.Fatalf("subject was not sanitized: %s", subject)
	}
}

func TestRegistrySnapshotSelectorAndStaleHealth(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry()
	for _, heartbeat := range []Heartbeat{
		New(Options{
			AgentID:    "agent-a",
			Tenant:     "lab",
			Labels:     map[string]string{"role": "mysql"},
			State:      StateReady,
			ObservedAt: now.Add(-10 * time.Second),
		}),
		New(Options{
			AgentID:    "agent-b",
			Tenant:     "lab",
			Labels:     map[string]string{"role": "mysql"},
			State:      StateReady,
			ObservedAt: now.Add(-2 * time.Minute),
		}),
		New(Options{
			AgentID:    "agent-c",
			Tenant:     "lab",
			Labels:     map[string]string{"role": "web"},
			State:      StateDegraded,
			ObservedAt: now.Add(-5 * time.Second),
		}),
	} {
		if err := registry.Apply(heartbeat); err != nil {
			t.Fatalf("apply heartbeat: %v", err)
		}
	}
	snapshot := registry.Snapshot(SnapshotRequest{
		Tenant:     "lab",
		Selector:   map[string]string{"role": "mysql"},
		Now:        now,
		StaleAfter: 45 * time.Second,
	})
	if snapshot.Summary.Total != 2 || snapshot.Summary.Ready != 1 || snapshot.Summary.Stale != 1 {
		t.Fatalf("unexpected summary: %#v", snapshot.Summary)
	}
	if snapshot.Agents[0].AgentID != "agent-a" || snapshot.Agents[0].Health != "ready" {
		t.Fatalf("unexpected first agent: %#v", snapshot.Agents[0])
	}
	if snapshot.Agents[1].AgentID != "agent-b" || snapshot.Agents[1].Health != "stale" {
		t.Fatalf("unexpected second agent: %#v", snapshot.Agents[1])
	}
}

func TestParseRejectsUnsupportedState(t *testing.T) {
	raw := []byte(`{"apiVersion":"torque.dev/agent-heartbeat/v1","kind":"AgentHeartbeat","agentId":"a","tenant":"lab","state":"broken","observedAt":"2026-05-26T12:00:00Z"}`)
	_, err := Parse(raw)
	if err == nil || !strings.Contains(err.Error(), "unsupported heartbeat state") {
		t.Fatalf("expected state error, got %v", err)
	}
}
