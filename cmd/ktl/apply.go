// File: cmd/ktl/apply.go
// Brief: CLI command wiring and implementation for 'apply'.

// Package main provides the ktl CLI entrypoints.

package main

import "github.com/spf13/cobra"

// apply.go exposes the top-level 'ktl apply' command while reusing the deploy apply implementation.

func newApplyCommand(kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	cmd := newDeployApplyCommand(nil, kubeconfig, kubeContext, logLevel, remoteAgent, "Apply Flags")
	cmd.AddCommand(newDeployPlanCommand(nil, kubeconfig, kubeContext, "Apply Plan Flags"))
	cmd.Example = `  # Apply a chart with prod values
  ktl apply --chart ./charts/web --release web-prod --namespace prod -f values/prod.yaml`
	return cmd
}
