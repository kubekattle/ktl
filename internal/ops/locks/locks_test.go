package locks

import (
	"context"
	"testing"
	"time"
)

func TestFileStoreAcquireBlocksAndReleases(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store := FileStore{Dir: t.TempDir(), Now: func() time.Time { return now }}
	first, err := store.Acquire(context.Background(), AcquireRequest{
		Scope:     "target/host-01",
		TargetID:  "host-01",
		Holder:    "operator-a",
		Operation: "host.command.run",
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if first.Decision != "acquired" || first.Record == nil || first.Record.Token == "" {
		t.Fatalf("first acquisition = %#v", first)
	}
	second, err := store.Acquire(context.Background(), AcquireRequest{
		Scope:  "target/host-01",
		Holder: "operator-b",
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if second.Decision != "blocked" || second.Existing == nil || second.Existing.Holder != "operator-a" {
		t.Fatalf("second acquisition = %#v", second)
	}
	if _, err := store.Release("target/host-01", "wrong-token"); err == nil {
		t.Fatal("Release() error = nil, want token mismatch")
	}
	released, err := store.Release("target/host-01", first.Record.Token)
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if released.Status != "released" || released.ReleasedAt == "" {
		t.Fatalf("released record = %#v", released)
	}
	third, err := store.Acquire(context.Background(), AcquireRequest{
		Scope:  "target/host-01",
		Holder: "operator-b",
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("third Acquire() error = %v", err)
	}
	if third.Decision != "acquired" || third.Record.Holder != "operator-b" {
		t.Fatalf("third acquisition = %#v", third)
	}
}

func TestFileStoreReplacesStaleLock(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	store := FileStore{Dir: t.TempDir(), Now: func() time.Time { return now }}
	first, err := store.Acquire(context.Background(), AcquireRequest{
		Scope:  "target/host-02",
		Holder: "operator-a",
		TTL:    time.Second,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	now = now.Add(2 * time.Second)
	inspected, found, err := store.Inspect("target/host-02")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !found || inspected.Status != "stale" {
		t.Fatalf("inspect stale lock = found %v record %#v", found, inspected)
	}
	second, err := store.Acquire(context.Background(), AcquireRequest{
		Scope:  "target/host-02",
		Holder: "operator-b",
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire(stale) error = %v", err)
	}
	if second.Decision != "acquired" || second.Record.Token == first.Record.Token || second.Record.Holder != "operator-b" {
		t.Fatalf("stale replacement = %#v", second)
	}
}
