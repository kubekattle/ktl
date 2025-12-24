// File: internal/ui/term.go
// Brief: Internal ui package implementation for 'terminal helpers'.

package ui

import (
	"io"

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

