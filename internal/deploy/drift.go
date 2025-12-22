package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

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
	if strings.TrimSpace(manifest) == "" || get == nil {
		return DriftReport{}, nil
	}
	files := splitManifestDocs(manifest)
	out := DriftReport{}
	for _, doc := range files {
		base, target, ok := parseManifestDoc(doc)
		if !ok {
			continue
		}
		// Skip hook resources; they are often ephemeral and not reliably comparable.
		if isHookResource(base) {
			continue
		}
		live, err := get(ctx, target)
		if err != nil {
			out.Items = append(out.Items, DriftItem{
				Kind:      target.Kind,
				Namespace: target.Namespace,
				Name:      target.Name,
				Reason:    fmt.Sprintf("unable to fetch live object: %v", err),
			})
			continue
		}
		if live == nil {
			out.Items = append(out.Items, DriftItem{
				Kind:      target.Kind,
				Namespace: target.Namespace,
				Name:      target.Name,
				Reason:    "object missing from cluster",
			})
			continue
		}

		baseNorm := normalizeForDrift(base)
		liveNorm := normalizeForDrift(live)

		eq, diff, err := diffUnstructured(baseNorm, liveNorm)
		if err != nil {
			return DriftReport{}, err
		}
		if !eq {
			out.Items = append(out.Items, DriftItem{
				Kind:      target.Kind,
				Namespace: pickNamespace(target.Namespace, live.GetNamespace()),
				Name:      target.Name,
				Reason:    "object differs from last applied manifest",
				Diff:      diff,
			})
		}
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

	// Service allocated fields.
	if strings.EqualFold(c.GetKind(), "Service") {
		unstructured.RemoveNestedField(c.Object, "spec", "clusterIP")
		unstructured.RemoveNestedField(c.Object, "spec", "clusterIPs")
		unstructured.RemoveNestedField(c.Object, "spec", "ipFamilies")
		unstructured.RemoveNestedField(c.Object, "spec", "ipFamilyPolicy")
		unstructured.RemoveNestedField(c.Object, "spec", "healthCheckNodePort")
	}
	return c
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
