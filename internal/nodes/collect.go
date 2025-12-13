// collect.go queries Kubernetes for node stats, taints, and pressures that feed the nodes report.
package nodes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/example/ktl/internal/resourceutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options controls how node summaries are collected.
type Options struct {
	LabelSelector string
}

// Summary captures allocatable vs capacity metrics for a node.
type Summary struct {
	Name             string
	Schedulable      bool
	Ready            bool
	ReadyReason      string
	Taints           []corev1.Taint
	Notes            []string
	Capacity         ResourceSnapshot
	Allocatable      ResourceSnapshot
	Requested        ResourceSnapshot
	PodCapacity      int64
	PodAllocatable   int64
	PodCount         int
	PodPressureNotes []string
}

// ResourceSnapshot standardizes CPU/memory/ephemeral measurements.
type ResourceSnapshot struct {
	CPU       int64 // millicores
	Memory    int64 // bytes
	Ephemeral int64 // bytes
}

type usage struct {
	CPU       int64
	Memory    int64
	Ephemeral int64
	Pods      int
}

// Collect produces summaries for every node that matches the options.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) ([]Summary, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: opts.LabelSelector})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	pods, err := client.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	nodeUsage := aggregatePodUsage(pods)
	summaries := make([]Summary, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		summary := Summary{
			Name:        node.Name,
			Schedulable: !node.Spec.Unschedulable,
			Taints:      append([]corev1.Taint{}, node.Spec.Taints...),
			Capacity:    snapshotFromResourceList(node.Status.Capacity),
			Allocatable: snapshotFromResourceList(node.Status.Allocatable),
		}
		if qty, ok := node.Status.Capacity[corev1.ResourcePods]; ok {
			summary.PodCapacity = resourceutil.QuantityInt(qty)
		}
		if qty, ok := node.Status.Allocatable[corev1.ResourcePods]; ok {
			summary.PodAllocatable = resourceutil.QuantityInt(qty)
		}
		nodeUse := nodeUsage[node.Name]
		summary.Requested = ResourceSnapshot{
			CPU:       nodeUse.CPU,
			Memory:    nodeUse.Memory,
			Ephemeral: nodeUse.Ephemeral,
		}
		summary.PodCount = nodeUse.Pods

		summary.Ready, summary.ReadyReason = nodeReadyState(node.Status.Conditions)
		summary.Notes = append(summary.Notes, conditionNotes(node.Status.Conditions)...)
		if !summary.Schedulable {
			summary.Notes = append(summary.Notes, "cordoned")
		}
		for _, taint := range node.Spec.Taints {
			if taint.Effect == corev1.TaintEffectNoSchedule {
				summary.Notes = append(summary.Notes, fmt.Sprintf("taint %s=%s:NoSchedule", taint.Key, taint.Value))
			}
		}

		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func aggregatePodUsage(pods *corev1.PodList) map[string]usage {
	result := make(map[string]usage)
	for _, pod := range pods.Items {
		nodeName := pod.Spec.NodeName
		if nodeName == "" {
			continue
		}
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if pod.DeletionTimestamp != nil {
			continue
		}
		agg := result[nodeName]
		agg.Pods++
		agg.CPU += resourceutil.SumRequests(pod.Spec.Containers, corev1.ResourceCPU, resourceutil.QuantityMilli)
		agg.CPU += resourceutil.SumRequests(pod.Spec.InitContainers, corev1.ResourceCPU, resourceutil.QuantityMilli)
		agg.Memory += resourceutil.SumRequests(pod.Spec.Containers, corev1.ResourceMemory, resourceutil.QuantityInt)
		agg.Memory += resourceutil.SumRequests(pod.Spec.InitContainers, corev1.ResourceMemory, resourceutil.QuantityInt)
		agg.Ephemeral += resourceutil.SumRequests(pod.Spec.Containers, corev1.ResourceEphemeralStorage, resourceutil.QuantityInt)
		agg.Ephemeral += resourceutil.SumRequests(pod.Spec.InitContainers, corev1.ResourceEphemeralStorage, resourceutil.QuantityInt)
		result[nodeName] = agg
	}
	return result
}

func snapshotFromResourceList(list corev1.ResourceList) ResourceSnapshot {
	snap := ResourceSnapshot{}
	if qty, ok := list[corev1.ResourceCPU]; ok {
		snap.CPU = resourceutil.QuantityMilli(qty)
	}
	if qty, ok := list[corev1.ResourceMemory]; ok {
		snap.Memory = resourceutil.QuantityInt(qty)
	}
	if qty, ok := list[corev1.ResourceEphemeralStorage]; ok {
		snap.Ephemeral = resourceutil.QuantityInt(qty)
	}
	return snap
}

func nodeReadyState(conditions []corev1.NodeCondition) (bool, string) {
	for _, cond := range conditions {
		if cond.Type == corev1.NodeReady {
			if cond.Status == corev1.ConditionTrue {
				return true, cond.Message
			}
			return false, cond.Message
		}
	}
	return false, "Ready condition not reported"
}

func conditionNotes(conditions []corev1.NodeCondition) []string {
	var notes []string
	for _, cond := range conditions {
		if cond.Status != corev1.ConditionTrue {
			continue
		}
		switch cond.Type {
		case corev1.NodeDiskPressure:
			notes = append(notes, "disk pressure")
		case corev1.NodeMemoryPressure:
			notes = append(notes, "memory pressure")
		case corev1.NodePIDPressure:
			notes = append(notes, "pid pressure")
		case corev1.NodeNetworkUnavailable:
			notes = append(notes, "network unavailable")
		}
	}
	return notes
}

// FormatNotes collapses duplicate note strings.
func (s *Summary) FormatNotes() string {
	if len(s.Notes) == 0 {
		return ""
	}
	unique := make(map[string]struct{})
	var ordered []string
	for _, note := range s.Notes {
		n := strings.TrimSpace(note)
		if n == "" {
			continue
		}
		if _, exists := unique[n]; exists {
			continue
		}
		unique[n] = struct{}{}
		ordered = append(ordered, n)
	}
	return strings.Join(ordered, ", ")
}
