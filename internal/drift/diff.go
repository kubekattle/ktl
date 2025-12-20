// File: internal/drift/diff.go
// Brief: Internal drift package implementation for 'diff'.

// diff.go computes human-readable drift summaries between pod snapshots.
package drift

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Diff describes the change set between two snapshots.
type Diff struct {
	Added   []PodSnapshot
	Removed []PodSnapshot
	Changed []PodChange
}

// PodChange captures human-readable reasons for a pod drift.
type PodChange struct {
	Namespace string
	Name      string
	Reasons   []string
}

// DiffSnapshots compares two snapshots and returns a change summary.
func DiffSnapshots(prev *Snapshot, curr *Snapshot) Diff {
	if curr == nil {
		return Diff{}
	}
	prevMap := map[string]PodSnapshot{}
	if prev != nil {
		for _, pod := range prev.Pods {
			prevMap[key(pod.Namespace, pod.Name)] = pod
		}
	}
	currMap := map[string]PodSnapshot{}
	for _, pod := range curr.Pods {
		currMap[key(pod.Namespace, pod.Name)] = pod
	}
	diff := Diff{}
	for key, pod := range currMap {
		if _, ok := prevMap[key]; !ok {
			diff.Added = append(diff.Added, pod)
		}
	}
	for key, pod := range prevMap {
		if _, ok := currMap[key]; !ok {
			diff.Removed = append(diff.Removed, pod)
		}
	}
	for key, currPod := range currMap {
		prevPod, ok := prevMap[key]
		if !ok {
			continue
		}
		reasons := comparePods(prevPod, currPod)
		if len(reasons) > 0 {
			diff.Changed = append(diff.Changed, PodChange{
				Namespace: currPod.Namespace,
				Name:      currPod.Name,
				Reasons:   reasons,
			})
		}
	}
	return diff
}

func comparePods(prev, curr PodSnapshot) []string {
	var reasons []string
	if prev.Phase != curr.Phase {
		reasons = append(reasons, fmt.Sprintf("phase %s -> %s", prev.Phase, curr.Phase))
	}
	if prev.Node != curr.Node {
		reasons = append(reasons, fmt.Sprintf("node %s -> %s", empty(prev.Node, "<none>"), empty(curr.Node, "<none>")))
	}
	if prev.RolloutHash != curr.RolloutHash {
		reasons = append(reasons, fmt.Sprintf("rollout hash %s -> %s", empty(prev.RolloutHash, "<none>"), empty(curr.RolloutHash, "<none>")))
	}
	if ownerStr(prev.Owners) != ownerStr(curr.Owners) {
		reasons = append(reasons, fmt.Sprintf("owners %s -> %s", ownerStr(prev.Owners), ownerStr(curr.Owners)))
	}
	readyPrev := prev.Conditions[string(coreReady)]
	readyCurr := curr.Conditions[string(coreReady)]
	if readyPrev != readyCurr {
		reasons = append(reasons, fmt.Sprintf("Ready %s -> %s", readyPrev, readyCurr))
	}
	containerReasons := compareContainers(prev.Containers, curr.Containers)
	reasons = append(reasons, containerReasons...)
	return reasons
}

const coreReady = corev1.PodReady

func compareContainers(prev, curr []ContainerSnapshot) []string {
	var reasons []string
	prevMap := map[string]ContainerSnapshot{}
	for _, c := range prev {
		prevMap[c.Name] = c
	}
	for _, c := range curr {
		p, ok := prevMap[c.Name]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("container %s added", c.Name))
			continue
		}
		if p.Ready != c.Ready {
			reasons = append(reasons, fmt.Sprintf("container %s ready %t -> %t", c.Name, p.Ready, c.Ready))
		}
		if c.RestartCount > p.RestartCount {
			reasons = append(reasons, fmt.Sprintf("container %s restarts %d -> %d", c.Name, p.RestartCount, c.RestartCount))
		}
	}
	return reasons
}

func ownerStr(owners []OwnerSnapshot) string {
	if len(owners) == 0 {
		return "<none>"
	}
	parts := make([]string, len(owners))
	for i, owner := range owners {
		parts[i] = fmt.Sprintf("%s/%s", owner.Kind, owner.Name)
	}
	return strings.Join(parts, " -> ")
}

func key(namespace, name string) string {
	return namespace + "/" + name
}

func empty(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}
