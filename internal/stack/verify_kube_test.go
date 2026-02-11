package stack

import (
	"context"
	"testing"
	"time"

	"github.com/example/ktl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestVerifyKubeRelease_WarningEventFilters(t *testing.T) {
	client := &kube.Client{Clientset: fake.NewSimpleClientset()}
	ns := "ns"
	release := "rel"
	manifest := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: rel
  namespace: ns
`

	now := time.Now().UTC()
	addEvent := func(reason string, ts time.Time) {
		_, _ = client.Clientset.CoreV1().Events(ns).Create(context.Background(), &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: reason + "-" + ts.Format("150405.000"), Namespace: ns, ResourceVersion: "99"},
			Type:       "Warning",
			Reason:     reason,
			Message:    "boom",
			InvolvedObject: corev1.ObjectReference{
				Kind:      "ConfigMap",
				Name:      "rel",
				Namespace: ns,
			},
			LastTimestamp: metav1.NewTime(ts),
		}, metav1.CreateOptions{})
	}

	// Baseline: no events => ok.
	_, err := verifyKubeRelease(context.Background(), client, ns, release, manifest, VerifyOptions{
		Enabled:        boolPtr(true),
		FailOnWarnings: boolPtr(true),
		EventsWindow:   durationPtr(15 * time.Minute),
	}, 0, "")
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}

	// Event is present => fail.
	addEvent("KtlVerifyDemo", now.Add(-30*time.Second))
	_, err = verifyKubeRelease(context.Background(), client, ns, release, manifest, VerifyOptions{
		Enabled:        boolPtr(true),
		FailOnWarnings: boolPtr(true),
		EventsWindow:   durationPtr(15 * time.Minute),
	}, 0, "")
	if err == nil {
		t.Fatalf("expected warning failure, got nil")
	}

	// DenyReasons acts as a reason allowlist: ignore non-matching reasons.
	_, err = verifyKubeRelease(context.Background(), client, ns, release, manifest, VerifyOptions{
		Enabled:        boolPtr(true),
		FailOnWarnings: boolPtr(true),
		DenyReasons:    []string{"SomeOtherReason"},
		EventsWindow:   durationPtr(15 * time.Minute),
	}, 0, "")
	if err != nil {
		t.Fatalf("expected ok with denyReasons filter, got %v", err)
	}

	// Watermark excludes old warnings: okSince wins over eventsWindow lower bound.
	_, err = verifyKubeRelease(context.Background(), client, ns, release, manifest, VerifyOptions{
		Enabled:        boolPtr(true),
		FailOnWarnings: boolPtr(true),
		EventsWindow:   durationPtr(15 * time.Minute),
	}, now.Add(-1*time.Second).UnixNano(), "")
	if err != nil {
		t.Fatalf("expected ok with watermark, got %v", err)
	}

	// ResourceVersion watermark excludes old events without relying on clocks.
	_, err = verifyKubeRelease(context.Background(), client, ns, release, manifest, VerifyOptions{
		Enabled:        boolPtr(true),
		FailOnWarnings: boolPtr(true),
		EventsWindow:   durationPtr(15 * time.Minute),
	}, 0, `{"ns":"100"}`)
	if err != nil {
		t.Fatalf("expected ok with rv watermark, got %v", err)
	}
}

func boolPtr(v bool) *bool                       { return &v }
func durationPtr(d time.Duration) *time.Duration { return &d }
