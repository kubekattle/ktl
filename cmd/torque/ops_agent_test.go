package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
)

func TestOpsAgentStatusJSON(t *testing.T) {
	oldCollector := collectOpsAgentStatus
	defer func() { collectOpsAgentStatus = oldCollector }()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	collectOpsAgentStatus = func(ctx context.Context, opts heartbeat.CollectOptions) (heartbeat.Snapshot, error) {
		if opts.NATS.Server != "nats://127.0.0.1:4222" {
			t.Fatalf("NATS server = %q", opts.NATS.Server)
		}
		if opts.Tenant != "lab" || opts.Selector["role"] != "mysql" {
			t.Fatalf("unexpected opts: %#v", opts)
		}
		registry := heartbeat.NewRegistry()
		if err := registry.Apply(heartbeat.New(heartbeat.Options{
			AgentID:      "host-141",
			Tenant:       "lab",
			TargetID:     "host/mysql-01",
			Version:      "dev",
			Labels:       map[string]string{"role": "mysql"},
			Capabilities: []string{"host.file.ensure"},
			State:        heartbeat.StateReady,
			ObservedAt:   now.Add(-time.Second),
		})); err != nil {
			return heartbeat.Snapshot{}, err
		}
		return registry.Snapshot(heartbeat.SnapshotRequest{
			Tenant:     opts.Tenant,
			Selector:   opts.Selector,
			Now:        now,
			StaleAfter: opts.StaleAfter,
		}), nil
	}

	out, errOut, err := runRootForOpsInventory(t,
		"ops", "agent", "status",
		"--nats-url", "nats://127.0.0.1:4222",
		"--tenant", "lab",
		"--selector", "role=mysql",
		"--timeout", "1ms",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("execute agent status failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var snapshot heartbeat.Snapshot
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if snapshot.Summary.Total != 1 || snapshot.Summary.Ready != 1 || snapshot.Agents[0].AgentID != "host-141" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestOpsAgentStatusFromFileStore(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	storePath := filepath.Join(t.TempDir(), "registry.json")
	store, err := heartbeat.NewFileStore(storePath)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	record, err := heartbeat.NewCompactRecord(heartbeat.New(heartbeat.Options{
		AgentID:    "agent-mysql-01",
		Tenant:     "lab",
		TargetID:   "host/mysql-01",
		Labels:     map[string]string{"role": "mysql"},
		State:      heartbeat.StateReady,
		ObservedAt: now.Add(-time.Second),
	}), heartbeat.StreamOffset{
		Stream:   heartbeat.DefaultEventStream,
		Consumer: heartbeat.DefaultRegistryDurable,
		Sequence: 7,
	}, now, 45*time.Second)
	if err != nil {
		t.Fatalf("NewCompactRecord: %v", err)
	}
	if err := store.Put(context.Background(), record); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out, errOut, err := runRootForOpsInventory(t,
		"ops", "agent", "status",
		"--source", "store",
		"--store", "file",
		"--store-path", storePath,
		"--tenant", "lab",
		"--selector", "role=mysql",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("execute agent status failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var snapshot heartbeat.Snapshot
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if snapshot.Summary.Total != 1 || snapshot.Agents[0].EvidenceOffset == nil || snapshot.Agents[0].EvidenceOffset.Sequence != 7 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestOpsAgentRegistryCompactJSON(t *testing.T) {
	oldIngest := ingestOpsAgentRegistry
	defer func() { ingestOpsAgentRegistry = oldIngest }()
	ingestOpsAgentRegistry = func(ctx context.Context, opts heartbeat.IngestOptions) (heartbeat.IngestResult, error) {
		if opts.NATS.Stream != heartbeat.DefaultEventStream || opts.Durable != "test-registry" {
			t.Fatalf("unexpected ingest opts: %#v", opts)
		}
		return heartbeat.IngestResult{
			APIVersion:   heartbeat.SnapshotAPIVersion,
			Kind:         heartbeat.IngestResultKind,
			Tenant:       "lab",
			Stream:       opts.NATS.Stream,
			Consumer:     opts.Durable,
			Processed:    2,
			Stored:       2,
			LastSequence: 12,
			Status:       "succeeded",
			StartedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			FinishedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		}, nil
	}
	out, errOut, err := runRootForOpsInventory(t,
		"ops", "agent", "registry", "compact",
		"--nats-url", "nats://127.0.0.1:4222",
		"--tenant", "lab",
		"--durable", "test-registry",
		"--store", "file",
		"--store-path", filepath.Join(t.TempDir(), "registry.json"),
		"--max-messages", "2",
		"--format", "json",
	)
	if err != nil {
		t.Fatalf("execute compact failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var result heartbeat.IngestResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if result.Processed != 2 || result.Stored != 2 || result.LastSequence != 12 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
