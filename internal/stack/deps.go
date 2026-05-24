package stack

import (
	"fmt"
	"sort"
	"strings"
)

func resolveDependencyNode(nodes []*ResolvedRelease, from *ResolvedRelease, depRef string) (*ResolvedRelease, error) {
	depRef = strings.TrimSpace(depRef)
	if depRef == "" {
		return nil, fmt.Errorf("empty dependency reference")
	}
	for _, n := range nodes {
		if n != nil && n.ID == depRef {
			return n, nil
		}
	}

	var sameCluster []*ResolvedRelease
	if from != nil && strings.TrimSpace(from.Cluster.Name) != "" {
		cluster := strings.TrimSpace(from.Cluster.Name)
		for _, n := range nodes {
			if n == nil {
				continue
			}
			if n.Name == depRef && strings.TrimSpace(n.Cluster.Name) == cluster {
				sameCluster = append(sameCluster, n)
			}
		}
		if len(sameCluster) == 1 {
			return sameCluster[0], nil
		}
		if len(sameCluster) > 1 {
			return nil, fmt.Errorf("dependency %q from %s is ambiguous in cluster %q (matches %v)", depRef, from.ID, cluster, nodeIDs(sameCluster))
		}
	}

	var global []*ResolvedRelease
	for _, n := range nodes {
		if n != nil && n.Name == depRef {
			global = append(global, n)
		}
	}
	if len(global) == 1 {
		return global[0], nil
	}
	if len(global) > 1 {
		fromID := ""
		if from != nil {
			fromID = from.ID
		}
		return nil, fmt.Errorf("dependency %q from %s is ambiguous (matches %v); use node id", depRef, fromID, nodeIDs(global))
	}
	return nil, fmt.Errorf("missing dependency %q", depRef)
}

func nodeIDs(nodes []*ResolvedRelease) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			out = append(out, n.ID)
		}
	}
	sort.Strings(out)
	return out
}
