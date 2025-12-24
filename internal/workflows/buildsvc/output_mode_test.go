// File: internal/workflows/buildsvc/output_mode_test.go
// Brief: Internal buildsvc package implementation for 'output mode tests'.

package buildsvc

import "testing"

func TestResolveBuildOutputMode(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		terminal bool
		want     OutputMode
	}{
		{name: "auto terminal", raw: "auto", terminal: true, want: OutputModeTTY},
		{name: "auto nonterminal", raw: "auto", terminal: false, want: OutputModeLogs},
		{name: "empty terminal", raw: "", terminal: true, want: OutputModeTTY},
		{name: "tty terminal", raw: "tty", terminal: true, want: OutputModeTTY},
		{name: "tty nonterminal falls back", raw: "tty", terminal: false, want: OutputModeLogs},
		{name: "logs terminal", raw: "logs", terminal: true, want: OutputModeLogs},
		{name: "unknown terminal", raw: "wat", terminal: true, want: OutputModeTTY},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveOutputMode(tc.raw, tc.terminal); got != tc.want {
				t.Fatalf("ResolveOutputMode(%q, %v)=%q want %q", tc.raw, tc.terminal, got, tc.want)
			}
		})
	}
}
