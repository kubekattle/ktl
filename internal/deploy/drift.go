package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type DriftOptions struct {
	RequireHelmOwnership bool
	IgnoreMissing        bool
	MaxConcurrency       int
	PerObjectTimeout     time.Duration
}

type DriftItem struct {
	Kind      string
	Namespace string
	Name      string
	Reason    string
	Diff      string
}

type DriftReport struct {
	Items []DriftItem
}

func (r DriftReport) Empty() bool { return len(r.Items) == 0 }

type DriftLiveGetter func(ctx context.Context, target resourceTarget) (*unstructured.Unstructured, error)

func CheckReleaseDrift(ctx context.Context, releaseName string, manifest string, get DriftLiveGetter) (DriftReport, error) {
	return CheckReleaseDriftWithOptions(ctx, releaseName, manifest, get, DriftOptions{
		RequireHelmOwnership: true,
		IgnoreMissing:        false,
		MaxConcurrency:       8,
		PerObjectTimeout:     6 * time.Second,
	})
}

func CheckReleaseDriftWithOptions(ctx context.Context, releaseName string, manifest string, get DriftLiveGetter, opts DriftOptions) (DriftReport, error) {
	if strings.TrimSpace(manifest) == "" || get == nil {
		return DriftReport{}, nil
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 8
	}
	if opts.PerObjectTimeout <= 0 {
		opts.PerObjectTimeout = 6 * time.Second
	}
	files := splitManifestDocs(manifest)
	type job struct {
		base   *unstructured.Unstructured
		target resourceTarget
	}
	jobs := make([]job, 0, len(files))
	for _, doc := range files {
		base, target, ok := parseManifestDoc(doc)
		if !ok {
			continue
		}
		if isHookResource(base) {
			continue
		}
		if opts.RequireHelmOwnership && !hasHelmOwnership(releaseName, base) {
			continue
		}
		jobs = append(jobs, job{base: base, target: target})
	}
	if len(jobs) == 0 {
		return DriftReport{}, nil
	}

	var (
		mu       sync.Mutex
		out      DriftReport
		firstErr error
	)
	sem := make(chan struct{}, opts.MaxConcurrency)
	var wg sync.WaitGroup
	for _, j := range jobs {
		if firstErr != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()

			itemCtx, cancel := context.WithTimeout(ctx, opts.PerObjectTimeout)
			defer cancel()

			live, err := get(itemCtx, j.target)
			if err != nil {
				mu.Lock()
				out.Items = append(out.Items, DriftItem{
					Kind:      j.target.Kind,
					Namespace: j.target.Namespace,
					Name:      j.target.Name,
					Reason:    fmt.Sprintf("fetch_error: %v", err),
				})
				mu.Unlock()
				return
			}
			if live == nil {
				if opts.IgnoreMissing {
					return
				}
				mu.Lock()
				out.Items = append(out.Items, DriftItem{
					Kind:      j.target.Kind,
					Namespace: j.target.Namespace,
					Name:      j.target.Name,
					Reason:    "missing",
				})
				mu.Unlock()
				return
			}
			if opts.RequireHelmOwnership && !hasHelmOwnership(releaseName, live) {
				// Skip unmanaged/shared resources to avoid false positives.
				return
			}

			baseNorm := normalizeForDrift(j.base)
			liveNorm := normalizeForDrift(live)

			eq, diff, derr := diffUnstructured(baseNorm, liveNorm)
			if derr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = derr
				}
				mu.Unlock()
				return
			}
			if !eq {
				mu.Lock()
				out.Items = append(out.Items, DriftItem{
					Kind:      j.target.Kind,
					Namespace: pickNamespace(j.target.Namespace, live.GetNamespace()),
					Name:      j.target.Name,
					Reason:    "changed",
					Diff:      diff,
				})
				mu.Unlock()
			}
		}(j)
	}
	wg.Wait()
	if firstErr != nil {
		return DriftReport{}, firstErr
	}
	return out, nil
}

func splitManifestDocs(manifest string) []string {
	files := releaseutil.SplitManifests(manifest)
	out := make([]string, 0, len(files))
	for _, doc := range files {
		body := strings.TrimSpace(doc)
		if body == "" {
			continue
		}
		out = append(out, body)
	}
	return out
}

func parseManifestDoc(doc string) (*unstructured.Unstructured, resourceTarget, bool) {
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
		return nil, resourceTarget{}, false
	}
	u := &unstructured.Unstructured{Object: obj}
	gvk := u.GroupVersionKind()
	name := strings.TrimSpace(u.GetName())
	kind := strings.TrimSpace(gvk.Kind)
	if name == "" || kind == "" {
		return nil, resourceTarget{}, false
	}
	target := resourceTarget{
		Group:     gvk.Group,
		Version:   gvk.Version,
		Kind:      kind,
		Namespace: strings.TrimSpace(u.GetNamespace()),
		Name:      name,
	}
	return u, target, true
}

func pickNamespace(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func isHookResource(u *unstructured.Unstructured) bool {
	if u == nil {
		return false
	}
	ann := u.GetAnnotations()
	if len(ann) == 0 {
		return false
	}
	// Helm uses this annotation to mark hooks.
	if v := strings.TrimSpace(ann["helm.sh/hook"]); v != "" {
		return true
	}
	return false
}

func hasHelmOwnership(releaseName string, u *unstructured.Unstructured) bool {
	if u == nil {
		return false
	}
	labels := u.GetLabels()
	if strings.TrimSpace(labels["app.kubernetes.io/managed-by"]) != "Helm" {
		return false
	}
	ann := u.GetAnnotations()
	if strings.TrimSpace(ann["meta.helm.sh/release-name"]) == "" {
		return false
	}
	if strings.TrimSpace(releaseName) != "" && strings.TrimSpace(ann["meta.helm.sh/release-name"]) != strings.TrimSpace(releaseName) {
		return false
	}
	return true
}

func normalizeForDrift(u *unstructured.Unstructured) *unstructured.Unstructured {
	if u == nil {
		return nil
	}
	c := u.DeepCopy()
	// Drop status entirely.
	unstructured.RemoveNestedField(c.Object, "status")

	// Drop volatile metadata fields.
	unstructured.RemoveNestedField(c.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(c.Object, "metadata", "uid")
	unstructured.RemoveNestedField(c.Object, "metadata", "generation")
	unstructured.RemoveNestedField(c.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(c.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	cleanEmptyMetadataMaps(c)

	// Service allocated fields.
	if strings.EqualFold(c.GetKind(), "Service") {
		unstructured.RemoveNestedField(c.Object, "spec", "clusterIP")
		unstructured.RemoveNestedField(c.Object, "spec", "clusterIPs")
		unstructured.RemoveNestedField(c.Object, "spec", "ipFamilies")
		unstructured.RemoveNestedField(c.Object, "spec", "ipFamilyPolicy")
		unstructured.RemoveNestedField(c.Object, "spec", "healthCheckNodePort")
		removeServiceNodePorts(c)
	}
	if strings.EqualFold(c.GetKind(), "Deployment") || strings.EqualFold(c.GetKind(), "StatefulSet") || strings.EqualFold(c.GetKind(), "DaemonSet") {
		unstructured.RemoveNestedField(c.Object, "spec", "revisionHistoryLimit")
	}
	return c
}

func cleanEmptyMetadataMaps(u *unstructured.Unstructured) {
	if u == nil {
		return
	}
	ann, found, _ := unstructured.NestedStringMap(u.Object, "metadata", "annotations")
	if found && len(ann) == 0 {
		unstructured.RemoveNestedField(u.Object, "metadata", "annotations")
	}
	lbl, found, _ := unstructured.NestedStringMap(u.Object, "metadata", "labels")
	if found && len(lbl) == 0 {
		unstructured.RemoveNestedField(u.Object, "metadata", "labels")
	}
}

func removeServiceNodePorts(svc *unstructured.Unstructured) {
	if svc == nil {
		return
	}
	ports, found, _ := unstructured.NestedSlice(svc.Object, "spec", "ports")
	if !found || len(ports) == 0 {
		return
	}
	changed := false
	for i := range ports {
		m, ok := ports[i].(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := m["nodePort"]; ok {
			delete(m, "nodePort")
			changed = true
		}
	}
	if changed {
		_ = unstructured.SetNestedSlice(svc.Object, ports, "spec", "ports")
	}
}

func diffUnstructured(expected *unstructured.Unstructured, actual *unstructured.Unstructured) (bool, string, error) {
	expJSON, err := json.MarshalIndent(expected.Object, "", "  ")
	if err != nil {
		return false, "", fmt.Errorf("marshal expected: %w", err)
	}
	actJSON, err := json.MarshalIndent(actual.Object, "", "  ")
	if err != nil {
		return false, "", fmt.Errorf("marshal actual: %w", err)
	}
	if string(expJSON) == string(actJSON) {
		return true, "", nil
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(expJSON)),
		B:        difflib.SplitLines(string(actJSON)),
		FromFile: "expected",
		ToFile:   "live",
		Context:  2,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return false, "", err
	}
	return false, text, nil
}
