// File: cmd/ktl/help_template_test.go
// Brief: CLI command wiring and implementation for 'help template'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestFormatFlagUsagesRestoresNoOptDefVal(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	t.Helper()
	var value string
	fs.StringVarP(&value, "context", "C", ".", "context dir")
	flag := fs.Lookup("context")
	if flag.NoOptDefVal != "" {
		t.Fatalf("expected empty NoOptDefVal before format, got %q", flag.NoOptDefVal)
	}

	_ = formatFlagUsages(fs)

	if flag.NoOptDefVal != "" {
		t.Fatalf("expected NoOptDefVal to be restored, got %q", flag.NoOptDefVal)
	}
}
