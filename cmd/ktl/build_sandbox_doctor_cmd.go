package main

import "github.com/spf13/cobra"

func newBuildSandboxDoctorCommand(parent *buildCLIOptions) *cobra.Command {
	var contextDir string
	cmd := &cobra.Command{
		Use:          "doctor",
		Short:        "Diagnose nsjail sandbox readiness for ktl build",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuildSandboxDoctor(cmd, parent, contextDir)
		},
	}
	cmd.Flags().StringVar(&contextDir, "context", "", "Build context directory to use when probing (default: cwd)")
	return cmd
}
