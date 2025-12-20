// File: internal/drift/diff_test.go
// Brief: Internal drift package implementation for 'diff'.

// diff_test.go ensures drift diffing covers the expected pod change scenarios.
package drift

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func snapshot(pods ...PodSnapshot) *Snapshot {
	return &Snapshot{Timestamp: time.Now(), Pods: pods}
}

func TestDiffSnapshots(t *testing.T) {
	prev := snapshot(PodSnapshot{
		Namespace:   "default",
		Name:        "web-1",
		Phase:       "Running",
		Node:        "node-a",
		RolloutHash: "abc",
		Conditions:  map[string]corev1.ConditionStatus{string(coreReady): corev1.ConditionTrue},
		Containers:  []ContainerSnapshot{{Name: "web", Ready: true, RestartCount: 1}},
		Owners:      []OwnerSnapshot{{Kind: "ReplicaSet", Name: "web-abc"}},
	})
	curr := snapshot(
		PodSnapshot{
			Namespace:   "default",
			Name:        "web-1",
			Phase:       "Running",
			Node:        "node-b",
			RolloutHash: "def",
			Conditions:  map[string]corev1.ConditionStatus{string(coreReady): corev1.ConditionFalse},
			Containers:  []ContainerSnapshot{{Name: "web", Ready: false, RestartCount: 3}},
			Owners:      []OwnerSnapshot{{Kind: "ReplicaSet", Name: "web-def"}},
		},
		PodSnapshot{Namespace: "default", Name: "web-2"},
	)
	diff := DiffSnapshots(prev, curr)
	if len(diff.Added) != 1 || diff.Added[0].Name != "web-2" {
		t.Fatalf("expected web-2 to be added: %+v", diff.Added)
	}
	if len(diff.Removed) != 0 {
		t.Fatalf("unexpected removed pods: %+v", diff.Removed)
	}
	if len(diff.Changed) != 1 {
		t.Fatalf("expected one changed pod, got %d", len(diff.Changed))
	}
	reasons := strings.Join(diff.Changed[0].Reasons, " ")
	for _, expect := range []string{"node", "rollout", "Ready", "restarts"} {
		if !strings.Contains(reasons, expect) {
			t.Fatalf("expected reason to contain %q, got %s", expect, reasons)
		}
	}
}
