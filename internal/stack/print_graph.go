// File: internal/stack/print_graph.go
// Brief: Graph printing for plan debugging.

package stack

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

func PrintGraphDOT(w io.Writer, p *Plan) error {
	g, err := BuildGraph(p)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "digraph ktl_stack {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box,fontname=\"SF Pro Text\"];")

	clusters := make([]string, 0, len(p.ByCluster))
	for c := range p.ByCluster {
		clusters = append(clusters, c)
	}
	sort.Strings(clusters)
	for _, c := range clusters {
		fmt.Fprintf(w, "  subgraph \"cluster_%s\" {\n", safeID(c))
		fmt.Fprintf(w, "    label=\"cluster %s\";\n", c)
		for _, n := range p.ByCluster[c] {
			fmt.Fprintf(w, "    \"%s\" [label=\"%s\\nns/%s\"];\n", n.ID, n.Name, n.Namespace)
		}
		fmt.Fprintln(w, "  }")
	}

	for _, e := range g.Edges() {
		// Edge: from depends on to => to -> from.
		fmt.Fprintf(w, "  \"%s\" -> \"%s\";\n", e[1], e[0])
	}
	fmt.Fprintln(w, "}")
	return nil
}

func PrintGraphMermaid(w io.Writer, p *Plan) error {
	g, err := BuildGraph(p)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "graph TD")
	for _, n := range p.Nodes {
		fmt.Fprintf(w, "  %s[\"%s\\n%s\"]\n", safeID(n.ID), n.Name, n.Namespace)
	}
	for _, e := range g.Edges() {
		fmt.Fprintf(w, "  %s --> %s\n", safeID(e[1]), safeID(e[0]))
	}
	return nil
}

func safeID(s string) string {
	out := strings.Builder{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		default:
			out.WriteRune('_')
		}
	}
	return out.String()
}
