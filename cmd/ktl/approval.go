package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type approvalDecision struct {
	Approved       bool
	InteractiveTTY bool
	NonInteractive bool
}

func approvedFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("KTL_YES"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func approvalMode(cmd *cobra.Command, approved bool, nonInteractive bool) (approvalDecision, error) {
	if !approved && approvedFromEnv() {
		approved = true
	}
	in := cmd.InOrStdin()
	out := cmd.ErrOrStderr()
	interactive := isTerminalReader(in) && isTerminalWriter(out)
	if nonInteractive && !approved {
		return approvalDecision{}, fmt.Errorf("--non-interactive requires --yes")
	}
	return approvalDecision{
		Approved:       approved,
		InteractiveTTY: interactive,
		NonInteractive: nonInteractive,
	}, nil
}
