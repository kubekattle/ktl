package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func openTTY(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/tty available: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestApprovalMode_Table(t *testing.T) {
	type tc struct {
		name           string
		approved       bool
		nonInteractive bool
		useTTY         bool
		wantErr        bool
		wantTTY        bool
	}
	cases := []tc{
		{name: "approved_noninteractive_notty", approved: true, nonInteractive: true, useTTY: false, wantErr: false, wantTTY: false},
		{name: "noninteractive_requires_yes", approved: false, nonInteractive: true, useTTY: false, wantErr: true},
		{name: "notty_requires_prompt", approved: false, nonInteractive: false, useTTY: false, wantErr: false, wantTTY: false},
		{name: "tty_interactive", approved: false, nonInteractive: false, useTTY: true, wantErr: false, wantTTY: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			if c.useTTY {
				tty := openTTY(t)
				cmd.SetIn(tty)
				cmd.SetErr(tty)
			} else {
				cmd.SetIn(bytes.NewBufferString(""))
				cmd.SetErr(&bytes.Buffer{})
			}

			dec, err := approvalMode(cmd, c.approved, c.nonInteractive)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dec.Approved != c.approved {
				t.Fatalf("approved mismatch: got %v want %v", dec.Approved, c.approved)
			}
			if dec.NonInteractive != c.nonInteractive {
				t.Fatalf("nonInteractive mismatch: got %v want %v", dec.NonInteractive, c.nonInteractive)
			}
			if dec.InteractiveTTY != c.wantTTY {
				t.Fatalf("InteractiveTTY mismatch: got %v want %v", dec.InteractiveTTY, c.wantTTY)
			}
		})
	}
}
