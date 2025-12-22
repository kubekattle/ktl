// File: cmd/ktl/confirm_test.go
// Brief: Tests for interactive confirmation prompts.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmActionYesAcceptsYes(t *testing.T) {
	in := strings.NewReader("yes\n")
	out := &bytes.Buffer{}
	if err := confirmAction(in, out, true, "Confirm?", confirmModeYes, ""); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestConfirmActionYesRejectsOtherInput(t *testing.T) {
	in := strings.NewReader("no\n")
	out := &bytes.Buffer{}
	if err := confirmAction(in, out, true, "Confirm?", confirmModeYes, ""); err == nil {
		t.Fatalf("expected error")
	}
}

func TestConfirmActionExactRequiresMatch(t *testing.T) {
	in := strings.NewReader("monitoring\n")
	out := &bytes.Buffer{}
	if err := confirmAction(in, out, true, "Type release:", confirmModeExact, "monitoring"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestConfirmActionNonInteractiveFails(t *testing.T) {
	in := strings.NewReader("yes\n")
	out := &bytes.Buffer{}
	if err := confirmAction(in, out, false, "Confirm?", confirmModeYes, ""); err == nil {
		t.Fatalf("expected error")
	}
}
