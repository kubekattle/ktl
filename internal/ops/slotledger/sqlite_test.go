package slotledger

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteStoreReserveBlocksReleasesAndReclaims(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	store, err := NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "slots.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	first, err := store.Reserve(ctx, ReserveRequest{
		Tenant:   "lab",
		TargetID: "host-141",
		Holder:   "run-a",
		RunID:    "run-a",
		NodeID:   "node-a",
		LeaseID:  "lease-a",
		MaxSlots: 1,
		TTL:      time.Minute,
		Now:      now,
		Token:    "token-a",
	})
	if err != nil {
		t.Fatalf("Reserve first: %v", err)
	}
	if first.Decision != "acquired" || first.Lease == nil || first.Lease.SlotIndex != 1 || first.Available != 0 {
		t.Fatalf("first reserve = %#v", first)
	}

	blocked, err := store.Reserve(ctx, ReserveRequest{
		Tenant:   "lab",
		TargetID: "host-141",
		Holder:   "run-b",
		RunID:    "run-b",
		NodeID:   "node-b",
		LeaseID:  "lease-b",
		MaxSlots: 1,
		TTL:      time.Minute,
		Now:      now.Add(time.Second),
		Token:    "token-b",
	})
	if err != nil {
		t.Fatalf("Reserve blocked: %v", err)
	}
	if blocked.Decision != "blocked" || len(blocked.Existing) != 1 || !strings.Contains(blocked.Reason, "no available ledger slots") {
		t.Fatalf("blocked reserve = %#v", blocked)
	}

	released, err := store.Release(ctx, ReleaseRequest{
		Tenant:   "lab",
		TargetID: "host-141",
		LeaseID:  "lease-a",
		Token:    "token-a",
		Now:      now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released.Status != StatusReleased || released.ReleasedAt == "" {
		t.Fatalf("released = %#v", released)
	}

	second, err := store.Reserve(ctx, ReserveRequest{
		Tenant:   "lab",
		TargetID: "host-141",
		Holder:   "run-b",
		RunID:    "run-b",
		NodeID:   "node-b",
		LeaseID:  "lease-b",
		MaxSlots: 1,
		TTL:      time.Minute,
		Now:      now.Add(3 * time.Second),
		Token:    "token-b",
	})
	if err != nil {
		t.Fatalf("Reserve second: %v", err)
	}
	if second.Decision != "acquired" || second.Lease == nil || second.Lease.SlotIndex != 1 {
		t.Fatalf("second reserve = %#v", second)
	}

	expiredStore, err := NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "expired.sqlite"))
	if err != nil {
		t.Fatalf("NewSQLiteStore expired: %v", err)
	}
	defer expiredStore.Close()
	expiring, err := expiredStore.Reserve(ctx, ReserveRequest{
		Tenant:   "lab",
		TargetID: "host-141",
		Holder:   "run-old",
		RunID:    "run-old",
		NodeID:   "node-old",
		LeaseID:  "lease-old",
		MaxSlots: 1,
		TTL:      time.Second,
		Now:      now,
		Token:    "token-old",
	})
	if err != nil || expiring.Decision != "acquired" {
		t.Fatalf("Reserve expiring = %#v err=%v", expiring, err)
	}
	reclaimed, err := expiredStore.Reserve(ctx, ReserveRequest{
		Tenant:   "lab",
		TargetID: "host-141",
		Holder:   "run-new",
		RunID:    "run-new",
		NodeID:   "node-new",
		LeaseID:  "lease-new",
		MaxSlots: 1,
		TTL:      time.Minute,
		Now:      now.Add(2 * time.Second),
		Token:    "token-new",
	})
	if err != nil {
		t.Fatalf("Reserve reclaimed: %v", err)
	}
	if reclaimed.Decision != "acquired" || len(reclaimed.Reclaimed) != 1 || reclaimed.Reclaimed[0].Status != StatusExpired {
		t.Fatalf("reclaimed reserve = %#v", reclaimed)
	}
}
