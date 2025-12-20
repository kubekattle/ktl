// File: cmd/ktl/build_test.go
// Brief: CLI command wiring and implementation for 'build'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"strings"
	"testing"
)

func TestRequireBuildContextArg(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		err := requireBuildContextArg(nil, nil)
		if err == nil {
			t.Fatal("expected error when context argument missing")
		}
		want := "'ktl build' requires 1 argument (CONTEXT). Try '.' for the current directory"
		if err.Error() != want {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		err := requireBuildContextArg(nil, []string{"one", "two"})
		if err == nil {
			t.Fatal("expected error for extra args")
		}
		if !strings.Contains(err.Error(), "exactly one context") {
			t.Fatalf("expected error to mention single context, got: %v", err)
		}
	})

	t.Run("single", func(t *testing.T) {
		if err := requireBuildContextArg(nil, []string{"."}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}
