// File: cmd/ktl/validate.go
// Brief: Shared CLI validation helpers.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func validateVerboseLogLevel(cmd *cobra.Command, verbose bool, logLevel *string) error {
	if !verbose || logLevel == nil {
		return nil
	}
	if flag := cmd.Flags().Lookup("log-level"); flag != nil && flag.Changed {
		return fmt.Errorf("--verbose cannot be combined with --log-level")
	}
	if flag := cmd.InheritedFlags().Lookup("log-level"); flag != nil && flag.Changed {
		return fmt.Errorf("--verbose cannot be combined with --log-level")
	}
	*logLevel = "debug"
	return nil
}

func validateNonInteractive(cmd *cobra.Command, nonInteractive bool, autoApprove bool, allowWithoutApproval bool) error {
	if !nonInteractive {
		return nil
	}
	if allowWithoutApproval {
		return nil
	}
	if autoApprove {
		return nil
	}
	return fmt.Errorf("--non-interactive requires --auto-approve")
}
