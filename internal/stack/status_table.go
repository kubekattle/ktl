// File: internal/stack/status_table.go
// Brief: Human-friendly run status table output.

package stack

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

func PrintRunStatusTable(w io.Writer, runID string, s *RunSummary) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "RUN\t%s\n", runID)
	fmt.Fprintf(tw, "STATUS\t%s\n", s.Status)
	fmt.Fprintf(tw, "STARTED\t%s\n", s.StartedAt)
	fmt.Fprintf(tw, "UPDATED\t%s\n", s.UpdatedAt)
	fmt.Fprintf(tw, "TOTALS\tplanned=%d succeeded=%d failed=%d blocked=%d running=%d\n", s.Totals.Planned, s.Totals.Succeeded, s.Totals.Failed, s.Totals.Blocked, s.Totals.Running)
	fmt.Fprintln(tw)

	fmt.Fprintln(tw, "ID\tSTATUS\tATTEMPT\tERROR")

	var order []string
	if len(s.Order) > 0 {
		order = append(order, s.Order...)
	} else {
		for id := range s.Nodes {
			order = append(order, id)
		}
		sort.Strings(order)
	}

	for _, id := range order {
		ns := s.Nodes[id]
		err := strings.TrimSpace(ns.Error)
		if len(err) > 140 {
			err = err[:140] + "…"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", id, strings.ToUpper(ns.Status), ns.Attempt, err)
	}
	return nil
}
