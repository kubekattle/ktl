// push.go implements 'ktl push', copying container artifacts between registries with progress feedback and retries.
package main

import (
	"fmt"

	"github.com/example/ktl/pkg/registry"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
)

func newPushCommand() *cobra.Command {
	var (
		sign    bool
		allTags bool
	)

	cmd := &cobra.Command{
		Use:   "push IMAGE[:TAG]",
		Short: "Push an image built by ktl to its registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			opts := registry.PushOptions{Sign: sign, Output: cmd.ErrOrStderr()}
			if allTags {
				repo, err := repositoryFrom(target)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Pushing all tags for %s\n", repo)
				return registryClient.PushRepository(cmd.Context(), repo, opts)
			}
			return registryClient.PushReference(cmd.Context(), target, opts)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().BoolVar(&sign, "sign", false, "Sign the pushed image with cosign (requires cosign in PATH)")
	cmd.Flags().BoolVar(&allTags, "all-tags", false, "Push every cached tag for the repository instead of a single reference")

	decorateCommandHelp(cmd, "Push Flags")
	return cmd
}

func repositoryFrom(value string) (string, error) {
	ref, err := name.ParseReference(value)
	if err != nil {
		return "", err
	}
	return ref.Context().Name(), nil
}
