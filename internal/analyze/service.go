package analyze

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Evidence collects all relevant data for analysis
type Evidence struct {
	Pod             *corev1.Pod
	Node            *corev1.Node      // Details of the node running the pod
	Events          []corev1.Event    // Events related to the pod
	NamespaceEvents []corev1.Event    // Recent events in the namespace (for context)
	Logs            map[string]string // ContainerName -> Last N lines of logs
	SourceSnippets  []Snippet         // Code snippets linked from stack traces
	Manifest        string            // YAML representation (optional)
}

// Diagnosis represents the result of an analysis
type Diagnosis struct {
	RootCause       string
	Suggestion      string
	Explanation     string
	ConfidenceScore float64 // 0.0 to 1.0
	Patch           string  // Optional: Patch to apply
}

type Analyzer interface {
	Analyze(ctx context.Context, evidence *Evidence) (*Diagnosis, error)
}

// GatherEvidence collects data from the cluster
func GatherEvidence(ctx context.Context, client kubernetes.Interface, namespace, podName string) (*Evidence, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	// 1. Get Pod Events
	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// 2. Get Node Info (if scheduled)
	var node *corev1.Node
	if pod.Spec.NodeName != "" {
		n, err := client.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
		if err == nil {
			node = n
		}
	}

	// 3. Get Recent Namespace Events (Contextual Clues)
	// We grab the last 20 warning events in the namespace to see if there's a wider issue
	// (e.g. Quota exceeded, PVC binding issues unrelated to this specific pod object ref)
	nsEventsList, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		Limit: 20,
	})
	var nsEvents []corev1.Event
	if err == nil {
		// Simple filter for Warnings or recent stuff could be done here.
		// For now, we just pass them all, and let the analyzer filter.
		nsEvents = nsEventsList.Items
	}

	logs := make(map[string]string)
	// Tail logs for all containers (init and regular)
	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, c := range allContainers {
		// Only fetch logs if container has started or tried to start
		// (Simplification: fetch tail 50 lines)
		opts := &corev1.PodLogOptions{
			Container: c.Name,
			TailLines: int64Ptr(50),
		}
		req := client.CoreV1().Pods(namespace).GetLogs(podName, opts)
		podLogs, err := req.DoRaw(ctx)
		if err == nil {
			logs[c.Name] = string(podLogs)
		} else {
			logs[c.Name] = fmt.Sprintf("<failed to fetch logs: %v>", err)
		}
	}

	// 4. Source Code Correlation (The Magic)
	// We scan the collected logs for stack traces and try to find matching code in the CWD
	var snippets []Snippet
	for _, log := range logs {
		s := FindSourceSnippets(log)
		snippets = append(snippets, s...)
	}

	return &Evidence{
		Pod:             pod,
		Node:            node,
		Events:          events.Items,
		NamespaceEvents: nsEvents,
		Logs:            logs,
		SourceSnippets:  snippets,
	}, nil
}

func int64Ptr(i int64) *int64 { return &i }
