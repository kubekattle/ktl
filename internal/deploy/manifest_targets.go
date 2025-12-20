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
