//go:build !linux

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func runBuildSandboxDoctor(cmd *cobra.Command, parent *buildCLIOptions, contextDir string) error {
	_ = parent
	_ = contextDir
	if cmd == nil {
		return fmt.Errorf("sandbox doctor: command is nil")
	}
	return fmt.Errorf("sandbox doctor is only supported on Linux (nsjail)")
}
