// File: internal/stack/git_map.go
// Brief: Map changed files to releases based on ownership directories.

package stack

import (
	"os"
	"path/filepath"
	"sort"
)

func mapChangedFiles(u *Universe, p *Plan, changedFiles []string) map[string][]string {
	mapped := map[string][]string{}

	// If a stack.yaml changed, select every release under that stack.yaml directory
	// because hierarchical defaults can affect the full subtree.
	for _, rel := range changedFiles {
		abs := filepath.Join(p.StackRoot, rel)
		if filepath.Base(abs) == stackFileName {
			stackDir := filepath.Dir(abs)
			var ids []string
			for _, n := range p.Nodes {
				if samePath(n.Dir, stackDir) || isUnder(n.Dir, stackDir) {
					ids = append(ids, n.ID)
				}
			}
			sort.Strings(ids)
			if len(ids) > 0 {
				mapped[rel] = ids
			}
			continue
		}
	}

	ownerDirs := map[string][]string{} // abs dir -> node IDs
	addOwnerDir := func(dir, id string) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		if !samePath(abs, p.StackRoot) && !isUnder(abs, p.StackRoot) && !samePath(p.StackRoot, abs) {
			return
		}
		ownerDirs[abs] = append(ownerDirs[abs], id)
	}

	for _, n := range p.Nodes {
		addOwnerDir(n.Dir, n.ID)

		for _, v := range n.Values {
			if !isLocalPath(v) {
				continue
			}
			addOwnerDir(filepath.Dir(v), n.ID)
		}

		if isExistingPath(n.Chart) {
			chartPath := n.Chart
			if st, err := os.Stat(chartPath); err == nil && !st.IsDir() {
				chartPath = filepath.Dir(chartPath)
			}
			addOwnerDir(chartPath, n.ID)
		}
	}
	for dir := range ownerDirs {
		sort.Strings(ownerDirs[dir])
	}

	for _, rel := range changedFiles {
		if _, already := mapped[rel]; already {
			continue
		}

		abs := filepath.Join(p.StackRoot, rel)
		dir := filepath.Dir(abs)
		for {
			if ids, ok := ownerDirs[dir]; ok {
				mapped[rel] = append([]string(nil), ids...)
				break
			}
			if samePath(dir, p.StackRoot) {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	_ = u // reserved for future mapping rules.
	return mapped
}
