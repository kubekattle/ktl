// File: internal/capture/manifests.go
// Brief: Internal capture package implementation for 'manifests'.

// Package capture provides capture helpers.

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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

type captureManifestIndex struct {
	GeneratedAt time.Time                 `json:"generatedAt"`
	Resources   []captureManifestResource `json:"resources"`
}

type captureManifestResource struct {
	APIVersion string                    `json:"apiVersion"`
	Kind       string                    `json:"kind"`
	Namespace  string                    `json:"namespace,omitempty"`
	Name       string                    `json:"name"`
	Owners     []captureManifestOwnerRef `json:"owners,omitempty"`
	Path       string                    `json:"path"`
}

type captureManifestOwnerRef struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	UID        string `json:"uid,omitempty"`
	Controller bool   `json:"controller,omitempty"`
}

func (s *Session) writeManifestAttachments(ctx context.Context, pods map[string]*corev1.Pod) error {
	if len(pods) == 0 {
		return nil
	}
	base := filepath.Join(s.tempDir, "manifests")
	resBase := filepath.Join(base, "resources")
	if err := os.MkdirAll(resBase, 0o755); err != nil {
		return err
	}

	seen := make(map[string]captureManifestResource)
	var queue []runtime.Object
	podsByNamespace := make(map[string][]*corev1.Pod)
	configMapRefs := make(map[string]map[string]struct{})
	for _, pod := range pods {
		if pod == nil {
			continue
		}
		queue = append(queue, pod)
		podsByNamespace[pod.Namespace] = append(podsByNamespace[pod.Namespace], pod)
		for _, cfgName := range extractConfigMaps(pod) {
			if _, ok := configMapRefs[pod.Namespace]; !ok {
				configMapRefs[pod.Namespace] = make(map[string]struct{})
			}
			configMapRefs[pod.Namespace][cfgName] = struct{}{}
		}
	}

	// Add services that select any observed pod.
	for ns, nsPods := range podsByNamespace {
		svcs, err := s.client.Clientset.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for i := range svcs.Items {
			svc := &svcs.Items[i]
			if svc == nil {
				continue
			}
			selector := svc.Spec.Selector
			if len(selector) == 0 {
				continue
			}
			matches := false
			for _, pod := range nsPods {
				if pod == nil {
					continue
				}
				if podLabelsMatchSelector(pod.Labels, selector) {
					matches = true
					break
				}
			}
			if matches {
				queue = append(queue, svc)
			}
		}
	}

	// Add referenced ConfigMaps (redacted by sanitizer).
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
			queue = append(queue, cfg)
		}
	}

	addObject := func(obj runtime.Object) error {
		if obj == nil {
			return nil
		}
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil
		}
		gvk := gvkForObject(obj)
		if gvk.Empty() {
			return nil
		}
		ns := strings.TrimSpace(accessor.GetNamespace())
		name := strings.TrimSpace(accessor.GetName())
		if name == "" {
			return nil
		}
		key := fmt.Sprintf("%s/%s/%s/%s", gvk.Group, gvk.Kind, ns, name)
		if _, ok := seen[key]; ok {
			return nil
		}
		owners := make([]captureManifestOwnerRef, 0, len(accessor.GetOwnerReferences()))
		for _, ref := range accessor.GetOwnerReferences() {
			owners = append(owners, captureManifestOwnerRef{
				Kind:       ref.Kind,
				Name:       ref.Name,
				Namespace:  ns,
				UID:        string(ref.UID),
				Controller: ref.Controller != nil && *ref.Controller,
			})
		}

		fileRel := filepath.Join(
			"resources",
			emptyString(ns, "_cluster"),
			safePathComponent(strings.ToLower(gvk.Kind)),
			safePathComponent(name)+".yaml",
		)
		fileAbs := filepath.Join(base, fileRel)
		if err := os.MkdirAll(filepath.Dir(fileAbs), 0o755); err != nil {
			return err
		}
		yml, err := marshalSanitizedYAML(obj)
		if err != nil {
			return nil
		}
		if err := os.WriteFile(fileAbs, yml, 0o644); err != nil {
			return err
		}
		seen[key] = captureManifestResource{
			APIVersion: gvk.GroupVersion().String(),
			Kind:       gvk.Kind,
			Namespace:  ns,
			Name:       name,
			Owners:     owners,
			Path:       filepath.ToSlash(fileRel),
		}
		return nil
	}

	fetchAndEnqueueOwners := func(obj runtime.Object) {
		if obj == nil {
			return
		}
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return
		}
		ns := strings.TrimSpace(accessor.GetNamespace())
		for _, ref := range accessor.GetOwnerReferences() {
			parent := s.fetchOwnerObject(ctx, ns, ref)
			if parent != nil {
				queue = append(queue, parent)
			}
		}
	}

	for i := 0; i < len(queue); i++ {
		obj := queue[i]
		if err := addObject(obj); err != nil {
			return err
		}
		fetchAndEnqueueOwners(obj)
	}

	index := captureManifestIndex{GeneratedAt: time.Now().UTC()}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	index.Resources = make([]captureManifestResource, 0, len(seen))
	for _, k := range keys {
		index.Resources = append(index.Resources, seen[k])
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(base, "index.json"), data, 0o644)
}

func (s *Session) fetchOwnerObject(ctx context.Context, namespace string, ref metav1.OwnerReference) runtime.Object {
	ns := strings.TrimSpace(namespace)
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return nil
	}
	switch ref.Kind {
	case "ReplicaSet":
		rs, err := s.client.Clientset.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return rs
	case "Deployment":
		deploy, err := s.client.Clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return deploy
	case "StatefulSet":
		sts, err := s.client.Clientset.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return sts
	case "DaemonSet":
		ds, err := s.client.Clientset.AppsV1().DaemonSets(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return ds
	case "Job":
		job, err := s.client.Clientset.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return job
	case "CronJob":
		cj, err := s.client.Clientset.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return cj
	default:
		return nil
	}
}

func gvkForObject(obj runtime.Object) schema.GroupVersionKind {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	if err != nil || len(gvks) == 0 {
		return schema.GroupVersionKind{}
	}
	return gvks[0]
}

func marshalSanitizedYAML(obj runtime.Object) ([]byte, error) {
	// Roundtrip through JSON to get a map we can easily scrub.
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if gvk := gvkForObject(obj); !gvk.Empty() {
		if _, ok := doc["apiVersion"]; !ok {
			doc["apiVersion"] = gvk.GroupVersion().String()
		}
		if _, ok := doc["kind"]; !ok {
			doc["kind"] = gvk.Kind
		}
	}
	// Drop noisy + often non-shareable fields.
	delete(doc, "status")
	if kind, ok := doc["kind"].(string); ok && strings.EqualFold(strings.TrimSpace(kind), "ConfigMap") {
		if data, ok := doc["data"].(map[string]any); ok {
			for k := range data {
				data[k] = "<redacted>"
			}
		}
		if data, ok := doc["binaryData"].(map[string]any); ok {
			for k := range data {
				data[k] = "<redacted>"
			}
		}
	}
	metaAny, ok := doc["metadata"].(map[string]any)
	if ok {
		delete(metaAny, "managedFields")
		delete(metaAny, "resourceVersion")
		delete(metaAny, "uid")
		delete(metaAny, "generation")
		delete(metaAny, "creationTimestamp")
		delete(metaAny, "selfLink")
		if ann, ok := metaAny["annotations"].(map[string]any); ok {
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			if len(ann) == 0 {
				delete(metaAny, "annotations")
			}
		}
	}
	return yaml.Marshal(doc)
}

func safePathComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '.' || r == '_' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

func emptyString(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func podLabelsMatchSelector(labels map[string]string, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels == nil {
			return false
		}
		if labels[k] != v {
			return false
		}
	}
	return true
}
