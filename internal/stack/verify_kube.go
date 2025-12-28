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
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func verifyWarnOnly(v VerifyOptions) bool {
	if v.WarnOnly == nil {
		return false
	}
	return *v.WarnOnly
}

func verifyEventsWindow(v VerifyOptions) time.Duration {
	if v.EventsWindow == nil || *v.EventsWindow <= 0 {
		return 15 * time.Minute
	}
	return *v.EventsWindow
}

func verifyTimeout(v VerifyOptions) time.Duration {
	if v.Timeout == nil || *v.Timeout <= 0 {
		return 2 * time.Minute
	}
	return *v.Timeout
}

func verifyKubeRelease(ctx context.Context, kubeClient *kube.Client, defaultNamespace string, releaseName string, manifest string, v VerifyOptions, okSinceNS int64) (string, error) {
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
				reason := strings.TrimSpace(b.Reason)
				msg := strings.TrimSpace(b.Message)
				if reason == "" {
					reason = "-"
				}
				if msg == "" {
					msg = "-"
				}
				parts = append(parts, fmt.Sprintf("%s/%s status=%s reason=%s msg=%s", b.Kind, b.Name, b.Status, reason, msg))
			}
			return "", fmt.Errorf("verify: not ready (top blockers: %s)", strings.Join(parts, " | "))
		}
		return "", fmt.Errorf("verify: not ready")
	}

	if len(v.RequireConditions) > 0 {
		if err := verifyRequiredConditions(ctx, kubeClient, defaultNamespace, targets, v.RequireConditions); err != nil {
			return "", err
		}
	}

	if verifyFailOnWarnings(v) {
		window := verifyEventsWindow(v)
		since := time.Now().Add(-window)
		if okSinceNS > 0 {
			if t := time.Unix(0, okSinceNS); t.After(since) {
				since = t
			}
		}
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
				if !verifyReasonAllowed(v, strings.TrimSpace(ev.Reason)) {
					continue
				}
				warnings = append(warnings, ev)
			}
		}
		sort.Slice(warnings, func(i, j int) bool { return eventTimestamp(warnings[i]).Before(eventTimestamp(warnings[j])) })
		if len(warnings) > 0 {
			latest := warnings[len(warnings)-1]
			return "", fmt.Errorf("verify: warning events observed (count=%d since=%s latest=%s)", len(warnings), since.Format(time.RFC3339), formatEventSummary(latest))
		}
	}

	return "verified", nil
}

func formatEventSummary(ev corev1.Event) string {
	kind := strings.TrimSpace(ev.InvolvedObject.Kind)
	name := strings.TrimSpace(ev.InvolvedObject.Name)
	ns := strings.TrimSpace(ev.InvolvedObject.Namespace)
	reason := strings.TrimSpace(ev.Reason)
	msg := strings.TrimSpace(ev.Message)
	if kind == "" {
		kind = "Object"
	}
	if reason == "" {
		reason = "Warning"
	}
	if msg == "" {
		msg = "-"
	}
	target := fmt.Sprintf("%s/%s", kind, name)
	if ns != "" {
		target = fmt.Sprintf("%s/%s", ns, target)
	}
	return fmt.Sprintf("%s %s: %s", target, reason, msg)
}

func verifyRequiredConditions(ctx context.Context, kubeClient *kube.Client, defaultNamespace string, targets []deploy.ManifestTarget, reqs []VerifyConditionRequirement) error {
	if kubeClient == nil || kubeClient.Dynamic == nil || kubeClient.RESTMapper == nil {
		return fmt.Errorf("verify: requireConditions requires dynamic client + rest mapper")
	}
	defaultNamespace = strings.TrimSpace(defaultNamespace)
	if defaultNamespace == "" {
		defaultNamespace = "default"
	}

	type reqKey struct {
		group string
		kind  string
	}
	byGK := map[reqKey][]VerifyConditionRequirement{}
	for _, r := range reqs {
		g := strings.ToLower(strings.TrimSpace(r.Group))
		k := strings.ToLower(strings.TrimSpace(r.Kind))
		if g == "" || k == "" {
			continue
		}
		byGK[reqKey{group: g, kind: k}] = append(byGK[reqKey{group: g, kind: k}], r)
	}
	if len(byGK) == 0 {
		return nil
	}

	for _, t := range targets {
		g := strings.ToLower(strings.TrimSpace(t.Group))
		k := strings.ToLower(strings.TrimSpace(t.Kind))
		list := byGK[reqKey{group: g, kind: k}]
		if len(list) == 0 {
			continue
		}
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		ns := strings.TrimSpace(t.Namespace)
		if ns == "" {
			ns = defaultNamespace
		}

		gvk := schema.GroupVersionKind{Group: strings.TrimSpace(t.Group), Version: strings.TrimSpace(t.Version), Kind: strings.TrimSpace(t.Kind)}
		mapping, err := kubeClient.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("verify: map %s/%s: %w", strings.TrimSpace(t.Kind), name, err)
		}
		res := kubeClient.Dynamic.Resource(mapping.Resource)
		var obj *unstructured.Unstructured
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			obj, err = res.Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		} else {
			obj, err = res.Get(ctx, name, metav1.GetOptions{})
		}
		if err != nil {
			for _, r := range list {
				if r.AllowMissing {
					continue
				}
				return fmt.Errorf("verify: %s/%s missing (required condition %s)", strings.TrimSpace(t.Kind), name, strings.TrimSpace(r.ConditionType))
			}
			continue
		}

		for _, r := range list {
			condType := strings.TrimSpace(r.ConditionType)
			wantStatus := strings.TrimSpace(r.RequireStatus)
			if wantStatus == "" {
				wantStatus = "True"
			}
			gotStatus, gotReason, gotMsg, ok := findStatusCondition(obj, condType)
			if !ok {
				if r.AllowMissing {
					continue
				}
				return fmt.Errorf("verify: %s/%s missing condition %q", strings.TrimSpace(t.Kind), name, condType)
			}
			if strings.ToLower(gotStatus) != strings.ToLower(wantStatus) {
				detail := strings.TrimSpace(gotReason)
				if detail == "" {
					detail = strings.TrimSpace(gotMsg)
				}
				if detail != "" {
					return fmt.Errorf("verify: %s/%s condition %s=%s (want %s): %s", strings.TrimSpace(t.Kind), name, condType, gotStatus, wantStatus, detail)
				}
				return fmt.Errorf("verify: %s/%s condition %s=%s (want %s)", strings.TrimSpace(t.Kind), name, condType, gotStatus, wantStatus)
			}
		}
	}
	return nil
}

func findStatusCondition(obj *unstructured.Unstructured, condType string) (status string, reason string, message string, ok bool) {
	if obj == nil {
		return "", "", "", false
	}
	condType = strings.TrimSpace(condType)
	if condType == "" {
		return "", "", "", false
	}
	raw, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found || len(raw) == 0 {
		return "", "", "", false
	}
	for i := range raw {
		m, ok := raw[i].(map[string]interface{})
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if strings.ToLower(strings.TrimSpace(t)) != strings.ToLower(condType) {
			continue
		}
		s, _ := m["status"].(string)
		r, _ := m["reason"].(string)
		msg, _ := m["message"].(string)
		return strings.TrimSpace(s), strings.TrimSpace(r), strings.TrimSpace(msg), true
	}
	return "", "", "", false
}

func verifyReasonAllowed(v VerifyOptions, reason string) bool {
	r := strings.ToLower(strings.TrimSpace(reason))
	if len(v.AllowReasons) > 0 {
		for _, a := range v.AllowReasons {
			if strings.ToLower(strings.TrimSpace(a)) == r {
				return true
			}
		}
		return false
	}
	if len(v.DenyReasons) > 0 {
		for _, d := range v.DenyReasons {
			if strings.ToLower(strings.TrimSpace(d)) == r {
				return true
			}
		}
		return false
	}
	return true
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
