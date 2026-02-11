package main

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestParseDeployMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		modeRaw string
		pinRaw  string
		want    deployMode
		wantErr bool
	}{
		{name: "default", modeRaw: "", want: deployModeActive},
		{name: "active", modeRaw: "active", want: deployModeActive},
		{name: "stable", modeRaw: "stable", want: deployModeStable},
		{name: "canary", modeRaw: "canary", want: deployModeCanary},
		{name: "stable-canary", modeRaw: "stable+canary", want: deployModeStableCanary},
		{name: "pin-fallback-stable", modeRaw: "typo", pinRaw: "stable", want: deployModeStable},
		{name: "pin-fallback-canary", modeRaw: "typo", pinRaw: "canary", want: deployModeCanary},
		{name: "pin-fallback-both", modeRaw: "typo", pinRaw: "stable,canary", want: deployModeStableCanary},
		{name: "unknown", modeRaw: "typo", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseDeployMode(tt.modeRaw, tt.pinRaw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mode=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeDeploymentLensFromObjects(t *testing.T) {
	t.Parallel()
	uid := types.UID("deploy-uid")
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api",
			Namespace: "prod",
			UID:       uid,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"pod-template-hash": "h2"},
				},
			},
		},
	}

	rsStable := newRS("prod", "api-rs-1", uid, "h1", 1, 1, 1, 1)
	rsCanary := newRS("prod", "api-rs-2", uid, "h2", 2, 1, 1, 0)
	pods := []*corev1.Pod{
		newPod("prod", "api-1", rsStable.Name),
		newPod("prod", "api-2", rsCanary.Name),
	}
	replicaSets := []*appsv1.ReplicaSet{rsStable, rsCanary}

	tests := []struct {
		name string
		mode deployMode
		want map[string]string
	}{
		{
			name: "active",
			mode: deployModeActive,
			want: map[string]string{
				"prod/api-1": "stable",
				"prod/api-2": "canary",
			},
		},
		{
			name: "stable",
			mode: deployModeStable,
			want: map[string]string{
				"prod/api-1": "stable",
			},
		},
		{
			name: "canary",
			mode: deployModeCanary,
			want: map[string]string{
				"prod/api-2": "canary",
			},
		},
		{
			name: "stable+canary",
			mode: deployModeStableCanary,
			want: map[string]string{
				"prod/api-1": "stable",
				"prod/api-2": "canary",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeDeploymentLensFromObjects(deploy, replicaSets, pods, tt.mode)
			assertMapEqual(t, got, tt.want)
		})
	}
}

func newRS(namespace, name string, deployUID types.UID, hash string, revision int64, specReplicas, ready, available int32) *appsv1.ReplicaSet {
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"deployment.kubernetes.io/revision": "0",
			},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Deployment", Name: "api", UID: deployUID},
			},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Duration(revision) * time.Minute)),
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &specReplicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"pod-template-hash": hash},
				},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			ReadyReplicas:     ready,
			AvailableReplicas: available,
		},
	}
	rs.Annotations["deployment.kubernetes.io/revision"] = itoa64(revision)
	return rs
}

func newPod(namespace, name, rsName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "ReplicaSet", Name: rsName},
			},
		},
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func assertMapEqual(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d (got=%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %q: got %q, want %q (got=%v)", k, got[k], v, got)
		}
	}
}
