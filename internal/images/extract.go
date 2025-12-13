// extract.go unpacks previously saved image tarballs for reuse in ktl workflows.
package images

import (
	"fmt"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Extract scans rendered Kubernetes manifests and returns every unique container image reference.
func Extract(manifest string) ([]string, error) {
	if strings.TrimSpace(manifest) == "" {
		return nil, fmt.Errorf("manifest is empty")
	}
	docs := releaseutil.SplitManifests(manifest)
	seen := make(map[string]struct{})
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, fmt.Errorf("parse manifest chunk: %w", err)
		}
		u := &unstructured.Unstructured{Object: obj}
		for _, image := range extractFromObject(u) {
			if strings.TrimSpace(image) == "" {
				continue
			}
			seen[image] = struct{}{}
		}
	}
	images := make([]string, 0, len(seen))
	for ref := range seen {
		images = append(images, ref)
	}
	sort.Strings(images)
	return images, nil
}

func extractFromObject(u *unstructured.Unstructured) []string {
	kind := strings.ToLower(u.GetKind())
	switch kind {
	case "pod", "replicationcontroller", "replicationcontrollerlist":
		if spec, ok := u.Object["spec"].(map[string]interface{}); ok {
			return collectFromPodSpec(spec)
		}
	case "deployment", "replicaset", "statefulset", "daemonset", "job":
		if spec := nestedMap(u.Object, "spec", "template", "spec"); spec != nil {
			return collectFromPodSpec(spec)
		}
	case "cronjob":
		if spec := nestedMap(u.Object, "spec", "jobTemplate", "spec", "template", "spec"); spec != nil {
			return collectFromPodSpec(spec)
		}
	default:
		// Attempt generic template lookup
		if spec := nestedMap(u.Object, "spec", "template", "spec"); spec != nil {
			return collectFromPodSpec(spec)
		}
	}
	return nil
}

func nestedMap(obj map[string]interface{}, fields ...string) map[string]interface{} {
	current := obj
	for _, field := range fields {
		val, ok := current[field]
		if !ok {
			return nil
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func collectFromPodSpec(spec map[string]interface{}) []string {
	var images []string
	images = append(images, collectFromContainers(spec, "containers")...)
	images = append(images, collectFromContainers(spec, "initContainers")...)
	images = append(images, collectFromContainers(spec, "ephemeralContainers")...)
	return images
}

func collectFromContainers(spec map[string]interface{}, field string) []string {
	val, ok := spec[field]
	if !ok {
		return nil
	}
	items, ok := val.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		container, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if image, ok := container["image"].(string); ok {
			result = append(result, image)
		}
	}
	return result
}
