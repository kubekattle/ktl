package stack

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStackStateSQLite_LockAcquireRelease(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lock1, err := s.AcquireLock(ctx, "owner-1", 10*time.Minute, false, "run-1")
	if err != nil {
		t.Fatalf("acquire lock1: %v", err)
	}
	if lock1.Owner != "owner-1" || lock1.RunID != "run-1" {
		t.Fatalf("lock1=%+v", lock1)
	}

	_, err = s.AcquireLock(ctx, "owner-2", 10*time.Minute, false, "run-2")
	if err == nil {
		t.Fatalf("expected lock contention error")
	}
	if !strings.Contains(err.Error(), "locked by") {
		t.Fatalf("unexpected error: %v", err)
	}

	lock2, err := s.AcquireLock(ctx, "owner-2", 10*time.Minute, true, "run-2")
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if lock2.Owner != "owner-2" || lock2.RunID != "run-2" {
		t.Fatalf("lock2=%+v", lock2)
	}

	if err := s.ReleaseLock(ctx, "owner-1", "run-1"); err != nil {
		t.Fatalf("release old owner: %v", err)
	}
	// Current owner should be able to release.
	if err := s.ReleaseLock(ctx, "owner-2", "run-2"); err != nil {
		t.Fatalf("release: %v", err)
	}
	got, err := s.GetLock(ctx)
	if err != nil {
		t.Fatalf("get lock: %v", err)
	}
	if got != nil {
		t.Fatalf("expected lock to be released, got %+v", got)
	}
}

func TestStackStateSQLite_LockExpires(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.AcquireLock(ctx, "owner-1", 5*time.Millisecond, false, "run-1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	_, err = s.AcquireLock(ctx, "owner-2", 10*time.Minute, false, "run-2")
	if err != nil {
		t.Fatalf("acquire after expiry: %v", err)
	}
}
