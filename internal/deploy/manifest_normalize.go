// File: internal/deploy/manifest_normalize.go
// Brief: Normalization policies for plan diffing.

package deploy

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type normalizationPolicy struct {
	IgnoreAnnotationPrefixes            []string
	IgnoreAnnotationKeys                []string
	StripEmptyClusterIP                 bool
	StripPodTemplateChecksumAnnotations bool
	NormalizeCommonNamedLists           bool
}

var defaultNormalizationPolicy = normalizationPolicy{
	IgnoreAnnotationPrefixes: []string{"checksum/"},
	IgnoreAnnotationKeys:     []string{"helm.sh/chart"},
	StripEmptyClusterIP:      true,
	// Checksums are almost always derived; strip them under pod templates by default.
	StripPodTemplateChecksumAnnotations: true,
	NormalizeCommonNamedLists:           true,
}

var kindNormalizationPolicies = map[string]normalizationPolicy{
	// Most kinds benefit from defaults; override here when a kind needs special handling.
	"Service": {
		IgnoreAnnotationPrefixes:            defaultNormalizationPolicy.IgnoreAnnotationPrefixes,
		IgnoreAnnotationKeys:                defaultNormalizationPolicy.IgnoreAnnotationKeys,
		StripEmptyClusterIP:                 true,
		StripPodTemplateChecksumAnnotations: false,
		NormalizeCommonNamedLists:           true,
	},
}

func policyForKind(kind string) normalizationPolicy {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return defaultNormalizationPolicy
	}
	if p, ok := kindNormalizationPolicies[kind]; ok {
		return p
	}
	return defaultNormalizationPolicy
}

func normalizeObjectForPlan(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return &unstructured.Unstructured{}
	}
	clone := obj.DeepCopy()
	policy := policyForKind(strings.TrimSpace(clone.GetKind()))

	unstructured.RemoveNestedField(clone.Object, "status")

	normalizeObjectMetadata(clone, policy)

	if policy.StripEmptyClusterIP && strings.EqualFold(strings.TrimSpace(clone.GetKind()), "Service") {
		if clusterIP, found, _ := unstructured.NestedString(clone.Object, "spec", "clusterIP"); found {
			if strings.TrimSpace(clusterIP) == "" {
				unstructured.RemoveNestedField(clone.Object, "spec", "clusterIP")
			}
		}
	}

	if policy.StripPodTemplateChecksumAnnotations {
		normalizePodTemplateAnnotations(clone, policy)
	}

	if policy.NormalizeCommonNamedLists {
		normalizeListOrder(clone.Object)
	}
	return clone
}

func normalizeObjectMetadata(obj *unstructured.Unstructured, policy normalizationPolicy) {
	if obj == nil {
		return
	}
	meta, ok := obj.Object["metadata"].(map[string]interface{})
	if !ok {
		return
	}
	delete(meta, "creationTimestamp")
	delete(meta, "generation")
	delete(meta, "managedFields")
	delete(meta, "resourceVersion")
	delete(meta, "uid")
	delete(meta, "selfLink")
	delete(meta, "finalizers")

	annotations, _ := meta["annotations"].(map[string]interface{})
	if annotations != nil {
		for _, key := range policy.IgnoreAnnotationKeys {
			delete(annotations, key)
		}
		for k := range annotations {
			for _, prefix := range policy.IgnoreAnnotationPrefixes {
				if strings.HasPrefix(k, prefix) {
					delete(annotations, k)
					break
				}
			}
		}
		if len(annotations) == 0 {
			delete(meta, "annotations")
		} else {
			meta["annotations"] = annotations
		}
	}
	obj.Object["metadata"] = meta
}

func normalizePodTemplateAnnotations(obj *unstructured.Unstructured, policy normalizationPolicy) {
	if obj == nil {
		return
	}
	ann, found, _ := unstructured.NestedMap(obj.Object, "spec", "template", "metadata", "annotations")
	if !found || ann == nil {
		return
	}
	changed := false
	for k := range ann {
		for _, prefix := range policy.IgnoreAnnotationPrefixes {
			if strings.HasPrefix(k, prefix) {
				delete(ann, k)
				changed = true
				break
			}
		}
	}
	if !changed {
		return
	}
	if len(ann) == 0 {
		unstructured.RemoveNestedField(obj.Object, "spec", "template", "metadata", "annotations")
		return
	}
	_ = unstructured.SetNestedMap(obj.Object, ann, "spec", "template", "metadata", "annotations")
}
