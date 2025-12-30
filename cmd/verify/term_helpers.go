package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

func isTerminalWriter(w io.Writer) bool {
	switch v := w.(type) {
	case *os.File:
		return term.IsTerminal(int(v.Fd()))
	default:
		return false
	}
}

func isTerminalReader(r io.Reader) bool {
	switch v := r.(type) {
	case *os.File:
		return term.IsTerminal(int(v.Fd()))
	default:
		return false
	}
}
