// collect.go inspects PriorityClasses and their consumers for the priorities diagnostic.
package priorities

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options controls which namespaces are considered when scanning pods.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
}

// Summary contains PriorityClass information plus pod priority usage.
type Summary struct {
	Classes []PriorityClassSummary
	Pods    []PodPriority
}

// PriorityClassSummary describes a single PriorityClass.
type PriorityClassSummary struct {
	Name             string
	Value            int32
	GlobalDefault    bool
	Description      string
	PreemptionPolicy string
}

// PodPriority captures the priority metadata for a pod.
type PodPriority struct {
	Namespace        string
	Name             string
	Priority         int32
	PriorityClass    string
	Phase            corev1.PodPhase
	PreemptionPolicy string
	NominatedNode    string
	Deleting         bool
	Reason           string
	Message          string
	DisruptionNote   string
}

// Collect retrieves cluster priority class information alongside pod usage.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) (*Summary, error) {
	classList, err := client.SchedulingV1().PriorityClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list PriorityClasses: %w", err)
	}
	namespaces, err := resolveNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	pods, err := listPods(ctx, client, namespaces)
	if err != nil {
		return nil, err
	}
	summary := &Summary{
		Classes: buildClassSummaries(classList.Items),
		Pods:    buildPodSummaries(pods),
	}
	return summary, nil
}

func buildClassSummaries(items []schedulingv1.PriorityClass) []PriorityClassSummary {
	summaries := make([]PriorityClassSummary, 0, len(items))
	for _, item := range items {
		pps := ""
		if item.PreemptionPolicy != nil {
			pps = string(*item.PreemptionPolicy)
		}
		summaries = append(summaries, PriorityClassSummary{
			Name:             item.Name,
			Value:            item.Value,
			GlobalDefault:    item.GlobalDefault,
			Description:      item.Description,
			PreemptionPolicy: pps,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Value == summaries[j].Value {
			return summaries[i].Name < summaries[j].Name
		}
		return summaries[i].Value > summaries[j].Value
	})
	return summaries
}

func buildPodSummaries(items []corev1.Pod) []PodPriority {
	summaries := make([]PodPriority, 0, len(items))
	for _, pod := range items {
		priority := int32(0)
		if pod.Spec.Priority != nil {
			priority = *pod.Spec.Priority
		}
		policy := ""
		if pod.Spec.PreemptionPolicy != nil {
			policy = string(*pod.Spec.PreemptionPolicy)
		}
		s := PodPriority{
			Namespace:        pod.Namespace,
			Name:             pod.Name,
			Priority:         priority,
			PriorityClass:    pod.Spec.PriorityClassName,
			Phase:            pod.Status.Phase,
			PreemptionPolicy: policy,
			NominatedNode:    pod.Status.NominatedNodeName,
			Deleting:         pod.DeletionTimestamp != nil,
			Reason:           pod.Status.Reason,
			Message:          truncate(pod.Status.Message, 120),
			DisruptionNote:   disruptionMessage(pod.Status.Conditions),
		}
		summaries = append(summaries, s)
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Priority == summaries[j].Priority {
			if summaries[i].Namespace == summaries[j].Namespace {
				return summaries[i].Name < summaries[j].Name
			}
			return summaries[i].Namespace < summaries[j].Namespace
		}
		return summaries[i].Priority > summaries[j].Priority
	})
	return summaries
}

func disruptionMessage(conditions []corev1.PodCondition) string {
	for _, cond := range conditions {
		if cond.Type == corev1.DisruptionTarget && cond.Status == corev1.ConditionTrue {
			if cond.Message != "" {
				return cond.Message
			}
			if cond.Reason != "" {
				return cond.Reason
			}
			return "marked for disruption"
		}
	}
	return ""
}

func truncate(msg string, max int) string {
	if len(msg) <= max {
		return msg
	}
	return msg[:max] + "…"
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
