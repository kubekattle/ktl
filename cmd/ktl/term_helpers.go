// File: cmd/ktl/term_helpers.go
// Brief: CLI command wiring and implementation for 'term helpers'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"io"

	"github.com/kubekattle/ktl/internal/ui"
)

func isTerminalReader(r io.Reader) bool {
	return ui.IsTerminalReader(r)
}

func isTerminalWriter(w io.Writer) bool {
	return ui.IsTerminalWriter(w)
}
