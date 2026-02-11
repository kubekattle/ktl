// File: internal/stack/print_runs.go
// Brief: Human-friendly printing for `ktl stack runs`.

package stack

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func PrintRunsTable(w io.Writer, runs []RunIndexEntry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	fmt.Fprintln(tw, "RUN\tSTATUS\tPLANNED\tSUCCEEDED\tFAILED\tBLOCKED\tRUNNING\tUPDATED")
	for _, r := range runs {
		status := r.Status
		if !r.HasSummary {
			status = "unknown"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
			r.RunID,
			strings.ToUpper(status),
			r.Totals.Planned,
			r.Totals.Succeeded,
			r.Totals.Failed,
			r.Totals.Blocked,
			r.Totals.Running,
			r.UpdatedAt,
		)
	}
	return nil
}
