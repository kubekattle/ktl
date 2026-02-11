// File: internal/stack/graph.go
// Brief: Graph utilities for dependency expansion.

package stack

import (
	"fmt"
	"sort"
)

type Graph struct {
	deps       map[string][]string
	dependents map[string][]string
}

func BuildGraph(p *Plan) (*Graph, error) {
	g := &Graph{
		deps:       map[string][]string{},
		dependents: map[string][]string{},
	}
	for _, nodes := range p.ByCluster {
		byName := map[string]*ResolvedRelease{}
		for _, n := range nodes {
			byName[n.Name] = n
		}
		for _, n := range nodes {
			for _, depName := range n.Needs {
				dep, ok := byName[depName]
				if !ok {
					return nil, fmt.Errorf("release %s needs missing dependency %q", n.ID, depName)
				}
				g.deps[n.ID] = append(g.deps[n.ID], dep.ID)
				g.dependents[dep.ID] = append(g.dependents[dep.ID], n.ID)
			}
		}
	}
	for k := range g.deps {
		sort.Strings(g.deps[k])
	}
	for k := range g.dependents {
		sort.Strings(g.dependents[k])
	}
	return g, nil
}

func (g *Graph) DepsOf(id string) []string {
	var out []string
	seen := map[string]struct{}{}
	var walk func(string)
	walk = func(cur string) {
		for _, dep := range g.deps[cur] {
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			out = append(out, dep)
			walk(dep)
		}
	}
	walk(id)
	sort.Strings(out)
	return out
}

func (g *Graph) DependentsOf(id string) []string {
	var out []string
	seen := map[string]struct{}{}
	var walk func(string)
	walk = func(cur string) {
		for _, dep := range g.dependents[cur] {
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			out = append(out, dep)
			walk(dep)
		}
	}
	walk(id)
	sort.Strings(out)
	return out
}

func (g *Graph) Edges() [][2]string {
	var edges [][2]string
	for from, deps := range g.deps {
		for _, to := range deps {
			edges = append(edges, [2]string{from, to})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i][0] != edges[j][0] {
			return edges[i][0] < edges[j][0]
		}
		return edges[i][1] < edges[j][1]
	})
	return edges
}
