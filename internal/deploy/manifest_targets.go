// File: internal/deploy/manifest_targets.go
// Brief: Internal deploy package implementation for 'manifest targets'.

// Package deploy provides deploy helpers.

package deploy

import (
	"strings"

	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type resourceTarget struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
	Labels    map[string]string
}

// ManifestTarget is an object reference extracted from a rendered manifest.
// It is intentionally lightweight (GVK + namespace + name + labels).
type ManifestTarget struct {
	Group     string
	Version   string
	Kind      string
	Namespace string
	Name      string
	Labels    map[string]string
}

func targetsFromManifest(manifest string) []resourceTarget {
	trimmed := strings.TrimSpace(manifest)
	if trimmed == "" {
		return nil
	}
	files := releaseutil.SplitManifests(manifest)
	targets := make([]resourceTarget, 0, len(files))
	for _, doc := range files {
		body := strings.TrimSpace(doc)
		if body == "" {
			continue
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(body), &obj); err != nil {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		gvk := u.GroupVersionKind()
		name := strings.TrimSpace(u.GetName())
		if name == "" || strings.TrimSpace(gvk.Kind) == "" {
			continue
		}
		target := resourceTarget{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			Namespace: u.GetNamespace(),
			Name:      name,
		}
		if len(u.GetLabels()) > 0 {
			target.Labels = make(map[string]string, len(u.GetLabels()))
			for k, v := range u.GetLabels() {
				target.Labels[k] = v
			}
		}
		targets = append(targets, target)
	}
	return targets
}

// ManifestTargets extracts object targets from a rendered manifest.
func ManifestTargets(manifest string) []ManifestTarget {
	raw := targetsFromManifest(manifest)
	if len(raw) == 0 {
		return nil
	}
	out := make([]ManifestTarget, 0, len(raw))
	for _, t := range raw {
		mt := ManifestTarget{
			Group:     t.Group,
			Version:   t.Version,
			Kind:      t.Kind,
			Namespace: t.Namespace,
			Name:      t.Name,
		}
		if len(t.Labels) > 0 {
			mt.Labels = make(map[string]string, len(t.Labels))
			for k, v := range t.Labels {
				mt.Labels[k] = v
			}
		}
		out = append(out, mt)
	}
	return out
}
