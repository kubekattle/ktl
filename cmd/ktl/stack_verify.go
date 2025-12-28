// File: cmd/ktl/stack_verify.go
// Brief: `ktl stack verify` command wiring.

package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackVerifyCommand(rootDir *string) *cobra.Command {
	var bundlePath string
	var pubPath string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify a signed stack bundle (signature.json)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b := strings.TrimSpace(bundlePath)
			if b == "" {
				return fmt.Errorf("--bundle is required")
			}
			var trustedPub []byte
			if p := strings.TrimSpace(pubPath); p != "" {
				_, pub, _, err := stack.LoadBundleKey(p)
				if err != nil {
					return err
				}
				trustedPub = pub
			}
			sig, err := stack.VerifyBundle(b, trustedPub)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "ktl stack verify: ok (manifestSha256=%s)\n", sig.ManifestSHA256)
			return nil
		},
	}
	_ = rootDir
	cmd.Flags().StringVar(&bundlePath, "bundle", "", "Path to a .tgz bundle containing manifest.json + signature.json")
	cmd.Flags().StringVar(&pubPath, "pub", "", "Optional trusted public key (ed25519 key JSON); overrides embedded public key")
	return cmd
}
