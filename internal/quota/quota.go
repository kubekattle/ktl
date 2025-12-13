// quota.go analyzes ResourceQuota/LimitRange data sets for 'ktl diag quotas'.
package quota

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/example/ktl/internal/resourceutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options controls which namespaces are inspected for quota usage.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
}

// Summary holds the quota usage numbers for a namespace.
type Summary struct {
	Namespace   string
	Pods        Metric
	CPU         Metric
	Memory      Metric
	PVCs        Metric
	LimitRanges []LimitRangeDetail
}

// Metric captures used and limited values for a resource.
type Metric struct {
	Used     int64
	Limit    int64
	HasLimit bool
}

// LimitRangeDetail provides a concise description of a namespace limit range.
type LimitRangeDetail struct {
	Name               string
	Type               corev1.LimitType
	DefaultRequestCPU  string
	DefaultRequestMem  string
	DefaultLimitCPU    string
	DefaultLimitMem    string
	MinCPU             string
	MaxCPU             string
	MinMemory          string
	MaxMemory          string
	DefaultRequestEphe string
	DefaultLimitEphe   string
}

// String returns a human readable description of the limit range detail.
func (l LimitRangeDetail) String() string {
	var parts []string
	if l.DefaultRequestCPU != "" || l.DefaultLimitCPU != "" {
		parts = append(parts, fmt.Sprintf("cpu req/limit=%s/%s", dashIfEmpty(l.DefaultRequestCPU), dashIfEmpty(l.DefaultLimitCPU)))
	}
	if l.DefaultRequestMem != "" || l.DefaultLimitMem != "" {
		parts = append(parts, fmt.Sprintf("mem req/limit=%s/%s", dashIfEmpty(l.DefaultRequestMem), dashIfEmpty(l.DefaultLimitMem)))
	}
	if l.DefaultRequestEphe != "" || l.DefaultLimitEphe != "" {
		parts = append(parts, fmt.Sprintf("ephemeral req/limit=%s/%s", dashIfEmpty(l.DefaultRequestEphe), dashIfEmpty(l.DefaultLimitEphe)))
	}
	if l.MinCPU != "" || l.MaxCPU != "" {
		parts = append(parts, fmt.Sprintf("cpu min/max=%s/%s", dashIfEmpty(l.MinCPU), dashIfEmpty(l.MaxCPU)))
	}
	if l.MinMemory != "" || l.MaxMemory != "" {
		parts = append(parts, fmt.Sprintf("mem min/max=%s/%s", dashIfEmpty(l.MinMemory), dashIfEmpty(l.MaxMemory)))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s (%s)", l.Name, l.Type)
	}
	return fmt.Sprintf("%s (%s): %s", l.Name, l.Type, strings.Join(parts, ", "))
}

func dashIfEmpty(val string) string {
	if val == "" {
		return "-"
	}
	return val
}

// Collect walks the requested namespaces and produces quota summaries.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) ([]Summary, error) {
	namespaces, err := resolveNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	summaries := make([]Summary, 0, len(namespaces))
	for _, ns := range namespaces {
		summary := Summary{Namespace: ns}
		if err := populateNamespace(ctx, client, &summary); err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Namespace < summaries[j].Namespace
	})
	return summaries, nil
}

func resolveNamespaces(ctx context.Context, client kubernetes.Interface, opts Options) ([]string, error) {
	var namespaces []string
	if opts.AllNamespaces {
		nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	} else if len(opts.Namespaces) > 0 {
		for _, ns := range opts.Namespaces {
			trimmed := strings.TrimSpace(ns)
			if trimmed != "" {
				namespaces = append(namespaces, trimmed)
			}
		}
	} else if opts.DefaultNamespace != "" {
		namespaces = append(namespaces, opts.DefaultNamespace)
	} else {
		namespaces = append(namespaces, "default")
	}
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("no namespaces resolved")
	}
	return namespaces, nil
}

func populateNamespace(ctx context.Context, client kubernetes.Interface, summary *Summary) error {
	if err := loadResourceQuotas(ctx, client, summary); err != nil {
		return err
	}
	if err := loadLimitRanges(ctx, client, summary); err != nil {
		return err
	}
	if err := loadActualUsage(ctx, client, summary); err != nil {
		return err
	}
	return nil
}

func loadResourceQuotas(ctx context.Context, client kubernetes.Interface, summary *Summary) error {
	quotas, err := client.CoreV1().ResourceQuotas(summary.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list resource quotas for %s: %w", summary.Namespace, err)
	}
	for _, rq := range quotas.Items {
		applyQuota(&summary.Pods, rq.Status.Used, rq.Status.Hard, corev1.ResourcePods, resourceutil.QuantityInt)
		applyQuota(&summary.CPU, rq.Status.Used, rq.Status.Hard, corev1.ResourceRequestsCPU, resourceutil.QuantityMilli)
		applyQuota(&summary.Memory, rq.Status.Used, rq.Status.Hard, corev1.ResourceRequestsMemory, resourceutil.QuantityInt)
		applyQuota(&summary.PVCs, rq.Status.Used, rq.Status.Hard, corev1.ResourcePersistentVolumeClaims, resourceutil.QuantityInt)
	}
	return nil
}

func loadLimitRanges(ctx context.Context, client kubernetes.Interface, summary *Summary) error {
	list, err := client.CoreV1().LimitRanges(summary.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list limit ranges for %s: %w", summary.Namespace, err)
	}
	for _, lr := range list.Items {
		for _, item := range lr.Spec.Limits {
			detail := LimitRangeDetail{
				Name: lr.Name,
				Type: item.Type,
			}
			if val, ok := item.DefaultRequest[corev1.ResourceCPU]; ok {
				detail.DefaultRequestCPU = val.String()
			}
			if val, ok := item.DefaultRequest[corev1.ResourceMemory]; ok {
				detail.DefaultRequestMem = val.String()
			}
			if val, ok := item.DefaultRequest[corev1.ResourceEphemeralStorage]; ok {
				detail.DefaultRequestEphe = val.String()
			}
			if val, ok := item.Default[corev1.ResourceCPU]; ok {
				detail.DefaultLimitCPU = val.String()
			}
			if val, ok := item.Default[corev1.ResourceMemory]; ok {
				detail.DefaultLimitMem = val.String()
			}
			if val, ok := item.Default[corev1.ResourceEphemeralStorage]; ok {
				detail.DefaultLimitEphe = val.String()
			}
			if val, ok := item.Min[corev1.ResourceCPU]; ok {
				detail.MinCPU = val.String()
			}
			if val, ok := item.Max[corev1.ResourceCPU]; ok {
				detail.MaxCPU = val.String()
			}
			if val, ok := item.Min[corev1.ResourceMemory]; ok {
				detail.MinMemory = val.String()
			}
			if val, ok := item.Max[corev1.ResourceMemory]; ok {
				detail.MaxMemory = val.String()
			}
			summary.LimitRanges = append(summary.LimitRanges, detail)
		}
	}
	return nil
}

func loadActualUsage(ctx context.Context, client kubernetes.Interface, summary *Summary) error {
	pods, err := client.CoreV1().Pods(summary.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pods for %s: %w", summary.Namespace, err)
	}
	pvcs, err := client.CoreV1().PersistentVolumeClaims(summary.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pvcs for %s: %w", summary.Namespace, err)
	}
	actual := computeActualUsage(pods, pvcs)
	if !summary.Pods.HasLimit {
		summary.Pods.Used = actual.Pods
	}
	if !summary.CPU.HasLimit {
		summary.CPU.Used = actual.CPU
	}
	if !summary.Memory.HasLimit {
		summary.Memory.Used = actual.Memory
	}
	if !summary.PVCs.HasLimit {
		summary.PVCs.Used = actual.PVCs
	}
	return nil
}

type actualUsage struct {
	Pods   int64
	PVCs   int64
	CPU    int64
	Memory int64
}

func computeActualUsage(pods *corev1.PodList, pvcs *corev1.PersistentVolumeClaimList) actualUsage {
	var usage actualUsage
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		usage.Pods++
		usage.CPU += resourceutil.SumRequests(pod.Spec.Containers, corev1.ResourceCPU, resourceutil.QuantityMilli)
		usage.CPU += resourceutil.SumRequests(pod.Spec.InitContainers, corev1.ResourceCPU, resourceutil.QuantityMilli)
		usage.Memory += resourceutil.SumRequests(pod.Spec.Containers, corev1.ResourceMemory, resourceutil.QuantityInt)
		usage.Memory += resourceutil.SumRequests(pod.Spec.InitContainers, corev1.ResourceMemory, resourceutil.QuantityInt)
	}
	usage.PVCs = int64(len(pvcs.Items))
	return usage
}

func applyQuota(metric *Metric, used corev1.ResourceList, hard corev1.ResourceList, name corev1.ResourceName, conv func(resource.Quantity) int64) {
	if metric == nil {
		return
	}
	if quantity, ok := used[name]; ok {
		metric.Used += conv(quantity)
	}
	if quantity, ok := hard[name]; ok {
		metric.Limit += conv(quantity)
		metric.HasLimit = true
	}
}
