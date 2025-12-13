// pods.go renders pod-level CPU and memory usage tables for the 'ktl diag top' command.
package top

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// PodUsage summarizes CPU/memory usage for a pod.
type PodUsage struct {
	Namespace   string
	Pod         string
	CPUm        int64
	MemoryBytes int64
}

// PodOptions control how metrics are fetched.
type PodOptions struct {
	Namespaces    []string
	AllNamespaces bool
	LabelSelector string
	SortByCPU     bool
}

// ListPodUsage returns usage for pods matching the options.
func ListPodUsage(ctx context.Context, client metricsclient.Interface, opts PodOptions) ([]PodUsage, error) {
	if client == nil {
		return nil, fmt.Errorf("metrics client is not initialized")
	}
	nsList := resolveNamespaces(opts)
	results := make([]PodUsage, 0, 32)
	for _, ns := range nsList {
		list, err := client.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{
			LabelSelector: opts.LabelSelector,
		})
		if err != nil {
			return nil, fmt.Errorf("list pod metrics in namespace %q: %w", namespaceOrAll(ns), err)
		}
		for i := range list.Items {
			metric := list.Items[i]
			totalCPU := int64(0)
			totalMem := int64(0)
			for _, c := range metric.Containers {
				if cpu := c.Usage.Cpu(); cpu != nil {
					totalCPU += cpu.MilliValue()
				}
				if mem := c.Usage.Memory(); mem != nil {
					totalMem += mem.Value()
				}
			}
			results = append(results, PodUsage{
				Namespace:   metric.Namespace,
				Pod:         metric.Name,
				CPUm:        totalCPU,
				MemoryBytes: totalMem,
			})
		}
	}
	if opts.SortByCPU {
		sort.Slice(results, func(i, j int) bool {
			if results[i].CPUm == results[j].CPUm {
				if results[i].Namespace == results[j].Namespace {
					return results[i].Pod < results[j].Pod
				}
				return results[i].Namespace < results[j].Namespace
			}
			return results[i].CPUm > results[j].CPUm
		})
	} else {
		sort.Slice(results, func(i, j int) bool {
			if results[i].Namespace == results[j].Namespace {
				return results[i].Pod < results[j].Pod
			}
			return results[i].Namespace < results[j].Namespace
		})
	}
	return results, nil
}

func resolveNamespaces(opts PodOptions) []string {
	if opts.AllNamespaces || len(opts.Namespaces) == 0 {
		return []string{metav1.NamespaceAll}
	}
	seen := sets.New[string]()
	var namespaces []string
	for _, ns := range opts.Namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" || ns == metav1.NamespaceAll {
			continue
		}
		if seen.Has(ns) {
			continue
		}
		seen.Insert(ns)
		namespaces = append(namespaces, ns)
	}
	if len(namespaces) == 0 {
		return []string{metav1.NamespaceAll}
	}
	return namespaces
}

func namespaceOrAll(ns string) string {
	if ns == metav1.NamespaceAll {
		return "*"
	}
	return ns
}
