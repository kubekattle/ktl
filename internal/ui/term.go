// File: internal/ui/term.go
// Brief: Internal ui package implementation for 'terminal helpers'.

package ui

import (
	"io"
	"os"

	"golang.org/x/term"
)

func TerminalWidth(w io.Writer) (int, bool) {
	type fdProvider interface {
		Fd() uintptr
	}
	if v, ok := w.(fdProvider); ok {
		if cols, _, err := term.GetSize(int(v.Fd())); err == nil {
			return cols, true
		}
	}
	return 0, false
}

func IsTerminalReader(r io.Reader) bool {
	type fdProvider interface {
		Fd() uintptr
	}
	if v, ok := r.(fdProvider); ok {
		return term.IsTerminal(int(v.Fd()))
	}
	if f, ok := r.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func IsTerminalWriter(w io.Writer) bool {
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
