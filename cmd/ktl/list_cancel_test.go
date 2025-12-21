package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunWithCancelReturnsContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runWithCancel(ctx, func() (string, error) {
		time.Sleep(50 * time.Millisecond)
		return "done", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
