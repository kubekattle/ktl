package main

import "github.com/spf13/cobra"

func newBuildSandboxCommand(parent *buildCLIOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sandbox",
		Short:        "Sandbox tooling for ktl build",
		SilenceUsage: true,
	}
	cmd.AddCommand(newBuildSandboxDoctorCommand(parent))
	return cmd
}
