package stack

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func verifyEnabled(v VerifyOptions) bool {
	if v.Enabled == nil {
		return false
	}
	return *v.Enabled
}

func verifyFailOnWarnings(v VerifyOptions) bool {
	if v.FailOnWarnings == nil {
		return true
	}
	return *v.FailOnWarnings
}

func verifyEventsWindow(v VerifyOptions) time.Duration {
	if v.EventsWindow == nil || *v.EventsWindow <= 0 {
		return 15 * time.Minute
	}
	return *v.EventsWindow
}

func verifyKubeRelease(ctx context.Context, kubeClient *kube.Client, defaultNamespace string, releaseName string, manifest string, v VerifyOptions) (string, error) {
	if kubeClient == nil || kubeClient.Clientset == nil {
		return "", fmt.Errorf("missing kube client")
	}
	if strings.TrimSpace(releaseName) == "" {
		return "", fmt.Errorf("missing release name")
	}
	manifest = strings.TrimSpace(manifest)
	if manifest == "" {
		return "no manifest targets", nil
	}

	targets := deploy.ManifestTargets(manifest)
	targetKey := map[string]struct{}{}
	nsSet := map[string]struct{}{}
	for _, t := range targets {
		ns := strings.TrimSpace(t.Namespace)
		if ns == "" {
			ns = strings.TrimSpace(defaultNamespace)
		}
		if ns == "" {
			ns = "default"
		}
		targetKey[strings.ToLower(strings.TrimSpace(t.Kind))+"\n"+ns+"\n"+strings.TrimSpace(t.Name)] = struct{}{}
		nsSet[ns] = struct{}{}
	}
	var namespaces []string
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	// Conditions: leverage the existing tracker, which already evaluates core workload readiness.
	tracker := deploy.NewResourceTracker(kubeClient, defaultNamespace, releaseName, manifest, nil)
	rows := tracker.Snapshot(ctx)
	if !allReleaseResourcesReady(rows) {
		blockers := deploy.TopBlockers(rows, 6)
		if len(blockers) > 0 {
			var parts []string
			for _, b := range blockers {
				parts = append(parts, fmt.Sprintf("%s/%s=%s", b.Kind, b.Name, b.Status))
			}
			return "", fmt.Errorf("verify: not ready (%s)", strings.Join(parts, ", "))
		}
		return "", fmt.Errorf("verify: not ready")
	}

	if verifyFailOnWarnings(v) {
		window := verifyEventsWindow(v)
		since := time.Now().Add(-window)
		var warnings []corev1.Event
		for _, ns := range namespaces {
			evs, err := kubeClient.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			for i := range evs.Items {
				ev := evs.Items[i]
				if strings.ToLower(strings.TrimSpace(ev.Type)) != "warning" {
					continue
				}
				ts := eventTimestamp(ev)
				if !ts.IsZero() && ts.Before(since) {
					continue
				}
				kind := strings.TrimSpace(ev.InvolvedObject.Kind)
				name := strings.TrimSpace(ev.InvolvedObject.Name)
				evNS := strings.TrimSpace(ev.InvolvedObject.Namespace)
				if evNS == "" {
					evNS = ns
				}
				if _, ok := targetKey[strings.ToLower(kind)+"\n"+evNS+"\n"+name]; !ok {
					continue
				}
				warnings = append(warnings, ev)
			}
		}
		sort.Slice(warnings, func(i, j int) bool { return eventTimestamp(warnings[i]).Before(eventTimestamp(warnings[j])) })
		if len(warnings) > 0 {
			ev := warnings[len(warnings)-1]
			reason := strings.TrimSpace(ev.Reason)
			msg := strings.TrimSpace(ev.Message)
			if reason == "" {
				reason = "Warning"
			}
			if msg == "" {
				msg = "-"
			}
			return "", fmt.Errorf("verify: warning events observed (count=%d latest=%s: %s)", len(warnings), reason, msg)
		}
	}

	return "verified", nil
}

func eventTimestamp(ev corev1.Event) time.Time {
	if !ev.EventTime.IsZero() {
		return ev.EventTime.Time
	}
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.FirstTimestamp.IsZero() {
		return ev.FirstTimestamp.Time
	}
	if ev.Series != nil && !ev.Series.LastObservedTime.IsZero() {
		return ev.Series.LastObservedTime.Time
	}
	return time.Time{}
}
