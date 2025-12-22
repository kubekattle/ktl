// File: cmd/ktl/confirm_test.go
// Brief: Tests for interactive confirmation prompts.

package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestConfirmActionYesAcceptsYes(t *testing.T) {
	in := strings.NewReader("yes\n")
	out := &bytes.Buffer{}
	if err := confirmAction(context.Background(), in, out, approvalDecision{InteractiveTTY: true}, "Confirm?", confirmModeYes, ""); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestConfirmActionYesRejectsOtherInput(t *testing.T) {
	in := strings.NewReader("no\n")
	out := &bytes.Buffer{}
	if err := confirmAction(context.Background(), in, out, approvalDecision{InteractiveTTY: true}, "Confirm?", confirmModeYes, ""); err == nil {
		t.Fatalf("expected error")
	}
}

func TestConfirmActionYesAcceptsCaseInsensitiveYes(t *testing.T) {
	in := strings.NewReader("YES\n")
	out := &bytes.Buffer{}
	if err := confirmAction(context.Background(), in, out, approvalDecision{InteractiveTTY: true}, "Confirm?", confirmModeYes, ""); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestConfirmActionExactRequiresMatch(t *testing.T) {
	in := strings.NewReader("monitoring\n")
	out := &bytes.Buffer{}
	if err := confirmAction(context.Background(), in, out, approvalDecision{InteractiveTTY: true}, "Type release:", confirmModeExact, "monitoring"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestConfirmActionNonInteractiveFails(t *testing.T) {
	in := strings.NewReader("yes\n")
	out := &bytes.Buffer{}
	if err := confirmAction(context.Background(), in, out, approvalDecision{InteractiveTTY: false}, "Confirm?", confirmModeYes, ""); err == nil {
		t.Fatalf("expected error")
	}
}

func TestConfirmActionApprovedNeverPrompts(t *testing.T) {
	in := strings.NewReader("no\n")
	out := &bytes.Buffer{}
	if err := confirmAction(context.Background(), in, out, approvalDecision{Approved: true, InteractiveTTY: false, NonInteractive: true}, "Confirm?", confirmModeYes, ""); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestConfirmActionCanceledReturnsContextCanceled(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &bytes.Buffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- confirmAction(ctx, pr, out, approvalDecision{InteractiveTTY: true}, "Confirm?", confirmModeYes, "")
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	_ = pw.Close()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for confirmAction to return")
	}
}
