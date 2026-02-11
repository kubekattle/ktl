// File: internal/workflows/buildsvc/output_mode.go
// Brief: Internal buildsvc package implementation for 'output mode'.

package buildsvc

import (
	"strings"
)

// OutputMode controls how ktl build renders progress locally.
type OutputMode string

const (
	OutputModeTTY  OutputMode = "tty"
	OutputModeLogs OutputMode = "logs"
)

func ResolveOutputMode(raw string, terminal bool) OutputMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		if terminal {
			return OutputModeTTY
		}
		return OutputModeLogs
	case "tty":
		if terminal {
			return OutputModeTTY
		}
		return OutputModeLogs
	case "logs":
		return OutputModeLogs
	default:
		if terminal {
			return OutputModeTTY
		}
		return OutputModeLogs
	}
}
