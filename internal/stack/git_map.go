// File: internal/stack/git_map.go
// Brief: Map changed files to releases based on ownership directories.

package stack

import (
	"path/filepath"
	"sort"
)

func mapChangedFiles(u *Universe, p *Plan, changedFiles []string) map[string][]string {
	ownerDirs := map[string][]string{} // abs dir -> node IDs
	for _, n := range p.Nodes {
		abs, err := filepath.Abs(n.Dir)
		if err != nil {
			continue
		}
		ownerDirs[abs] = append(ownerDirs[abs], n.ID)
	}
	for dir := range ownerDirs {
		sort.Strings(ownerDirs[dir])
	}

	mapped := map[string][]string{}
	for _, rel := range changedFiles {
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

	_ = u // reserved for future mapping rules (stack.yaml releases ownership).
	return mapped
}
