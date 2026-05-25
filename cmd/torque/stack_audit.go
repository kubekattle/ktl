// File: cmd/torque/stack_audit.go
// Brief: `torque stack audit` command wiring.

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ingresslabs/torque/internal/stack"
	"github.com/spf13/cobra"
)

func newStackAuditCommand(rootDir *string) *cobra.Command {
	var runID string
	var output string
	var verify bool
	var fromBundle string
	var verifyBundle bool
	var eventsLimit int
	var includePlan bool
	var includeEvents bool
	var includeArtifacts bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show who/what/when for a stack run (sqlite-backed)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := stack.GetRunAudit(cmd.Context(), stack.RunAuditOptions{
				RootDir:          *rootDir,
				RunID:            runID,
				BundlePath:       fromBundle,
				VerifyBundle:     verifyBundle,
				Verify:           verify,
				EventsLimit:      eventsLimit,
				IncludePlan:      includePlan,
				IncludeEvents:    includeEvents,
				IncludeArtifacts: includeArtifacts,
			})
			if err != nil {
				return err
			}
			var renderErr error
			switch strings.ToLower(strings.TrimSpace(output)) {
			case "", "table":
				renderErr = stack.PrintRunAuditTable(cmd.OutOrStdout(), a)
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				renderErr = enc.Encode(a)
			case "html":
				renderErr = stack.PrintRunAuditHTML(cmd.OutOrStdout(), a)
			default:
				return fmt.Errorf("unknown --output %q (expected table|json|html)", output)
			}
			if renderErr != nil {
				return renderErr
			}
			if verify && stack.OpsAuditVerificationFailed(a) {
				return fmt.Errorf("ops audit verification failed: %d finding(s)", len(a.Ops.Findings))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "Run ID (stored in .torque/stack/state.sqlite); defaults to most recent")
	cmd.Flags().StringVar(&output, "output", "table", "Output format: table|json|html")
	cmd.Flags().BoolVar(&verify, "verify", true, "Verify event chain and run digest")
	cmd.Flags().StringVar(&fromBundle, "from-bundle", "", "Audit a portable stack run bundle (.tgz) instead of local state")
	cmd.Flags().BoolVar(&verifyBundle, "verify-bundle", true, "Verify bundle manifest digests before auditing --from-bundle")
	cmd.Flags().IntVar(&eventsLimit, "events", 1000, "How many events to include in json/html output (0 uses default, -1 means all)")
	cmd.Flags().BoolVar(&includePlan, "include-plan", true, "Include the stored run plan in json/html output")
	cmd.Flags().BoolVar(&includeEvents, "include-events", true, "Include stored events in json/html output")
	cmd.Flags().BoolVar(&includeArtifacts, "include-artifacts", true, "Include stored node artifacts in json/html output")
	return cmd
}
