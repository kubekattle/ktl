// File: internal/stack/select.go
// Brief: Selection engine + reason tracking.

package stack

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type Selector struct {
	Tags      []string
	FromPaths []string
	Releases  []string
	GitRange  string

	GitIncludeDeps       bool
	GitIncludeDependents bool

	IncludeDeps       bool
	IncludeDependents bool

	// AllowMissingDeps relaxes validation and treats missing needs as "skipped":
	// the selected plan is pruned so nodes only depend on other selected nodes.
	AllowMissingDeps bool
}

type SelectResult struct {
	Plan     *Plan
	Selected []*ResolvedRelease
}

func Select(u *Universe, p *Plan, clusters []string, sel Selector) (*Plan, error) {
	p = FilterByClusters(p, clusters)
	if p == nil {
		return nil, fmt.Errorf("plan is nil")
	}

	normalizedTags := normalizeStrings(sel.Tags)
	normalizedPaths, err := normalizePaths(p.StackRoot, sel.FromPaths)
	if err != nil {
		return nil, err
	}
	normalizedReleaseNames := normalizeStrings(sel.Releases)

	// If no selectors are provided, default to the whole universe (after cluster filter).
	hasAnySelector := len(normalizedTags) > 0 || len(normalizedPaths) > 0 || len(normalizedReleaseNames) > 0 || strings.TrimSpace(sel.GitRange) != ""
	selectedIDs := map[string]struct{}{}
	reasonsByID := map[string][]string{}

	if !hasAnySelector {
		for _, n := range p.Nodes {
			selectedIDs[n.ID] = struct{}{}
			reasonsByID[n.ID] = append(reasonsByID[n.ID], "default:all")
		}
	} else {
		if len(normalizedTags) > 0 {
			tagSet := map[string]struct{}{}
			for _, t := range normalizedTags {
				tagSet[t] = struct{}{}
			}
			for _, n := range p.Nodes {
				for _, t := range n.Tags {
					if _, ok := tagSet[t]; ok {
						selectedIDs[n.ID] = struct{}{}
						reasonsByID[n.ID] = append(reasonsByID[n.ID], "explicit:tag:"+t)
						break
					}
				}
			}
		}

		if len(normalizedPaths) > 0 {
			for _, n := range p.Nodes {
				relDir, _ := filepath.Rel(p.StackRoot, n.Dir)
				absDir, _ := filepath.Abs(n.Dir)
				for _, want := range normalizedPaths {
					if isUnder(absDir, want) {
						selectedIDs[n.ID] = struct{}{}
						reasonsByID[n.ID] = append(reasonsByID[n.ID], "explicit:path:"+relDir)
						break
					}
				}
			}
		}

		if len(normalizedReleaseNames) > 0 {
			for _, name := range normalizedReleaseNames {
				var matches []*ResolvedRelease
				for _, n := range p.Nodes {
					if n.Name == name {
						matches = append(matches, n)
					}
				}
				if len(matches) == 0 {
					return nil, fmt.Errorf("unknown release %q", name)
				}
				if len(matches) > 1 {
					var ids []string
					for _, m := range matches {
						ids = append(ids, m.ID)
					}
					sort.Strings(ids)
					return nil, fmt.Errorf("ambiguous release name %q (matches %v); use --cluster to disambiguate", name, ids)
				}
				selectedIDs[matches[0].ID] = struct{}{}
				reasonsByID[matches[0].ID] = append(reasonsByID[matches[0].ID], "explicit:release:"+name)
			}
		}

		if strings.TrimSpace(sel.GitRange) != "" {
			changed, err := GitChangedFiles(p.StackRoot, sel.GitRange)
			if err != nil {
				return nil, err
			}
			mapped := mapChangedFiles(u, p, changed)
			for file, ids := range mapped {
				for _, id := range ids {
					selectedIDs[id] = struct{}{}
					reasonsByID[id] = append(reasonsByID[id], "explicit:git:"+file)
				}
			}
		}
	}

	includeDeps := sel.IncludeDeps || sel.GitIncludeDeps
	includeDependents := sel.IncludeDependents || sel.GitIncludeDependents
	if includeDeps || includeDependents {
		g, err := BuildGraph(p)
		if err != nil {
			return nil, err
		}
		if includeDeps {
			for id := range cloneSet(selectedIDs) {
				for _, depID := range g.DepsOf(id) {
					if _, ok := selectedIDs[depID]; ok {
						continue
					}
					selectedIDs[depID] = struct{}{}
					reasonsByID[depID] = append(reasonsByID[depID], "expand:dep-of:"+id)
				}
			}
		}
		if includeDependents {
			for id := range cloneSet(selectedIDs) {
				for _, depID := range g.DependentsOf(id) {
					if _, ok := selectedIDs[depID]; ok {
						continue
					}
					selectedIDs[depID] = struct{}{}
					reasonsByID[depID] = append(reasonsByID[depID], "expand:dependent-of:"+id)
				}
			}
		}
	}

	outNodes := make([]*ResolvedRelease, 0, len(selectedIDs))
	for _, n := range p.Nodes {
		if _, ok := selectedIDs[n.ID]; ok {
			cp := *n
			cp.SelectedBy = dedupeStrings(reasonsByID[n.ID])
			outNodes = append(outNodes, &cp)
		}
	}

	out := &Plan{
		StackRoot: p.StackRoot,
		StackName: p.StackName,
		Profile:   p.Profile,
		Nodes:     outNodes,
		Runner:    p.Runner,
		Hooks:     p.Hooks,
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range outNodes {
		out.ByID[n.ID] = n
		out.ByCluster[n.Cluster.Name] = append(out.ByCluster[n.Cluster.Name], n)
	}

	if sel.AllowMissingDeps {
		pruneMissingNeeds(out)
	} else {
		// Ensure selected plan is internally consistent (missing deps are user error when not expanded).
		if err := validateSelectedNeeds(out); err != nil {
			return nil, err
		}
	}
	// Recompute waves for the selected graph.
	if err := assignExecutionGroups(out); err != nil {
		return nil, err
	}
	if order, err := ComputeExecutionOrder(out, "apply"); err == nil {
		out.Order = order
	}
	return out, nil
}

func pruneMissingNeeds(p *Plan) {
	for _, nodes := range p.ByCluster {
		byName := map[string]struct{}{}
		for _, n := range nodes {
			byName[n.Name] = struct{}{}
		}
		for _, n := range nodes {
			if len(n.Needs) == 0 {
				continue
			}
			out := make([]string, 0, len(n.Needs))
			for _, dep := range n.Needs {
				if _, ok := byName[dep]; ok {
					out = append(out, dep)
				}
			}
			n.Needs = out
		}
	}
}

func validateSelectedNeeds(p *Plan) error {
	for _, nodes := range p.ByCluster {
		byName := map[string]*ResolvedRelease{}
		for _, n := range nodes {
			byName[n.Name] = n
		}
		for _, n := range nodes {
			for _, dep := range n.Needs {
				if _, ok := byName[dep]; !ok {
					return fmt.Errorf("selected release %s needs %q which is not selected (rerun with --include-deps)", n.ID, dep)
				}
			}
		}
	}
	return nil
}

func normalizeStrings(in []string) []string {
	var out []string
	for _, v := range in {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func normalizePaths(root string, in []string) ([]string, error) {
	var out []string
	for _, p := range normalizeStrings(in) {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(root, p)
		}
		abs, err := filepath.Abs(abs)
		if err != nil {
			return nil, err
		}
		out = append(out, filepath.Clean(abs))
	}
	return out, nil
}

func isUnder(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func cloneSet(in map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
