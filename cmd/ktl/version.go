// File: cmd/ktl/version.go
// Brief: CLI command wiring and implementation for 'version'.

package main

import (
	"fmt"

	"github.com/kubekattle/ktl/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:           "version",
		Short:         "Print the ktl client version information",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Get()
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), info.Version)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Client Version: %s\n", info.Version)
			if info.GitCommit != "" && info.GitCommit != "unknown" {
				fmt.Fprintf(cmd.OutOrStdout(), "GitCommit: %s\n", info.GitCommit)
			}
			if info.GitTreeState != "" && info.GitTreeState != "unknown" {
				fmt.Fprintf(cmd.OutOrStdout(), "GitTreeState: %s\n", info.GitTreeState)
			}
			if info.BuildDate != "" && info.BuildDate != "unknown" {
				fmt.Fprintf(cmd.OutOrStdout(), "BuildDate: %s\n", info.BuildDate)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "GoVersion: %s\n", info.GoVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Platform: %s\n", info.Platform)
			return nil
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "Print just the version number")
	decorateCommandHelp(cmd, "Version Flags")
	return cmd
}
