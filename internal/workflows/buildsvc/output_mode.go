// File: internal/workflows/buildsvc/output_mode.go
// Brief: Internal buildsvc package implementation for 'output mode'.

package buildsvc

import (
	"io"
	"strings"

	"golang.org/x/term"
)

type buildOutputMode string

const (
	buildOutputTTY  buildOutputMode = "tty"
	buildOutputLogs buildOutputMode = "logs"
)

func resolveBuildOutputMode(raw string, terminal bool) buildOutputMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		if terminal {
			return buildOutputTTY
		}
		return buildOutputLogs
	case "tty":
		if terminal {
			return buildOutputTTY
		}
		return buildOutputLogs
	case "logs":
		return buildOutputLogs
	default:
		if terminal {
			return buildOutputTTY
		}
		return buildOutputLogs
	}
}

func terminalWidth(w io.Writer) (int, bool) {
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
