package main

import (
	"context"
	"encoding/json"
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
