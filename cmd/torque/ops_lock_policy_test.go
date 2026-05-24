package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ingresslabs/torque/internal/ops/locks"
	opspolicy "github.com/ingresslabs/torque/internal/ops/policy"
)

func TestOpsLockAcquireStatusReleaseJSON(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "locks")
	out, errOut, err := runRootForOpsInventory(t, "ops", "lock", "acquire", "--scope", "target/host-01", "--holder", "test-operator", "--operation", "host.command.run", "--lock-dir", lockDir, "--format", "json")
	if err != nil {
		t.Fatalf("acquire failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var acquired locks.AcquireResult
	if err := json.Unmarshal([]byte(out), &acquired); err != nil {
		t.Fatalf("decode acquire: %v\n%s", err, out)
	}
	if acquired.Decision != "acquired" || acquired.Record == nil || acquired.Record.Token == "" {
		t.Fatalf("acquire result = %#v", acquired)
	}

	blockedOut, _, err := runRootForOpsInventory(t, "ops", "lock", "acquire", "--scope", "target/host-01", "--holder", "other", "--lock-dir", lockDir, "--format", "json")
	if err == nil {
		t.Fatal("second acquire error = nil, want blocked")
	}
	var blocked locks.AcquireResult
	if err := json.Unmarshal([]byte(blockedOut), &blocked); err != nil {
		t.Fatalf("decode blocked acquire: %v\n%s", err, blockedOut)
	}
	if blocked.Decision != "blocked" || blocked.Existing == nil || blocked.Existing.Holder != "test-operator" {
		t.Fatalf("blocked result = %#v", blocked)
	}

	statusOut, errOut, err := runRootForOpsInventory(t, "ops", "lock", "status", "--scope", "target/host-01", "--lock-dir", lockDir, "--format", "json")
	if err != nil {
		t.Fatalf("status failed: %v\nstderr=%s\nstdout=%s", err, errOut, statusOut)
	}
	var status opsLockStatusResult
	if err := json.Unmarshal([]byte(statusOut), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusOut)
	}
	if !status.Found || status.Record == nil || status.Record.Token != acquired.Record.Token {
		t.Fatalf("status = %#v", status)
	}

	releaseOut, errOut, err := runRootForOpsInventory(t, "ops", "lock", "release", "--scope", "target/host-01", "--token", acquired.Record.Token, "--lock-dir", lockDir, "--format", "json")
	if err != nil {
		t.Fatalf("release failed: %v\nstderr=%s\nstdout=%s", err, errOut, releaseOut)
	}
	var released locks.Record
	if err := json.Unmarshal([]byte(releaseOut), &released); err != nil {
		t.Fatalf("decode release: %v\n%s", err, releaseOut)
	}
	if released.Status != "released" {
		t.Fatalf("release = %#v", released)
	}
}

func TestOpsPolicyCheckJSON(t *testing.T) {
	blockOut, _, err := runRootForOpsInventory(t, "ops", "policy", "check", "--mode", "observe-only", "--operation", "host.command.run", "--mutating", "--format", "json")
	if err == nil {
		t.Fatal("observe-only mutating policy error = nil, want block")
	}
	var blocked opspolicy.Decision
	if err := json.Unmarshal([]byte(blockOut), &blocked); err != nil {
		t.Fatalf("decode blocked policy: %v\n%s", err, blockOut)
	}
	if blocked.Decision != "block" {
		t.Fatalf("blocked policy = %#v", blocked)
	}

	allowOut, errOut, err := runRootForOpsInventory(t, "ops", "policy", "check", "--mode", "guarded", "--operation", "host.command.run", "--mutating", "--approved", "--format", "json")
	if err != nil {
		t.Fatalf("approved guarded policy failed: %v\nstderr=%s\nstdout=%s", err, errOut, allowOut)
	}
	var allowed opspolicy.Decision
	if err := json.Unmarshal([]byte(allowOut), &allowed); err != nil {
		t.Fatalf("decode allowed policy: %v\n%s", err, allowOut)
	}
	if allowed.Decision != "allow" {
		t.Fatalf("allowed policy = %#v", allowed)
	}

	unsafeOut, _, err := runRootForOpsInventory(t, "ops", "policy", "check", "--mode", "automatic", "--operation", "host.command.run", "--mutating", "--unsafe", "--format", "json")
	if err == nil {
		t.Fatal("unsafe automatic policy error = nil, want block")
	}
	var unsafeDecision opspolicy.Decision
	if err := json.Unmarshal([]byte(unsafeOut), &unsafeDecision); err != nil {
		t.Fatalf("decode unsafe policy: %v\n%s", err, unsafeOut)
	}
	if unsafeDecision.Decision != "block" || !unsafeDecision.Unsafe {
		t.Fatalf("unsafe policy = %#v", unsafeDecision)
	}
}
