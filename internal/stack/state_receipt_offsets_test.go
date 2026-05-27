package stack

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
)

func TestStackStateStore_ReceiptOffsetsRoundTrip(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	chartDir := filepath.Join(root, "chart")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: receipt-offsets\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write chart: %v", err)
	}
	p := &Plan{
		StackRoot: root,
		StackName: "receipt-offsets",
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
	runID := "run-receipt-offsets"
	store, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	defer store.Close()
	run := &runState{RunID: runID, Plan: p, Command: "apply", Nodes: wrapRunNodes(p.Nodes), Concurrency: 1, FailMode: "fail-fast"}
	if err := store.CreateRun(ctx, run, p); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	receivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	checkpoint := StackReceiptOffsetCheckpoint{
		RunID:         runID,
		ReceiptRunID:  runID,
		NodeID:        "host.command.run/write-marker",
		TargetID:      "host/one",
		AssignmentID:  "sha256:assignment",
		AgentID:       "agent-one",
		WorkerSubject: "torque.assign.lab.host_one",
		Offset: &natstransport.StreamOffset{
			Stream:       "TORQUE_RECEIPTS_TEST",
			Consumer:     "torque-stack-receipts-run",
			Subject:      "torque.receipt.lab.run.host_one",
			Sequence:     42,
			NumDelivered: 1,
			ReceivedAt:   receivedAt,
		},
		LastSeenAt: receivedAt,
		Receipt: transport.OperationResult{
			Operation: "run",
			Status:    "succeeded",
			Metadata: map[string]string{
				"runId":              runID,
				"nodeId":             "host.command.run/write-marker",
				"assignmentId":       "sha256:assignment",
				"assignmentTargetId": "host/one",
				"agentId":            "agent-one",
			},
		},
	}
	if err := store.UpsertReceiptOffset(ctx, checkpoint); err != nil {
		t.Fatalf("UpsertReceiptOffset: %v", err)
	}
	got, err := store.ListReceiptOffsets(ctx, runID, "host.command.run/write-marker")
	if err != nil {
		t.Fatalf("ListReceiptOffsets: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("checkpoints=%d want 1: %#v", len(got), got)
	}
	if got[0].AssignmentID != checkpoint.AssignmentID || got[0].Offset == nil || got[0].Offset.Sequence != 42 {
		t.Fatalf("checkpoint mismatch: %#v", got[0])
	}
	if got[0].Receipt.Status != "succeeded" || got[0].Receipt.Metadata["agentId"] != "agent-one" {
		t.Fatalf("receipt mismatch: %#v", got[0].Receipt)
	}
	if got[0].ReceiptDigest == "" {
		t.Fatalf("missing receipt digest: %#v", got[0])
	}
}
