// File: internal/stack/print.go
// Brief: Human-friendly plan printing.

package stack

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
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

	fmt.Fprintln(tw, "WAVE\tID\tDIR\tCHART\tTAGS\tNEEDS\tSELECTED_BY")
	nodes := append([]*ResolvedRelease(nil), p.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].ExecutionGroup != nodes[j].ExecutionGroup {
			return nodes[i].ExecutionGroup < nodes[j].ExecutionGroup
		}
		return nodes[i].ID < nodes[j].ID
	})
	for _, n := range nodes {
		dir := n.Dir
		if rel, err := filepath.Rel(p.StackRoot, n.Dir); err == nil {
			dir = rel
		}
		selectedBy := strings.Join(n.SelectedBy, ",")
		if len(selectedBy) > 140 {
			selectedBy = selectedBy[:140] + "…"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%v\t%v\t%s\n", n.ExecutionGroup, n.ID, dir, n.Chart, n.Tags, n.Needs, selectedBy)
	}
	return nil
}
