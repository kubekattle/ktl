package main

import "github.com/spf13/cobra"

func newHelpCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Show help for a command",
		Long:  "Show help for a command.",
		RunE: func(cmd *cobra.Command, args []string) error {
			target := root
			if len(args) > 0 {
				c, _, err := root.Find(args)
				if err != nil {
					return err
				}
				if c != nil {
					target = c
				}
			}
			return target.Help()
		},
	}
	return cmd
}
