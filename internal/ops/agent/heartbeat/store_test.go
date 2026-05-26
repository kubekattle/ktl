package heartbeat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileStoreSnapshotPreservesEvidenceOffset(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	store, err := NewFileStore(filepath.Join(t.TempDir(), "registry.json"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	record, err := NewCompactRecord(New(Options{
		AgentID:    "agent/mysql-01",
		Tenant:     "lab",
		TargetID:   "host/mysql-01",
		Labels:     map[string]string{"role": "mysql"},
		State:      StateReady,
		ObservedAt: now.Add(-time.Second),
	}), StreamOffset{
		Stream:   DefaultEventStream,
		Consumer: DefaultRegistryDurable,
		Subject:  "torque.v1.agent.heartbeat.lab.000.agent_mysql-01",
		Sequence: 42,
	}, now, 45*time.Second)
	if err != nil {
		t.Fatalf("NewCompactRecord: %v", err)
	}
	if err := store.Put(context.Background(), record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	snapshot, err := SnapshotFromStore(context.Background(), store, SnapshotRequest{
		Tenant:     "lab",
		Selector:   map[string]string{"role": "mysql"},
		Now:        now,
		StaleAfter: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("SnapshotFromStore: %v", err)
	}
	if snapshot.Summary.Total != 1 || snapshot.Summary.Ready != 1 {
		t.Fatalf("unexpected summary: %#v", snapshot.Summary)
	}
	if snapshot.Agents[0].EvidenceOffset == nil || snapshot.Agents[0].EvidenceOffset.Sequence != 42 {
		t.Fatalf("missing evidence offset: %#v", snapshot.Agents[0])
	}
}

func TestRegistryStoreKeyEncodesAgentID(t *testing.T) {
	key := RegistryStoreKey("/torque/", "lab.prod", "agent/mysql.01")
	if strings.Contains(key, "agent/mysql.01") {
		t.Fatalf("key leaked raw agent id: %s", key)
	}
	if !strings.HasPrefix(key, "/torque/agent-registry/v1/tenants/lab_prod/agents/") {
		t.Fatalf("unexpected key prefix: %s", key)
	}
}
