package stack

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func PrintRunAuditTable(w io.Writer, a *RunAudit) error {
	if a == nil {
		return fmt.Errorf("audit is nil")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "RUN\t%s\n", a.RunID)
	fmt.Fprintf(tw, "STATUS\t%s\n", strings.ToUpper(strings.TrimSpace(a.Status)))
	if a.CreatedBy != "" {
		fmt.Fprintf(tw, "ACTOR\t%s\n", a.CreatedBy)
	}
	if a.Host != "" || a.PID != 0 {
		fmt.Fprintf(tw, "HOST\t%s (pid=%d)\n", a.Host, a.PID)
	}
	if a.CIRunURL != "" {
		fmt.Fprintf(tw, "CI\t%s\n", a.CIRunURL)
	}
	if a.GitAuthor != "" {
		fmt.Fprintf(tw, "GIT_AUTHOR\t%s\n", a.GitAuthor)
	}
	if a.KubeContext != "" {
		fmt.Fprintf(tw, "KUBE_CONTEXT\t%s\n", a.KubeContext)
	}
	fmt.Fprintf(tw, "CREATED\t%s\n", a.CreatedAt)
	fmt.Fprintf(tw, "UPDATED\t%s\n", a.UpdatedAt)
	if a.CompletedAt != "" {
		fmt.Fprintf(tw, "COMPLETED\t%s\n", a.CompletedAt)
	}
	if a.StatePath != "" {
		fmt.Fprintf(tw, "STATE\t%s\n", a.StatePath)
	}
	if a.FollowCommand != "" {
		fmt.Fprintf(tw, "FOLLOW\t%s\n", a.FollowCommand)
	}

	if a.Summary != nil {
		fmt.Fprintf(tw, "STARTED\t%s\n", a.Summary.StartedAt)
		fmt.Fprintf(tw, "SUMMARY\tplanned=%d succeeded=%d failed=%d blocked=%d running=%d\n",
			a.Summary.Totals.Planned, a.Summary.Totals.Succeeded, a.Summary.Totals.Failed, a.Summary.Totals.Blocked, a.Summary.Totals.Running)
	}

	if strings.TrimSpace(a.RunDigest) != "" {
		fmt.Fprintf(tw, "RUN_DIGEST\t%s\n", a.RunDigest)
	}
	if len(a.Artifacts) > 0 {
		fmt.Fprintf(tw, "ARTIFACTS\t%d\n", len(a.Artifacts))
	}
	if a.Ops != nil {
		fmt.Fprintf(tw, "OPS\tstatus=%s verification=%s findings=%d targets=%d facts=%d locks=%d policy=%d hostCommands=%d\n",
			a.Ops.Status,
			a.Ops.Verification.Status,
			len(a.Ops.Findings),
			a.Ops.Summary.SelectedTargets,
			a.Ops.Summary.FactEvidence,
			a.Ops.Summary.Locks,
			a.Ops.Summary.PolicyDecisions,
			a.Ops.Summary.HostCommands,
		)
		if a.Ops.Replay != nil {
			fmt.Fprintf(tw, "OPS_REPLAY\tstatus=%s checks=%d passed=%d blocked=%d event=%t\n",
				a.Ops.Replay.Status, a.Ops.Replay.Checks, a.Ops.Replay.Passed, a.Ops.Replay.Blocked, a.Ops.Replay.EventSeen)
		}
		if a.Ops.Preflight != nil {
			fmt.Fprintf(tw, "OPS_PREFLIGHT\tstatus=%s checks=%d passed=%d blocked=%d event=%t\n",
				a.Ops.Preflight.Status, a.Ops.Preflight.Checks, a.Ops.Preflight.Passed, a.Ops.Preflight.Blocked, a.Ops.Preflight.EventSeen)
		}
		for i, finding := range a.Ops.Findings {
			fmt.Fprintf(tw, "OPS_FINDING_%d\tcode=%s node=%s artifact=%s target=%s reason=%s\n",
				i+1, finding.Code, finding.NodeID, finding.Artifact, finding.TargetID, finding.Reason)
		}
	}
	fmt.Fprintf(tw, "EVENTS_OK\t%t\n", a.Integrity.EventsOK)
	if a.Integrity.EventsError != "" {
		fmt.Fprintf(tw, "EVENTS_ERROR\t%s\n", a.Integrity.EventsError)
	}
	if a.Integrity.LastEventDigest != "" {
		fmt.Fprintf(tw, "LAST_EVENT\t%s\n", a.Integrity.LastEventDigest)
	}
	if a.Integrity.StoredLastDigest != "" && a.Integrity.StoredLastDigest != a.Integrity.LastEventDigest {
		fmt.Fprintf(tw, "STORED_LAST\t%s\n", a.Integrity.StoredLastDigest)
	}

	fmt.Fprintf(tw, "DIGEST_OK\t%t\n", a.Integrity.RunDigestOK)
	if a.Integrity.RunDigestError != "" {
		fmt.Fprintf(tw, "DIGEST_ERROR\t%s\n", a.Integrity.RunDigestError)
	}
	if len(a.FailureClusters) > 0 {
		for i, c := range a.FailureClusters {
			fmt.Fprintf(tw, "FAILURE_%d\tclass=%s nodes=%d events=%d digest=%s\n", i+1, c.ErrorClass, c.AffectedNodes, c.FailedEvents, c.ErrorDigest)
			if len(c.ExampleNodeIDs) > 0 {
				fmt.Fprintf(tw, "FAILURE_%d_NODES\t%s\n", i+1, strings.Join(c.ExampleNodeIDs, ","))
			}
		}
	}
	return nil
}
