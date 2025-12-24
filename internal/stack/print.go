// File: internal/stack/print.go
// Brief: Human-friendly plan printing.

package stack

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
)

func PrintPlanTable(w io.Writer, p *Plan) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	fmt.Fprintf(tw, "STACK\t%s\n", p.StackName)
	if p.Profile != "" {
		fmt.Fprintf(tw, "PROFILE\t%s\n", p.Profile)
	}
	fmt.Fprintf(tw, "ROOT\t%s\n", p.StackRoot)
	fmt.Fprintln(tw)

	fmt.Fprintln(tw, "WAVE\tID\tCHART\tTAGS\tNEEDS")
	nodes := append([]*ResolvedRelease(nil), p.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].ExecutionGroup != nodes[j].ExecutionGroup {
			return nodes[i].ExecutionGroup < nodes[j].ExecutionGroup
		}
		return nodes[i].ID < nodes[j].ID
	})
	for _, n := range nodes {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%v\t%v\n", n.ExecutionGroup, n.ID, n.Chart, n.Tags, n.Needs)
	}
	return nil
}
