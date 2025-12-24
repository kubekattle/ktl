// File: internal/stack/filter.go
// Brief: Plan filtering helpers.

package stack

import (
	"sort"
)

func FilterByClusters(p *Plan, clusters []string) *Plan {
	if len(clusters) == 0 {
		return p
	}
	want := map[string]struct{}{}
	for _, c := range clusters {
		if c == "" {
			continue
		}
		want[c] = struct{}{}
	}
	nodes := make([]*ResolvedRelease, 0, len(p.Nodes))
	for _, n := range p.Nodes {
		if _, ok := want[n.Cluster.Name]; ok {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	out := &Plan{
		StackRoot: p.StackRoot,
		StackName: p.StackName,
		Profile:   p.Profile,
		Nodes:     nodes,
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range nodes {
		out.ByID[n.ID] = n
		out.ByCluster[n.Cluster.Name] = append(out.ByCluster[n.Cluster.Name], n)
	}
	return out
}
