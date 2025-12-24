// File: internal/workflows/buildsvc/output_mode_test.go
// Brief: Internal buildsvc package implementation for 'output mode tests'.

package buildsvc

import "testing"

func TestResolveBuildOutputMode(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		terminal bool
		want     buildOutputMode
	}{
		{name: "auto terminal", raw: "auto", terminal: true, want: buildOutputTTY},
		{name: "auto nonterminal", raw: "auto", terminal: false, want: buildOutputLogs},
		{name: "empty terminal", raw: "", terminal: true, want: buildOutputTTY},
		{name: "tty terminal", raw: "tty", terminal: true, want: buildOutputTTY},
		{name: "tty nonterminal falls back", raw: "tty", terminal: false, want: buildOutputLogs},
		{name: "logs terminal", raw: "logs", terminal: true, want: buildOutputLogs},
		{name: "unknown terminal", raw: "wat", terminal: true, want: buildOutputTTY},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBuildOutputMode(tc.raw, tc.terminal); got != tc.want {
				t.Fatalf("resolveBuildOutputMode(%q, %v)=%q want %q", tc.raw, tc.terminal, got, tc.want)
			}
		})
	}
}

