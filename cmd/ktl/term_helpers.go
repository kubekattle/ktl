// File: cmd/ktl/term_helpers.go
// Brief: CLI command wiring and implementation for 'term helpers'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

func isTerminalWriter(w io.Writer) bool {
	type fdProvider interface {
		Fd() uintptr
	}
	if v, ok := w.(fdProvider); ok {
		return term.IsTerminal(int(v.Fd()))
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}
