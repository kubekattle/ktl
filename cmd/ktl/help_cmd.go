// File: cmd/ktl/help_cmd.go
// Brief: Custom Cobra help command with an optional HTML UI.

package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/helpui"
	"github.com/spf13/cobra"
)

func setHelpCommand(root *cobra.Command) {
	if root == nil {
		return
	}
	root.SetHelpCommand(newHelpCommand(root))
}

func newHelpCommand(root *cobra.Command) *cobra.Command {
	var uiAddr string
	cmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Show help for a command",
		Long:  "Show help for any ktl command, or launch an interactive help UI in your browser.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(uiAddr) != "" {
				model := helpui.BuildModel(root)
				srv, err := helpui.New(uiAddr, model)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "ktl help UI listening on http://localhost%s\n", srv.Addr())
				return srv.Run(cmd.Context())
			}

			target := root
			if len(args) > 0 {
				found, _, err := root.Find(args)
				if err != nil || found == nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Unknown help topic %q\n\n", strings.Join(args, " "))
					return root.Help()
				}
				target = found
			}
			target.SetOut(cmd.OutOrStdout())
			target.SetErr(cmd.ErrOrStderr())
			return target.Help()
		},
	}
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve the interactive help UI at this address (e.g. :8080)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8080"
	}
	decorateCommandHelp(cmd, "Help Flags")
	return cmd
}
