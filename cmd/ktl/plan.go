// File: cmd/ktl/plan.go
// Brief: CLI command wiring and implementation for 'plan'.

// Package main provides the ktl CLI entrypoints.

package main

import "github.com/spf13/cobra"

// plan.go exposes the top-level 'ktl plan' command while reusing the deploy plan implementation.

func newPlanCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	cmd := newDeployPlanCommand(nil, kubeconfig, kubeContext, "Plan Flags")
	cmd.Example = `  # Preview the impact of an upgrade
  ktl plan --chart ./charts/web --release web-prod --namespace prod -f values/prod.yaml`
	return cmd
}
