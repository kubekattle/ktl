package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

type approvalDecision struct {
	Approved       bool
	InteractiveTTY bool
	NonInteractive bool
}

func approvalMode(cmd *cobra.Command, approved bool, nonInteractive bool) (approvalDecision, error) {
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
