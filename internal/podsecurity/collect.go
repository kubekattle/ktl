// collect.go queries namespaces and builds the PodSecurity summary consumed by the renderer.
package podsecurity

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Options configures which namespaces the collector inspects.
type Options struct {
	Namespaces       []string
	AllNamespaces    bool
	DefaultNamespace string
}

// NamespaceSummary captures PodSecurity labels plus detected findings.
type NamespaceSummary struct {
	Namespace string
	Labels    LabelSet
	Findings  []Finding
}

// LabelSet holds the PodSecurity admission labels applied to a namespace.
type LabelSet struct {
	Enforce        string
	EnforceVersion string
	Audit          string
	AuditVersion   string
	Warn           string
	WarnVersion    string
}

// Finding represents a potential PodSecurity violation in a pod or container.
type Finding struct {
	Pod       string
	Container string
	Reason    string
	Level     string
	Action    string
}

// Collect inspects the requested namespaces and returns PodSecurity summaries.
func Collect(ctx context.Context, client kubernetes.Interface, opts Options) ([]NamespaceSummary, error) {
	namespaces, err := gatherNamespaces(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	summaries := make([]NamespaceSummary, 0, len(namespaces))
	for _, ns := range namespaces {
		summary := NamespaceSummary{
			Namespace: ns.Name,
			Labels:    extractLabels(&ns),
		}
		if err := populateFindings(ctx, client, &summary); err != nil {
			return nil, err
		}
		sort.Slice(summary.Findings, func(i, j int) bool {
			if summary.Findings[i].Pod == summary.Findings[j].Pod {
				return summary.Findings[i].Container < summary.Findings[j].Container
			}
			return summary.Findings[i].Pod < summary.Findings[j].Pod
		})
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Namespace < summaries[j].Namespace
	})
	return summaries, nil
}

func gatherNamespaces(ctx context.Context, client kubernetes.Interface, opts Options) ([]corev1.Namespace, error) {
	if opts.AllNamespaces {
		list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		return list.Items, nil
	}
	targets := opts.Namespaces
	if len(targets) == 0 {
		if opts.DefaultNamespace != "" {
			targets = []string{opts.DefaultNamespace}
		} else {
			targets = []string{"default"}
		}
	}
	var namespaces []corev1.Namespace
	for _, name := range targets {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ns, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("get namespace %s: %w", name, err)
		}
		namespaces = append(namespaces, *ns)
	}
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("no namespaces resolved")
	}
	return namespaces, nil
}

func extractLabels(ns *corev1.Namespace) LabelSet {
	labels := ns.GetLabels()
	return LabelSet{
		Enforce:        labels["pod-security.kubernetes.io/enforce"],
		EnforceVersion: labels["pod-security.kubernetes.io/enforce-version"],
		Audit:          labels["pod-security.kubernetes.io/audit"],
		AuditVersion:   labels["pod-security.kubernetes.io/audit-version"],
		Warn:           labels["pod-security.kubernetes.io/warn"],
		WarnVersion:    labels["pod-security.kubernetes.io/warn-version"],
	}
}

func populateFindings(ctx context.Context, client kubernetes.Interface, summary *NamespaceSummary) error {
	pods, err := client.CoreV1().Pods(summary.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pods for %s: %w", summary.Namespace, err)
	}
	for i := range pods.Items {
		pod := pods.Items[i]
		summary.Findings = append(summary.Findings, analyzePod(&pod, summary.Labels)...)
	}
	return nil
}

func analyzePod(pod *corev1.Pod, labels LabelSet) []Finding {
	var findings []Finding
	if pod.Spec.HostNetwork {
		findings = append(findings, newFinding(pod.Name, "", "uses hostNetwork", "baseline", labels))
	}
	if pod.Spec.HostPID {
		findings = append(findings, newFinding(pod.Name, "", "uses hostPID", "baseline", labels))
	}
	if pod.Spec.HostIPC {
		findings = append(findings, newFinding(pod.Name, "", "uses hostIPC", "baseline", labels))
	}
	for _, vol := range pod.Spec.Volumes {
		if vol.HostPath != nil {
			findings = append(findings, newFinding(pod.Name, "", fmt.Sprintf("mounts hostPath volume %s", vol.Name), "restricted", labels))
		}
	}
	containers := append([]corev1.Container{}, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, c := range containers {
		findings = append(findings, analyzeContainer(pod.Name, c, labels)...)
	}
	return findings
}

func analyzeContainer(podName string, container corev1.Container, labels LabelSet) []Finding {
	var findings []Finding
	sc := container.SecurityContext
	if sc == nil {
		return findings
	}
	if sc.Privileged != nil && *sc.Privileged {
		findings = append(findings, newFinding(podName, container.Name, "runs privileged", "baseline", labels))
	}
	if sc.AllowPrivilegeEscalation != nil && *sc.AllowPrivilegeEscalation {
		findings = append(findings, newFinding(podName, container.Name, "allowPrivilegeEscalation=true", "restricted", labels))
	}
	if sc.Capabilities != nil && len(sc.Capabilities.Add) > 0 {
		var caps []string
		for _, cap := range sc.Capabilities.Add {
			caps = append(caps, string(cap))
		}
		reason := fmt.Sprintf("adds capabilities [%s]", strings.Join(caps, ", "))
		findings = append(findings, newFinding(podName, container.Name, reason, "restricted", labels))
	}
	return findings
}

func newFinding(podName, containerName, reason, level string, labels LabelSet) Finding {
	return Finding{
		Pod:       podName,
		Container: containerName,
		Reason:    reason,
		Level:     strings.ToLower(level),
		Action:    determineAction(labels, level),
	}
}

func determineAction(labels LabelSet, violationLevel string) string {
	levelRank := policyRank(violationLevel)
	if levelRank < 0 {
		return "INFO"
	}
	if policyRank(labels.Enforce) >= levelRank {
		return "ENFORCE"
	}
	if policyRank(labels.Audit) >= levelRank {
		return "AUDIT"
	}
	if policyRank(labels.Warn) >= levelRank {
		return "WARN"
	}
	return "INFO"
}

func policyRank(level string) int {
	switch strings.ToLower(level) {
	case "privileged":
		return 0
	case "baseline":
		return 1
	case "restricted":
		return 2
	default:
		return -1
	}
}
