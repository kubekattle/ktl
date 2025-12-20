// File: internal/capture/types.go
// Brief: Internal capture package implementation for 'types'.

// types.go declares the structs serialized inside capture archives (logs, metadata, and node state).
package capture

import (
	corev1 "k8s.io/api/core/v1"
	"time"
)

// Entry represents a single captured log line plus optional workload state.
type Entry struct {
	Timestamp          time.Time       `json:"timestamp"`
	FormattedTimestamp string          `json:"formattedTimestamp,omitempty"`
	Namespace          string          `json:"namespace"`
	Pod                string          `json:"pod"`
	Container          string          `json:"container"`
	Raw                string          `json:"raw"`
	Rendered           string          `json:"rendered"`
	PodState           *PodState       `json:"podState,omitempty"`
	NodeState          *NodeState      `json:"nodeState,omitempty"`
	Owners             []OwnerSnapshot `json:"owners,omitempty"`
}

// PodState summarizes the pod status at capture time.
type PodState struct {
	Phase      corev1.PodPhase                   `json:"phase"`
	NodeName   string                            `json:"nodeName,omitempty"`
	HostIP     string                            `json:"hostIP,omitempty"`
	PodIP      string                            `json:"podIP,omitempty"`
	Conditions map[string]corev1.ConditionStatus `json:"conditions,omitempty"`
	Containers []ContainerState                  `json:"containers,omitempty"`
}

// ContainerState summarizes a single container status snapshot.
type ContainerState struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state,omitempty"`
	LastState    string `json:"lastState,omitempty"`
}

// NodeState captures node pressure/allocatable info for the pod's node.
type NodeState struct {
	Name        string                            `json:"name"`
	Conditions  map[string]corev1.ConditionStatus `json:"conditions,omitempty"`
	Allocatable map[corev1.ResourceName]string    `json:"allocatable,omitempty"`
	Capacity    map[corev1.ResourceName]string    `json:"capacity,omitempty"`
}

// OwnerSnapshot holds owner reference details (ReplicaSets, Deployments, etc).
type OwnerSnapshot struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	UID      string `json:"uid"`
	Hash     string `json:"hash,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// Metadata describes a capture artifact.
type Metadata struct {
	SessionName      string    `json:"sessionName,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	EndedAt          time.Time `json:"endedAt"`
	DurationSeconds  float64   `json:"durationSeconds"`
	Namespaces       []string  `json:"namespaces"`
	AllNamespaces    bool      `json:"allNamespaces"`
	PodQuery         string    `json:"podQuery"`
	TailLines        int64     `json:"tailLines"`
	Since            string    `json:"since"`
	Context          string    `json:"context"`
	Kubeconfig       string    `json:"kubeconfig"`
	PodCount         int       `json:"observedPodCount"`
	EventsEnabled    bool      `json:"eventsEnabled"`
	Follow           bool      `json:"follow"`
	SQLitePath       string    `json:"sqlitePath,omitempty"`
	ManifestsEnabled bool      `json:"manifestsEnabled"`
}
