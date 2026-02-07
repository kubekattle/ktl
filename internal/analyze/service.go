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
	Pod      *corev1.Pod
	Events   []corev1.Event
	Logs     map[string]string // ContainerName -> Last N lines of logs
	Manifest string            // YAML representation (optional)
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

	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
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

	return &Evidence{
		Pod:    pod,
		Events: events.Items,
		Logs:   logs,
	}, nil
}

func int64Ptr(i int64) *int64 { return &i }
