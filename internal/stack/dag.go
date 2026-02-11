// File: internal/stack/dag.go
// Brief: DAG validation and stable execution grouping.

package stack

import (
	"fmt"
	"sort"
	"strings"
)

func assignExecutionGroups(p *Plan) error {
	for _, nodes := range p.ByCluster {
		if err := assignClusterExecutionGroups(nodes); err != nil {
			return err
		}
	}
	return nil
}

// RecomputeExecutionGroups recalculates execution groups after mutating dependencies.
func RecomputeExecutionGroups(p *Plan) error {
	if err := assignExecutionGroups(p); err != nil {
		return err
	}
	if order, err := ComputeExecutionOrder(p, "apply"); err == nil {
		p.Order = order
	}
	return nil
}

func assignClusterExecutionGroups(nodes []*ResolvedRelease) error {
	byName := map[string]*ResolvedRelease{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	byID := map[string]*ResolvedRelease{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	inDegree := map[string]int{}
	dependents := map[string][]string{}
	for _, n := range nodes {
		inDegree[n.ID] = 0
	}

	for _, n := range nodes {
		for _, depName := range n.Needs {
			dep, ok := byName[depName]
			if !ok {
				return fmt.Errorf("release %s needs missing dependency %q", n.ID, depName)
			}
			if dep.Cluster.Name != n.Cluster.Name {
				return fmt.Errorf("release %s needs %q but it resolves to a different cluster (%s)", n.ID, depName, dep.Cluster.Name)
			}
			inDegree[n.ID]++
			dependents[dep.ID] = append(dependents[dep.ID], n.ID)
		}
	}
	for k := range dependents {
		sort.Strings(dependents[k])
	}

	ready := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if inDegree[n.ID] == 0 {
			ready = append(ready, n.ID)
		}
	}
	sort.Slice(ready, func(i, j int) bool {
		return releaseReadyKey(byID[ready[i]]) < releaseReadyKey(byID[ready[j]])
	})

	group := 0
	assigned := 0
	for len(ready) > 0 {
		wave := append([]string(nil), ready...)
		ready = ready[:0]
		for _, id := range wave {
			node := findByID(nodes, id)
			if node == nil {
				return fmt.Errorf("internal error: missing node %s", id)
			}
			node.ExecutionGroup = group
			assigned++
		}
		for _, id := range wave {
			for _, depID := range dependents[id] {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					ready = append(ready, depID)
				}
			}
		}
		sort.Slice(ready, func(i, j int) bool {
			return releaseReadyKey(byID[ready[i]]) < releaseReadyKey(byID[ready[j]])
		})
		group++
	}
	if assigned != len(nodes) {
		// Find cycle participants for a better error.
		var stuck []string
		for _, n := range nodes {
			if inDegree[n.ID] > 0 {
				stuck = append(stuck, n.ID)
			}
		}
		sort.Strings(stuck)
		if cycle := findCyclePath(stuck, dependents, inDegree); len(cycle) > 0 {
			return fmt.Errorf("dependency cycle detected: %s", cycleString(cycle, byID))
		}
		return fmt.Errorf("dependency cycle detected (%d nodes): %v", len(stuck), stuck)
	}
	return nil
}

func findByID(nodes []*ResolvedRelease, id string) *ResolvedRelease {
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func findCyclePath(stuck []string, dependents map[string][]string, inDegree map[string]int) []string {
	// Build deps from dependents (reverse edge: dep -> dependent).
	deps := map[string][]string{}
	for dep, outs := range dependents {
		for _, to := range outs {
			deps[to] = append(deps[to], dep)
		}
	}
	// DFS to find a back-edge cycle among stuck nodes.
	stuckSet := map[string]struct{}{}
	for _, id := range stuck {
		stuckSet[id] = struct{}{}
	}
	vis := map[string]bool{}
	onStack := map[string]bool{}
	var stack []string
	var cycle []string
	var dfs func(string) bool
	dfs = func(id string) bool {
		if _, ok := stuckSet[id]; !ok {
			return false
		}
		vis[id] = true
		onStack[id] = true
		stack = append(stack, id)
		for _, dep := range deps[id] {
			if _, ok := stuckSet[dep]; !ok {
				continue
			}
			if !vis[dep] {
				if dfs(dep) {
					return true
				}
				continue
			}
			if onStack[dep] {
				// Extract cycle from dep to end.
				idx := -1
				for i := range stack {
					if stack[i] == dep {
						idx = i
						break
					}
				}
				if idx >= 0 {
					cycle = append([]string(nil), stack[idx:]...)
				} else {
					cycle = []string{dep, id}
				}
				return true
			}
		}
		onStack[id] = false
		stack = stack[:len(stack)-1]
		return false
	}
	for _, id := range stuck {
		if inDegree[id] <= 0 || vis[id] {
			continue
		}
		if dfs(id) {
			break
		}
	}
	return cycle
}

func cycleString(cycle []string, byID map[string]*ResolvedRelease) string {
	parts := make([]string, 0, len(cycle)+1)
	for _, id := range cycle {
		n := byID[id]
		if n != nil && n.Name != "" {
			parts = append(parts, fmt.Sprintf("%s(%s)", id, n.Name))
		} else {
			parts = append(parts, id)
		}
	}
	if len(cycle) > 0 {
		parts = append(parts, parts[0])
	}
	// Attach edge hints when possible (from depends on to).
	var edges []string
	for i := 0; i+1 < len(cycle); i++ {
		from := byID[cycle[i]]
		to := byID[cycle[i+1]]
		if from == nil || to == nil {
			continue
		}
		hint := edgeHint(from, to.Name)
		if hint == "" {
			hint = "declared"
		}
		edges = append(edges, fmt.Sprintf("%s -> %s (%s)", from.Name, to.Name, hint))
	}
	if len(edges) == 0 {
		return fmt.Sprintf("%v", parts)
	}
	return fmt.Sprintf("%v edges=%v", parts, edges)
}

func edgeHint(from *ResolvedRelease, depName string) string {
	if from == nil || depName == "" {
		return ""
	}
	for _, inf := range from.InferredNeeds {
		if inf.Name != depName {
			continue
		}
		types := map[string]struct{}{}
		for _, r := range inf.Reasons {
			if r.Type != "" {
				types[r.Type] = struct{}{}
			}
		}
		if len(types) == 0 {
			return "inferred"
		}
		var out []string
		for t := range types {
			out = append(out, t)
		}
		sort.Strings(out)
		return strings.Join(out, "+")
	}
	return ""
}
