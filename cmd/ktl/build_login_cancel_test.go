package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestReadPasswordFromStdin_CancelledContext(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{}
	cmd.SetIn(pr)
	cmd.SetContext(ctx)

	cancel()
	start := time.Now()
	_, err := readPasswordFromStdin(cmd)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Fatalf("expected prompt cancellation to be fast")
	}
}
