// tag.go adds the lightweight 'ktl tag' command, copying image references within/between registries via the distribution API.
package main

import (
	"fmt"

	"github.com/example/ktl/pkg/registry"
	"github.com/spf13/cobra"
)

func newTagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag SOURCE_IMAGE TARGET_IMAGE",
		Short: "Copy/tag an image reference via the registry API",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			dst := args[1]
			fmt.Fprintf(cmd.ErrOrStderr(), "Tagging %s -> %s\n", src, dst)
			return registry.CopyReference(cmd.Context(), src, dst)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	decorateCommandHelp(cmd, "Tag Flags")
	return cmd
}
