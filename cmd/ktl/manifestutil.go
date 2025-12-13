// manifestutil.go hosts helper types for parsing/rendering Helm manifests when generating deploy plans and diffs.
package main

import (
	"fmt"
	"strings"

	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type resourceKey struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version,omitempty"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

func (k resourceKey) String() string {
	scope := k.Namespace
	if scope == "" {
		scope = "cluster"
	}
	group := k.Group
	if group == "" {
		group = "core"
	}
	return fmt.Sprintf("%s/%s %s (%s)", scope, k.Name, k.Kind, group)
}

type manifestDoc struct {
	Key            resourceKey
	Body           string
	Obj            *unstructured.Unstructured
	TemplateSource string
}

// parseManifestDocs converts a Helm manifest blob into structured entries.
func parseManifestDocs(manifest string) []manifestDoc {
	files := releaseutil.SplitManifests(manifest)
	docs := make([]manifestDoc, 0, len(files))
	for name, doc := range files {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			continue
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(trimmed), &obj); err != nil {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		docs = append(docs, manifestDoc{
			Key:            toResourceKey(u),
			Body:           trimmed,
			Obj:            u,
			TemplateSource: pickTemplateSource(trimmed, name),
		})
	}
	return docs
}

func pickTemplateSource(manifestBody, fallback string) string {
	lines := strings.Split(manifestBody, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# Source:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# Source:"))
		}
		break
	}
	return fallback
}

func toResourceKey(obj *unstructured.Unstructured) resourceKey {
	group := ""
	version := ""
	parts := strings.SplitN(obj.GetAPIVersion(), "/", 2)
	if len(parts) == 2 {
		group = parts[0]
		version = parts[1]
	} else if len(parts) == 1 {
		version = parts[0]
	}
	return resourceKey{
		Group:     group,
		Version:   version,
		Kind:      obj.GetKind(),
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

// splitManifests retains the legacy map[string]string behavior used by ktl app package.
func splitManifests(manifest string) map[string]string {
	files := releaseutil.SplitManifests(manifest)
	result := make(map[string]string, len(files))
	for name, doc := range files {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" {
			continue
		}
		key := manifestKey(name, trimmed)
		result[key] = trimmed
	}
	return result
}

func manifestKey(defaultName, doc string) string {
	meta := manifestMeta(doc)
	kind := strings.ToLower(meta.Kind)
	name := meta.Name
	ns := meta.Namespace
	if kind == "" || name == "" {
		return defaultName
	}
	if ns != "" {
		return fmt.Sprintf("%s-%s-%s", kind, ns, name)
	}
	return fmt.Sprintf("%s-%s", kind, name)
}

func manifestMeta(doc string) struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
} {
	type meta struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Namespace string `yaml:"namespace"`
			Name      string `yaml:"name"`
		} `yaml:"metadata"`
	}
	var m meta
	_ = yaml.Unmarshal([]byte(doc), &m)
	return struct {
		APIVersion string
		Kind       string
		Namespace  string
		Name       string
	}{
		APIVersion: m.APIVersion,
		Kind:       m.Kind,
		Namespace:  m.Metadata.Namespace,
		Name:       m.Metadata.Name,
	}
}
