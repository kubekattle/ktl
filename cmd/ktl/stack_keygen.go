// File: cmd/ktl/stack_keygen.go
// Brief: `ktl stack keygen` command wiring.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackKeygenCommand(rootDir *string) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate an ed25519 key for signing bundles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := strings.TrimSpace(outPath)
			if out == "" {
				out = filepath.Join(*rootDir, ".ktl", "stack", "keys", "ed25519.json")
			}
			k, err := stack.GenerateEd25519Key()
			if err != nil {
				return err
			}
			raw, err := json.MarshalIndent(k, "", "  ")
			if err != nil {
				return err
			}
			raw = append(raw, '\n')
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(out, raw, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "ktl stack keygen: wrote %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&outPath, "out", "", "Output key path (defaults to --root/.ktl/stack/keys/ed25519.json)")
	return cmd
}
