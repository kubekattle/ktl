// File: cmd/ktl/stack_sign.go
// Brief: `ktl stack sign` command wiring.

package main

import (
	"fmt"
	"strings"

	"github.com/kubekattle/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackSignCommand(rootDir *string) *cobra.Command {
	var bundlePath string
	var keyPath string
	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Sign a stack bundle (adds signature.json)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			b := strings.TrimSpace(bundlePath)
			if b == "" {
				return fmt.Errorf("--bundle is required")
			}
			k := strings.TrimSpace(keyPath)
			if k == "" {
				return fmt.Errorf("--key is required")
			}
			_, _, priv, err := stack.LoadBundleKey(k)
			if err != nil {
				return err
			}
			if priv == nil {
				return fmt.Errorf("key %s does not contain a private key", k)
			}
			sig, err := stack.SignBundle(b, priv)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "ktl stack sign: ok (manifestSha256=%s)\n", sig.ManifestSHA256)
			return nil
		},
	}
	_ = rootDir
	cmd.Flags().StringVar(&bundlePath, "bundle", "", "Path to a .tgz bundle (from stack export or stack plan --bundle)")
	cmd.Flags().StringVar(&keyPath, "key", "", "Path to an ed25519 key JSON file (from stack keygen)")
	return cmd
}
