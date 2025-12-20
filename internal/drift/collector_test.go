// File: internal/drift/collector_test.go
// Brief: Internal drift package implementation for 'collector'.

// collector_test.go covers the drift sampler logic and its edge cases.
package drift

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectorSnapshotStoresTimeline(t *testing.T) {
	ctx := context.Background()
	objects := []runtime.Object{
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "checkout",
				Namespace: "default",
				UID:       types.UID("deploy-uid"),
				Annotations: map[string]string{
					"deployment.kubernetes.io/revision": "42",
				},
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "checkout-abc123",
				Namespace: "default",
				UID:       types.UID("rs-uid"),
				Labels: map[string]string{
					"pod-template-hash": "abc123",
				},
				Annotations: map[string]string{
					"deployment.kubernetes.io/revision": "42",
				},
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "Deployment", Name: "checkout", UID: types.UID("deploy-uid")},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "checkout-abc123-xyz",
				Namespace: "default",
				Labels:    map[string]string{"pod-template-hash": "abc123"},
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "ReplicaSet", Name: "checkout-abc123", UID: types.UID("rs-uid")},
				},
			},
			Spec: corev1.PodSpec{NodeName: "node-1"},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:         "web",
					Ready:        true,
					RestartCount: 1,
					Image:        "img:v1",
					State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Time{Time: time.Now()}}},
				}},
			},
		},
	}
	client := fake.NewSimpleClientset(objects...)
	collector := NewCollector(client, []string{"default"}, 2)
	snapshot, err := collector.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Pods) != 1 {
		t.Fatalf("expected 1 pod snapshot, got %d", len(snapshot.Pods))
	}
	pod := snapshot.Pods[0]
	if pod.RolloutHash != "abc123" {
		t.Fatalf("expected rollout hash abc123, got %s", pod.RolloutHash)
	}
	if len(pod.Owners) != 2 {
		t.Fatalf("expected owner chain of length 2, got %d", len(pod.Owners))
	}
	if pod.Owners[0].Kind != "ReplicaSet" || pod.Owners[1].Kind != "Deployment" {
		t.Fatalf("unexpected owner chain order: %+v", pod.Owners)
	}
	if got := pod.Containers[0].State; got != "running" {
		t.Fatalf("expected container state 'running', got %s", got)
	}
	b := collector.Buffer().Snapshots()
	if len(b) != 1 {
		t.Fatalf("expected buffer to have 1 snapshot, got %d", len(b))
	}
	// Add another snapshot to verify ring behavior
	if _, err := collector.Snapshot(ctx); err != nil {
		t.Fatalf("second snapshot failed: %v", err)
	}
	if len(collector.Buffer().Snapshots()) != 2 {
		t.Fatalf("expected buffer capacity 2, got %d", len(collector.Buffer().Snapshots()))
	}
	if collector.Buffer().Snapshots()[0].Timestamp.After(time.Now().UTC()) {
		t.Fatalf("snapshot timestamp should be <= now")
	}
}
