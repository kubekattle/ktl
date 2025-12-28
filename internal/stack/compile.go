// File: internal/stack/compile.go
// Brief: Compiler: discovery + merge + validation into a resolved DAG.

package stack

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type CompileOptions struct {
	Profile string
}

type Plan struct {
	StackRoot string                        `json:"stackRoot"`
	StackName string                        `json:"stackName"`
	Profile   string                        `json:"profile"`
	Nodes     []*ResolvedRelease            `json:"nodes"`
	Order     []string                      `json:"order,omitempty"`
	Runner    RunnerResolved                `json:"runner,omitempty"`
	Hooks     StackHooksConfig              `json:"hooks,omitempty"`
	ByID      map[string]*ResolvedRelease   `json:"-"`
	ByCluster map[string][]*ResolvedRelease `json:"-"`
}

func Compile(u *Universe, opts CompileOptions) (*Plan, error) {
	profile := strings.TrimSpace(opts.Profile)
	if profile == "" {
		profile = strings.TrimSpace(u.DefaultProfile)
	}

	stackHooks, err := ResolveStackHooksConfig(u, profile)
	if err != nil {
		return nil, err
	}

	nodes := make([]*ResolvedRelease, 0, len(u.Releases))
	for _, dr := range u.Releases {
		node, err := resolveRelease(u, dr, profile)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	byID := make(map[string]*ResolvedRelease, len(nodes))
	byCluster := map[string][]*ResolvedRelease{}
	for _, n := range nodes {
		if _, exists := byID[n.ID]; exists {
			return nil, fmt.Errorf("duplicate release id %s", n.ID)
		}
		byID[n.ID] = n
		byCluster[n.Cluster.Name] = append(byCluster[n.Cluster.Name], n)
	}

	for clusterName := range byCluster {
		if strings.TrimSpace(clusterName) == "" {
			return nil, fmt.Errorf("cluster.name is required for every release (missing on at least one release)")
		}
	}

	for clusterName, list := range byCluster {
		seenName := map[string]string{}
		for _, n := range list {
			if prev, ok := seenName[n.Name]; ok {
				return nil, fmt.Errorf("duplicate release name %q in cluster %q (%s vs %s)", n.Name, clusterName, prev, n.ID)
			}
			seenName[n.Name] = n.ID
		}
	}

	p := &Plan{
		StackRoot: u.RootDir,
		StackName: u.StackName,
		Profile:   profile,
		Nodes:     nodes,
		Hooks:     filterHooksRunOnce(stackHooks, true),
		ByID:      byID,
		ByCluster: byCluster,
	}

	if r, err := ResolveRunnerConfig(u, profile); err == nil {
		p.Runner = r
	} else {
		return nil, err
	}

	if err := assignExecutionGroups(p); err != nil {
		return nil, err
	}
	if order, err := ComputeExecutionOrder(p, "apply"); err == nil {
		p.Order = order
	}
	return p, nil
}

func resolveRelease(u *Universe, dr discoveredRelease, profile string) (*ResolvedRelease, error) {
	var leaf ReleaseSpec
	switch {
	case dr.FromFile != nil:
		leaf = ReleaseSpec{
			Name:         dr.FromFile.Name,
			Chart:        dr.FromFile.Chart,
			ChartVersion: dr.FromFile.ChartVersion,
			Wave:         dr.FromFile.Wave,
			Critical:     dr.FromFile.Critical,
			Parallelism:  dr.FromFile.Parallelism,
			Cluster:      dr.FromFile.Cluster,
			Namespace:    dr.FromFile.Namespace,
			Values:       dr.FromFile.Values,
			Set:          dr.FromFile.Set,
			Tags:         dr.FromFile.Tags,
			Needs:        dr.FromFile.Needs,
			Apply:        dr.FromFile.Apply,
			Delete:       dr.FromFile.Delete,
			Hooks:        dr.FromFile.Hooks,
		}
	case dr.FromInline != nil:
		leaf = *dr.FromInline
	default:
		return nil, fmt.Errorf("internal error: discovered release without source")
	}

	if strings.TrimSpace(leaf.Name) == "" {
		return nil, fmt.Errorf("%s: release name is required", dr.Dir)
	}
	if strings.TrimSpace(leaf.Chart) == "" {
		return nil, fmt.Errorf("%s: chart is required for release %s", dr.Dir, leaf.Name)
	}

	n := &ResolvedRelease{
		Name: leaf.Name,
		Dir:  dr.Dir,
		Set:  map[string]string{},
	}

	chain, err := stackChain(u, dr.Dir)
	if err != nil {
		return nil, err
	}
	for _, dir := range chain {
		sf, ok := u.Stacks[dir]
		if !ok {
			continue
		}
		mergeDefaults(n, dir, sf.Defaults)
		allowRunOnce := samePath(dir, u.RootDir)
		if err := ValidateHooksConfig(sf.Hooks, allowRunOnce, fmt.Sprintf("%s hooks", dir)); err != nil {
			return nil, err
		}
		mergeHooks(n, dir, sf.Hooks)
		if profile != "" {
			if sp, ok := sf.Profiles[profile]; ok {
				mergeDefaults(n, dir, sp.Defaults)
				if err := ValidateHooksConfig(sp.Hooks, allowRunOnce, fmt.Sprintf("%s profiles.%s.hooks", dir, profile)); err != nil {
					return nil, err
				}
				mergeHooks(n, dir, sp.Hooks)
			}
		}
	}
	if err := ValidateHooksConfig(leaf.Hooks, false, fmt.Sprintf("%s release %s hooks", dr.Dir, leaf.Name)); err != nil {
		return nil, err
	}
	mergeReleaseOverride(n, dr.Dir, leaf)

	if n.Namespace == "" {
		n.Namespace = "default"
	}
	n.ID = fmt.Sprintf("%s/%s/%s", n.Cluster.Name, n.Namespace, n.Name)
	return n, nil
}

func stackChain(u *Universe, dir string) ([]string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(u.RootDir)
	cur := filepath.Clean(absDir)
	var chain []string
	for {
		chain = append(chain, cur)
		if cur == root {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return nil, fmt.Errorf("release %s is outside stack root %s", absDir, root)
		}
		cur = parent
	}
	// Root-to-leaf.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func resolvePath(baseDir, p string) string {
	pp := strings.TrimSpace(p)
	if pp == "" {
		return pp
	}
	// Keep non-filesystem chart refs (oci://, repo/name) untouched.
	if strings.Contains(pp, "://") {
		return pp
	}
	if filepath.IsAbs(pp) {
		return filepath.Clean(pp)
	}
	return filepath.Clean(filepath.Join(baseDir, pp))
}
