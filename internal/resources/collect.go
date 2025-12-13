// collect.go walks workloads per namespace and builds the resource summary consumed by the renderer.
package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/example/ktl/internal/resourceutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Options selects which namespaces and pods are inspected.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
	Top              int
}

// ContainerUsage holds requests/limits/usage values for a container.
type ContainerUsage struct {
	Namespace     string
	Pod           string
	Container     string
	Phase         corev1.PodPhase
	RequestCPU    int64
	LimitCPU      int64
	UsageCPU      int64
	RequestMemory int64
	LimitMemory   int64
	UsageMemory   int64
	NodeName      string
}

// Summary aggregates the per-container usage and notes whether usage metrics were available.
type Summary struct {
	Containers     []ContainerUsage
	MetricsEnabled bool
	MetricsError   string
}

// Collect gathers pod specs and metrics (if available) to produce usage summaries.
func Collect(ctx context.Context, client kubernetes.Interface, metrics metricsclient.Interface, opts Options) (*Summary, error) {
	namespaces, err := resolveNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	pods, err := listPods(ctx, client, namespaces)
	if err != nil {
		return nil, err
	}

	usageMap := map[string]usageSample{}
	summary := &Summary{MetricsEnabled: metrics != nil}

	if metrics != nil {
		if err := populateUsage(ctx, metrics, namespaces, usageMap); err != nil {
			summary.MetricsError = err.Error()
			summary.MetricsEnabled = false
		}
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			key := usageKey(pod.Namespace, pod.Name, container.Name)
			sample := usageMap[key]
			entry := ContainerUsage{
				Namespace:     pod.Namespace,
				Pod:           pod.Name,
				Container:     container.Name,
				Phase:         pod.Status.Phase,
				NodeName:      pod.Spec.NodeName,
				RequestCPU:    resourceutil.FromResourceList(container.Resources.Requests, corev1.ResourceCPU, resourceutil.QuantityMilli),
				LimitCPU:      resourceutil.FromResourceList(container.Resources.Limits, corev1.ResourceCPU, resourceutil.QuantityMilli),
				RequestMemory: resourceutil.FromResourceList(container.Resources.Requests, corev1.ResourceMemory, resourceutil.QuantityInt),
				LimitMemory:   resourceutil.FromResourceList(container.Resources.Limits, corev1.ResourceMemory, resourceutil.QuantityInt),
				UsageCPU:      sample.CPU,
				UsageMemory:   sample.Memory,
			}
			summary.Containers = append(summary.Containers, entry)
		}
	}

	sort.Slice(summary.Containers, func(i, j int) bool {
		if summary.Containers[i].UsageMemory == summary.Containers[j].UsageMemory {
			if summary.Containers[i].Namespace == summary.Containers[j].Namespace {
				if summary.Containers[i].Pod == summary.Containers[j].Pod {
					return summary.Containers[i].Container < summary.Containers[j].Container
				}
				return summary.Containers[i].Pod < summary.Containers[j].Pod
			}
			return summary.Containers[i].Namespace < summary.Containers[j].Namespace
		}
		return summary.Containers[i].UsageMemory > summary.Containers[j].UsageMemory
	})

	if opts.Top > 0 && len(summary.Containers) > opts.Top {
		summary.Containers = summary.Containers[:opts.Top]
	}

	return summary, nil
}

type usageSample struct {
	CPU    int64
	Memory int64
}

func populateUsage(ctx context.Context, metrics metricsclient.Interface, namespaces []string, dest map[string]usageSample) error {
	for _, ns := range namespaces {
		list, err := metrics.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list pod metrics in %s: %w", ns, err)
		}
		for _, metric := range list.Items {
			for _, container := range metric.Containers {
				key := usageKey(metric.Namespace, metric.Name, container.Name)
				cpu := resourceutil.QuantityMilli(container.Usage[corev1.ResourceCPU])
				mem := resourceutil.QuantityInt(container.Usage[corev1.ResourceMemory])
				dest[key] = usageSample{CPU: cpu, Memory: mem}
			}
		}
	}
	return nil
}

func usageKey(ns, pod, container string) string {
	return fmt.Sprintf("%s/%s/%s", ns, pod, container)
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

func listPods(ctx context.Context, client kubernetes.Interface, namespaces []string) ([]corev1.Pod, error) {
	var pods []corev1.Pod
	for _, ns := range namespaces {
		list, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list pods in %s: %w", ns, err)
		}
		pods = append(pods, list.Items...)
	}
	return pods, nil
}
