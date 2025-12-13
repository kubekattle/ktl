// collect.go correlates PVCs, pods, and node pressures to feed the storage diagnostics.
package pvcstatus

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options controls namespace selection.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
}

// Summary describes a PVC and any node pressure affecting pods that mount it.
type Summary struct {
	Namespace    string
	Name         string
	Phase        corev1.PersistentVolumeClaimPhase
	StorageClass string
	VolumeMode   string
	AccessModes  []corev1.PersistentVolumeAccessMode
	Capacity     string
	BoundVolume  string
	Pods         []PodUsage
	Notes        []string
}

// PodUsage ties a PVC to pods/nodes.
type PodUsage struct {
	PodName  string
	NodeName string
}

// Collect PVC summaries and correlate them with node pressure conditions.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) ([]Summary, error) {
	namespaces, err := resolveNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	nodePressures, err := fetchNodePressures(ctx, client)
	if err != nil {
		return nil, err
	}
	pvcUsers, err := mapPVCToPods(ctx, client, namespaces)
	if err != nil {
		return nil, err
	}

	var summaries []Summary
	for _, ns := range namespaces {
		pvcList, err := client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list pvcs in %s: %w", ns, err)
		}
		for _, pvc := range pvcList.Items {
			summary := summarizePVC(pvc, pvcUsers[key(ns, pvc.Name)], nodePressures)
			summaries = append(summaries, summary)
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Namespace == summaries[j].Namespace {
			return summaries[i].Name < summaries[j].Name
		}
		return summaries[i].Namespace < summaries[j].Namespace
	})
	return summaries, nil
}

func resolveNamespaces(ctx context.Context, client kubernetes.Interface, opts Options) ([]string, error) {
	if opts.AllNamespaces {
		list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		names := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		return names, nil
	}
	if len(opts.Namespaces) > 0 {
		var names []string
		for _, ns := range opts.Namespaces {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				names = append(names, ns)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no namespaces specified")
		}
		return names, nil
	}
	if opts.DefaultNamespace != "" {
		return []string{opts.DefaultNamespace}, nil
	}
	return []string{"default"}, nil
}

func fetchNodePressures(ctx context.Context, client kubernetes.Interface) (map[string][]string, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	result := make(map[string][]string)
	for _, node := range nodes.Items {
		var pressures []string
		for _, cond := range node.Status.Conditions {
			if cond.Status == corev1.ConditionTrue {
				switch cond.Type {
				case corev1.NodeMemoryPressure:
					pressures = append(pressures, "MemoryPressure")
				case corev1.NodeDiskPressure:
					pressures = append(pressures, "DiskPressure")
				case corev1.NodePIDPressure:
					pressures = append(pressures, "PIDPressure")
				case corev1.NodeNetworkUnavailable:
					pressures = append(pressures, "NetworkUnavailable")
				}
			}
		}
		result[node.Name] = pressures
	}
	return result, nil
}

func mapPVCToPods(ctx context.Context, client kubernetes.Interface, namespaces []string) (map[string][]PodUsage, error) {
	result := make(map[string][]PodUsage)
	for _, ns := range namespaces {
		pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list pods in %s: %w", ns, err)
		}
		for _, pod := range pods.Items {
			for _, vol := range pod.Spec.Volumes {
				if vol.PersistentVolumeClaim == nil {
					continue
				}
				claim := vol.PersistentVolumeClaim.ClaimName
				if claim == "" {
					continue
				}
				k := key(ns, claim)
				result[k] = append(result[k], PodUsage{
					PodName:  pod.Name,
					NodeName: pod.Spec.NodeName,
				})
			}
		}
	}
	return result, nil
}

func summarizePVC(pvc corev1.PersistentVolumeClaim, users []PodUsage, nodePressures map[string][]string) Summary {
	storageClass := ""
	if pvc.Spec.StorageClassName != nil {
		storageClass = *pvc.Spec.StorageClassName
	}
	volMode := ""
	if pvc.Spec.VolumeMode != nil {
		volMode = string(*pvc.Spec.VolumeMode)
	}
	capacity := ""
	if qty, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
		capacity = qty.String()
	}
	var notes []string
	if pvc.Status.Phase != corev1.ClaimBound {
		notes = append(notes, fmt.Sprintf("PVC phase: %s", pvc.Status.Phase))
	}
	var pressureNotes []string
	for _, usage := range users {
		if usage.NodeName == "" {
			continue
		}
		pressures := nodePressures[usage.NodeName]
		if len(pressures) > 0 {
			pressureNotes = append(pressureNotes, fmt.Sprintf("%s: %s", usage.NodeName, strings.Join(pressures, "/")))
		}
	}
	if len(pressureNotes) > 0 {
		notes = append(notes, pressureNotes...)
	}
	return Summary{
		Namespace:    pvc.Namespace,
		Name:         pvc.Name,
		Phase:        pvc.Status.Phase,
		StorageClass: storageClass,
		VolumeMode:   volMode,
		AccessModes:  append([]corev1.PersistentVolumeAccessMode{}, pvc.Spec.AccessModes...),
		Capacity:     capacity,
		BoundVolume:  pvc.Spec.VolumeName,
		Pods:         users,
		Notes:        notes,
	}
}

func key(ns, name string) string {
	return ns + "/" + name
}
