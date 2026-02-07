package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func remoteToken(cmd *cobra.Command) string {
	if cmd == nil {
		return strings.TrimSpace(os.Getenv("KTL_REMOTE_TOKEN"))
	}
	root := cmd.Root()
	if root == nil {
		return strings.TrimSpace(os.Getenv("KTL_REMOTE_TOKEN"))
	}
	if flag := root.PersistentFlags().Lookup("remote-token"); flag != nil {
		if v := strings.TrimSpace(flag.Value.String()); v != "" {
			return v
		}
	}
	return strings.TrimSpace(os.Getenv("KTL_REMOTE_TOKEN"))
}
