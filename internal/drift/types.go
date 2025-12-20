// File: internal/drift/types.go
// Brief: Internal drift package implementation for 'types'.

// types.go defines the structs used by the drift tracker when sampling pod deltas.
package drift

import (
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Snapshot captures workload metadata at a specific moment in time.
type Snapshot struct {
	Timestamp time.Time
	Pods      []PodSnapshot
}

// PodSnapshot summarizes pod/owner/container state for drift analysis.
type PodSnapshot struct {
	Namespace   string
	Name        string
	Node        string
	Phase       corev1.PodPhase
	Labels      map[string]string
	Conditions  map[string]corev1.ConditionStatus
	Containers  []ContainerSnapshot
	Owners      []OwnerSnapshot
	RolloutHash string
}

// ContainerSnapshot stores readiness + restart data for a container.
type ContainerSnapshot struct {
	Name         string
	Ready        bool
	RestartCount int32
	Image        string
	State        string
}

// OwnerSnapshot records the owner reference chain (ReplicaSet, Deployment, etc).
type OwnerSnapshot struct {
	Kind     string
	Name     string
	UID      string
	Hash     string
	Revision string
}

// TimelineBuffer retains the N most recent snapshots for quick diffing.
type TimelineBuffer struct {
	mu        sync.Mutex
	capacity  int
	snapshots []Snapshot
}

// NewTimelineBuffer creates a bounded snapshot buffer (default 20 entries).
func NewTimelineBuffer(capacity int) *TimelineBuffer {
	if capacity <= 0 {
		capacity = 20
	}
	return &TimelineBuffer{capacity: capacity}
}

// Add pushes a snapshot into the ring, trimming old entries.
func (b *TimelineBuffer) Add(s Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snapshots = append(b.snapshots, s)
	if len(b.snapshots) > b.capacity {
		trim := len(b.snapshots) - b.capacity
		b.snapshots = b.snapshots[trim:]
	}
}

// Snapshots returns a copy of the buffered snapshots.
func (b *TimelineBuffer) Snapshots() []Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Snapshot, len(b.snapshots))
	copy(out, b.snapshots)
	return out
}
