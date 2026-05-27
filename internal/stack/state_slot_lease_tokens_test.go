package stack

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStackStateStore_SlotLeaseTokensRoundTrip(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	chartDir := filepath.Join(root, "chart")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: slot-lease\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write chart: %v", err)
	}
	p := &Plan{
		StackRoot: root,
		StackName: "slot-lease",
		Profile:   "dev",
		Nodes: []*ResolvedRelease{{
			ID:        "host.command.run/write-marker",
			Name:      "write-marker",
			Kind:      NodeKindHostCommandRun,
			Chart:     chartDir,
			Cluster:   ClusterTarget{Name: "local"},
			Namespace: "default",
		}},
	}
	runID := "run-slot-lease"
	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer store.Close()
	run := &runState{RunID: runID, Plan: p, Command: "apply", Nodes: wrapRunNodes(p.Nodes), Concurrency: 1, FailMode: "fail-fast"}
	if err := store.CreateRun(ctx, run, p); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	now := time.Now().UTC()
	token := StackSlotLeaseToken{
		RunID:          runID,
		NodeID:         "host.command.run/write-marker",
		TargetID:       "host/one",
		LeaseID:        "lease-one",
		Tenant:         "lab",
		Token:          "raw-token",
		TokenDigest:    "sha256:digest",
		LedgerStore:    "file",
		LedgerStoreKey: "sqlite://lease-one",
		Status:         "held",
		AcquiredAt:     now.Format(time.RFC3339Nano),
		ExpiresAt:      now.Add(time.Minute).Format(time.RFC3339Nano),
		UpdatedAt:      now.Format(time.RFC3339Nano),
	}
	if err := store.UpsertSlotLeaseToken(ctx, token); err != nil {
		t.Fatalf("UpsertSlotLeaseToken: %v", err)
	}
	got, err := store.ListSlotLeaseTokens(ctx, runID, token.NodeID)
	if err != nil {
		t.Fatalf("ListSlotLeaseTokens: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("tokens=%d want 1: %#v", len(got), got)
	}
	if got[0].Token != "raw-token" || got[0].TokenDigest != "sha256:digest" || got[0].Status != "held" {
		t.Fatalf("token mismatch: %#v", got[0])
	}

	token.Status = "released"
	token.ReleasedAt = now.Add(2 * time.Second).Format(time.RFC3339Nano)
	token.UpdatedAt = token.ReleasedAt
	if err := store.UpsertSlotLeaseToken(ctx, token); err != nil {
		t.Fatalf("UpsertSlotLeaseToken released: %v", err)
	}
	got, err = store.ListSlotLeaseTokens(ctx, runID, token.NodeID)
	if err != nil {
		t.Fatalf("ListSlotLeaseTokens released: %v", err)
	}
	if len(got) != 1 || got[0].Token != "" || got[0].Status != "released" || got[0].ReleasedAt == "" {
		t.Fatalf("released token mismatch: %#v", got)
	}
}
