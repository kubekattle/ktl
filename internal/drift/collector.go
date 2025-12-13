// collector.go captures pod state over time for 'ktl logs drift watch'.
package drift

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Collector snapshots workload state and feeds it into a timeline buffer.
type Collector struct {
	client     kubernetes.Interface
	namespaces []string
	buffer     *TimelineBuffer
	interval   time.Duration
}

// NewCollector builds a collector targeting specific namespaces.
func NewCollector(client kubernetes.Interface, namespaces []string, capacity int) *Collector {
	if len(namespaces) == 0 {
		namespaces = []string{metav1.NamespaceDefault}
	}
	return &Collector{
		client:     client,
		namespaces: namespaces,
		buffer:     NewTimelineBuffer(capacity),
		interval:   30 * time.Second,
	}
}

// Buffer returns the backing timeline buffer.
func (c *Collector) Buffer() *TimelineBuffer {
	return c.buffer
}

// SetInterval overrides the default sampling interval.
func (c *Collector) SetInterval(d time.Duration) {
	if d > 0 {
		c.interval = d
	}
}

// Run starts a periodic snapshot loop until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		if _, err := c.Snapshot(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Snapshot grabs a fresh snapshot and appends it to the timeline buffer.
func (c *Collector) Snapshot(ctx context.Context) (Snapshot, error) {
	pods, err := c.collectPods(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Timestamp: time.Now().UTC(), Pods: pods}
	c.buffer.Add(s)
	return s, nil
}

func (c *Collector) collectPods(ctx context.Context) ([]PodSnapshot, error) {
	var snapshots []PodSnapshot
	for _, ns := range c.namespaces {
		list, err := c.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list pods in %s: %w", ns, err)
		}
		for i := range list.Items {
			pod := list.Items[i]
			ps := PodSnapshot{
				Namespace:   pod.Namespace,
				Name:        pod.Name,
				Node:        pod.Spec.NodeName,
				Phase:       pod.Status.Phase,
				Labels:      copyLabels(pod.Labels),
				Conditions:  map[string]corev1.ConditionStatus{},
				RolloutHash: pod.Labels["pod-template-hash"],
			}
			for _, cond := range pod.Status.Conditions {
				ps.Conditions[string(cond.Type)] = cond.Status
			}
			for _, cs := range pod.Status.ContainerStatuses {
				ps.Containers = append(ps.Containers, ContainerSnapshot{
					Name:         cs.Name,
					Ready:        cs.Ready,
					RestartCount: cs.RestartCount,
					Image:        cs.Image,
					State:        describeState(cs.State),
				})
			}
			ps.Owners = c.buildOwnerChain(ctx, &pod)
			if ps.RolloutHash == "" {
				ps.RolloutHash = firstHash(ps.Owners)
			}
			snapshots = append(snapshots, ps)
		}
	}
	return snapshots, nil
}

func (c *Collector) buildOwnerChain(ctx context.Context, pod *corev1.Pod) []OwnerSnapshot {
	if pod == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var chain []OwnerSnapshot
	for _, ref := range pod.OwnerReferences {
		c.appendOwner(ctx, pod.Namespace, ref, &chain, seen)
	}
	return chain
}

func (c *Collector) appendOwner(ctx context.Context, namespace string, ref metav1.OwnerReference, chain *[]OwnerSnapshot, seen map[string]struct{}) {
	key := fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	owner := OwnerSnapshot{Kind: ref.Kind, Name: ref.Name, UID: string(ref.UID)}
	switch ref.Kind {
	case "ReplicaSet":
		rs, err := c.client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err == nil {
			owner.Hash = rs.Labels["pod-template-hash"]
			owner.Revision = rs.Annotations["deployment.kubernetes.io/revision"]
			*chain = append(*chain, owner)
			for _, parent := range rs.OwnerReferences {
				c.appendOwner(ctx, namespace, parent, chain, seen)
			}
			return
		}
	case "Deployment":
		deploy, err := c.client.AppsV1().Deployments(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err == nil {
			owner.Revision = deploy.Annotations["deployment.kubernetes.io/revision"]
		}
	}
	*chain = append(*chain, owner)
}

func copyLabels(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func firstHash(owners []OwnerSnapshot) string {
	for _, owner := range owners {
		if owner.Hash != "" {
			return owner.Hash
		}
	}
	return ""
}

func describeState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return "running"
	case state.Waiting != nil:
		return "waiting"
	case state.Terminated != nil:
		return "terminated"
	default:
		return "unknown"
	}
}
