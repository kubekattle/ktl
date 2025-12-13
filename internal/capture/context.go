// context.go provides context helpers used to control capture lifecycles.
package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/sqlitewriter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Session) enrichArtifacts(ctx context.Context) {
	if s.sqlitePath == "" && !s.options.AttachDescribe {
		return
	}
	snapshot, pods, events, err := s.buildContextSnapshot(ctx)
	if err != nil {
		s.log.V(1).Error(err, "build capture context snapshot")
		return
	}
	if s.sqlitePath != "" && snapshot != nil {
		if err := sqlitewriter.WriteContext(s.sqlitePath, *snapshot); err != nil {
			s.log.V(1).Error(err, "write sqlite context")
		}
	}
	if s.options.AttachDescribe {
		if err := s.writeDescribeAttachments(ctx, pods, events); err != nil {
			s.log.V(1).Error(err, "write describe attachments")
		}
	}
}

func (s *Session) buildContextSnapshot(ctx context.Context) (*sqlitewriter.ContextSnapshot, map[string]*corev1.Pod, map[string][]corev1.Event, error) {
	nsPods := s.observedPodsByNamespace()
	if len(nsPods) == 0 {
		return &sqlitewriter.ContextSnapshot{}, map[string]*corev1.Pod{}, map[string][]corev1.Event{}, nil
	}
	pods := make(map[string]*corev1.Pod)
	eventsByPod := make(map[string][]corev1.Event)
	configMapRefs := make(map[string]map[string]struct{})

	for ns, podNames := range nsPods {
		for _, name := range podNames {
			pod, err := s.client.Clientset.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			key := fmt.Sprintf("%s/%s", ns, name)
			pods[key] = pod
			for _, cfgName := range extractConfigMaps(pod) {
				if _, ok := configMapRefs[pod.Namespace]; !ok {
					configMapRefs[pod.Namespace] = make(map[string]struct{})
				}
				configMapRefs[pod.Namespace][cfgName] = struct{}{}
			}
		}
	}

	eventRows := []sqlitewriter.EventRow{}
	for ns := range nsPods {
		list, err := s.client.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for _, evt := range list.Items {
			key := fmt.Sprintf("%s/%s", evt.InvolvedObject.Namespace, evt.InvolvedObject.Name)
			if _, ok := pods[key]; !ok {
				continue
			}
			eventsByPod[key] = append(eventsByPod[key], evt)
			eventRows = append(eventRows, sqlitewriter.EventRow{
				Namespace:         evt.Namespace,
				Name:              evt.Name,
				Type:              evt.Type,
				Reason:            evt.Reason,
				Message:           evt.Message,
				InvolvedKind:      evt.InvolvedObject.Kind,
				InvolvedName:      evt.InvolvedObject.Name,
				InvolvedNamespace: evt.InvolvedObject.Namespace,
				Count:             int(evt.Count),
				FirstTimestamp:    evt.EventTime.Time.UTC(),
				LastTimestamp:     evt.LastTimestamp.Time.UTC(),
			})
		}
	}

	deployRows := []sqlitewriter.DeploymentRow{}
	for ns := range nsPods {
		list, err := s.client.Clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for _, dep := range list.Items {
			deployRows = append(deployRows, toDeploymentRow(dep))
		}
	}

	configRows := []sqlitewriter.ConfigMapRow{}
	for ns, names := range configMapRefs {
		var keys []string
		for name := range names {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			cfg, err := s.client.Clientset.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			configRows = append(configRows, toConfigMapRow(cfg))
		}
	}

	snapshot := &sqlitewriter.ContextSnapshot{
		Events:      eventRows,
		Deployments: deployRows,
		ConfigMaps:  configRows,
	}
	return snapshot, pods, eventsByPod, nil
}

func extractConfigMaps(pod *corev1.Pod) []string {
	set := make(map[string]struct{})
	for _, vol := range pod.Spec.Volumes {
		if vol.ConfigMap != nil {
			set[vol.ConfigMap.Name] = struct{}{}
		}
	}
	for _, c := range pod.Spec.Containers {
		for _, env := range c.EnvFrom {
			if env.ConfigMapRef != nil && env.ConfigMapRef.Name != "" {
				set[env.ConfigMapRef.Name] = struct{}{}
			}
		}
		for _, env := range c.Env {
			if env.ValueFrom != nil && env.ValueFrom.ConfigMapKeyRef != nil {
				set[env.ValueFrom.ConfigMapKeyRef.Name] = struct{}{}
			}
		}
	}
	refs := make([]string, 0, len(set))
	for name := range set {
		refs = append(refs, name)
	}
	sort.Strings(refs)
	return refs
}

func toDeploymentRow(dep appsv1.Deployment) sqlitewriter.DeploymentRow {
	labels, _ := json.Marshal(dep.Labels)
	annotations, _ := json.Marshal(dep.Annotations)
	selector, _ := json.Marshal(dep.Spec.Selector)
	conditions, _ := json.Marshal(dep.Status.Conditions)
	strategy := dep.Spec.Strategy.Type
	return sqlitewriter.DeploymentRow{
		Namespace:         dep.Namespace,
		Name:              dep.Name,
		Replicas:          int(valueOrZero(dep.Spec.Replicas)),
		UpdatedReplicas:   int(dep.Status.UpdatedReplicas),
		ReadyReplicas:     int(dep.Status.ReadyReplicas),
		AvailableReplicas: int(dep.Status.AvailableReplicas),
		Strategy:          string(strategy),
		Selector:          string(selector),
		Labels:            string(labels),
		Annotations:       string(annotations),
		Conditions:        string(conditions),
		CreatedAt:         dep.CreationTimestamp.Time.UTC(),
	}
}

func toConfigMapRow(cfg *corev1.ConfigMap) sqlitewriter.ConfigMapRow {
	data, _ := json.Marshal(cfg.Data)
	binary, _ := json.Marshal(cfg.BinaryData)
	labels, _ := json.Marshal(cfg.Labels)
	annotations, _ := json.Marshal(cfg.Annotations)
	return sqlitewriter.ConfigMapRow{
		Namespace:       cfg.Namespace,
		Name:            cfg.Name,
		Data:            string(data),
		BinaryData:      string(binary),
		Labels:          string(labels),
		Annotations:     string(annotations),
		ResourceVersion: cfg.ResourceVersion,
		CreatedAt:       cfg.CreationTimestamp.Time.UTC(),
	}
}

func valueOrZero(ptr *int32) int32 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

func (s *Session) writeDescribeAttachments(ctx context.Context, pods map[string]*corev1.Pod, events map[string][]corev1.Event) error {
	base := filepath.Join(s.tempDir, "describes")
	for key, pod := range pods {
		evt := events[key]
		content := renderPodDescribe(pod, evt)
		dir := filepath.Join(base, pod.Namespace)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := filepath.Join(dir, fmt.Sprintf("%s.txt", pod.Name))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func renderPodDescribe(pod *corev1.Pod, events []corev1.Event) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Name: %s\nNamespace: %s\nNode: %s\nPhase: %s\nStart Time: %s\n\n",
		pod.Name, pod.Namespace, pod.Spec.NodeName, pod.Status.Phase, pod.CreationTimestamp.Time.UTC().Format(time.RFC3339))
	fmt.Fprintf(&builder, "Labels: %s\nAnnotations: %s\n\n", formatKV(pod.Labels), formatKV(pod.Annotations))
	builder.WriteString("Containers:\n")
	for _, c := range pod.Status.ContainerStatuses {
		fmt.Fprintf(&builder, "- %s (image=%s)\n  Ready: %t  Restarts: %d  State: %s\n",
			c.Name, c.Image, c.Ready, c.RestartCount, describeContainerState(c.State))
	}
	builder.WriteString("\nEvents:\n")
	if len(events) == 0 {
		builder.WriteString("  <none>\n")
	} else {
		sort.Slice(events, func(i, j int) bool {
			return events[i].LastTimestamp.Time.Before(events[j].LastTimestamp.Time)
		})
		for _, evt := range events {
			ts := evt.LastTimestamp.Time
			if ts.IsZero() {
				ts = evt.EventTime.Time
			}
			fmt.Fprintf(&builder, "  %s [%s] %s: %s (x%d)\n", ts.UTC().Format(time.RFC3339), evt.Type, evt.Reason, evt.Message, evt.Count)
		}
	}
	return builder.String()
}

func formatKV(values map[string]string) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, values[k]))
	}
	return strings.Join(parts, ", ")
}
