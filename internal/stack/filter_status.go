// File: internal/stack/filter_status.go
// Brief: Filter plan nodes based on status maps (resume/rerun-failed).

package stack

import "sort"

func FilterByNodeStatus(p *Plan, statusByID map[string]string, wantStatuses []string) *Plan {
	if p == nil {
		return nil
	}
	want := map[string]struct{}{}
	for _, s := range wantStatuses {
		if s != "" {
			want[s] = struct{}{}
		}
	}
	var nodes []*ResolvedRelease
	for _, n := range p.Nodes {
		if _, ok := want[statusByID[n.ID]]; ok {
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
	_ = assignExecutionGroups(out)
	return out
}
