package k8sfacts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectKubernetesFacts(t *testing.T) {
	replicas := int32(2)
	client := fake.NewClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "apps"},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
				NodeInfo: corev1.NodeSystemInfo{
					KubeletVersion: "v1.30.0",
					OSImage:        "Ubuntu 24.04",
					Architecture:   "amd64",
				},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 2, AvailableReplicas: 2},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "apps"},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "api-warning", Namespace: "apps"},
			Type:           corev1.EventTypeWarning,
			Reason:         "BackOff",
			Message:        "container restart backoff",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-0", Namespace: "apps"},
		},
	)

	snapshot, err := Collect(context.Background(), client, CollectRequest{
		TargetID:        "k8s/lab",
		TargetDigest:    TargetDigest("k8s/lab"),
		ObservedAt:      time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Namespaces:      []string{"apps"},
		EventLimit:      10,
		WorkloadSamples: 10,
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if snapshot.APIVersion != APIVersion || snapshot.Kind != Kind {
		t.Fatalf("unexpected snapshot identity: %#v", snapshot)
	}
	if snapshot.Namespaces.Count != 1 || len(snapshot.Namespaces.Selected) != 1 || snapshot.Namespaces.Selected[0].Name != "apps" {
		t.Fatalf("namespace facts = %#v", snapshot.Namespaces)
	}
	if snapshot.Nodes.Count != 1 || snapshot.Nodes.ReadyCount != 1 {
		t.Fatalf("node facts = %#v", snapshot.Nodes)
	}
	if snapshot.Workloads.Deployments.Count != 1 || snapshot.Workloads.Deployments.Ready != 1 || snapshot.Workloads.Pods.ReadyPods != 1 {
		t.Fatalf("workload facts = %#v", snapshot.Workloads)
	}
	if snapshot.Events.WarningCount != 1 {
		t.Fatalf("event facts = %#v", snapshot.Events)
	}
	if snapshot.Digest == "" || snapshot.Digest != snapshot.StableDigest() {
		t.Fatalf("unstable digest: %q vs %q", snapshot.Digest, snapshot.StableDigest())
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if strings.Contains(string(raw), "secret://") {
		t.Fatalf("snapshot leaked secret reference: %s", raw)
	}
}
