// File: internal/stack/dag.go
// Brief: DAG validation and stable execution grouping.

package stack

import (
	"fmt"
	"sort"
)

func assignExecutionGroups(p *Plan) error {
	for _, nodes := range p.ByCluster {
		if err := assignClusterExecutionGroups(nodes); err != nil {
			return err
		}
	}
	return nil
}

func assignClusterExecutionGroups(nodes []*ResolvedRelease) error {
	byName := map[string]*ResolvedRelease{}
	for _, n := range nodes {
		byName[n.Name] = n
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
	sort.Strings(ready)

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
		sort.Strings(ready)
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
