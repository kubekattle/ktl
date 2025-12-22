// File: internal/deploy/manifest_plan.go
// Brief: Manifest diff summarization for Terraform-like plan output.

package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type PlanAction string

const (
	PlanAdd     PlanAction = "add"
	PlanUpdate  PlanAction = "change"
	PlanDestroy PlanAction = "destroy"
)

type PlanChange struct {
	Action    PlanAction
	Kind      string
	Namespace string
	Name      string
}

type PlanSummary struct {
	Add     int
	Change  int
	Destroy int
	Changes []PlanChange
}

func ListManifestResources(manifest string) ([]PlanChange, error) {
	objs, err := parseManifestObjects(manifest)
	if err != nil {
		return nil, err
	}
	out := make([]PlanChange, 0, len(objs))
	for _, obj := range objs {
		out = append(out, obj.toPlanChange(PlanDestroy))
	}
	sort.Slice(out, func(i, j int) bool {
		a := out[i]
		b := out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return out, nil
}

func SummarizeManifestPlan(previousManifest, proposedManifest string) (*PlanSummary, error) {
	prev, err := parseManifestObjects(previousManifest)
	if err != nil {
		return nil, err
	}
	next, err := parseManifestObjects(proposedManifest)
	if err != nil {
		return nil, err
	}

	prevByKey := make(map[string]manifestObject, len(prev))
	for _, obj := range prev {
		prevByKey[obj.Key] = obj
	}
	nextByKey := make(map[string]manifestObject, len(next))
	for _, obj := range next {
		nextByKey[obj.Key] = obj
	}

	var summary PlanSummary
	for key, obj := range nextByKey {
		prevObj, ok := prevByKey[key]
		if !ok {
			summary.Add++
			summary.Changes = append(summary.Changes, obj.toPlanChange(PlanAdd))
			continue
		}
		if !bytes.Equal(prevObj.CanonicalJSON, obj.CanonicalJSON) {
			summary.Change++
			summary.Changes = append(summary.Changes, obj.toPlanChange(PlanUpdate))
		}
	}
	for key, obj := range prevByKey {
		if _, ok := nextByKey[key]; ok {
			continue
		}
		summary.Destroy++
		summary.Changes = append(summary.Changes, obj.toPlanChange(PlanDestroy))
	}

	sort.Slice(summary.Changes, func(i, j int) bool {
		a := summary.Changes[i]
		b := summary.Changes[j]
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
	return &summary, nil
}

type manifestObject struct {
	Key           string
	Kind          string
	Namespace     string
	Name          string
	CanonicalJSON []byte
}

func (m manifestObject) toPlanChange(action PlanAction) PlanChange {
	return PlanChange{
		Action:    action,
		Kind:      m.Kind,
		Namespace: m.Namespace,
		Name:      m.Name,
	}
}

func parseManifestObjects(manifest string) ([]manifestObject, error) {
	manifest = strings.TrimSpace(manifest)
	if manifest == "" {
		return nil, nil
	}

	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []manifestObject

	for {
		var raw map[string]interface{}
		err := dec.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode manifest: %w", err)
		}
		if len(raw) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{Object: raw}
		kind := strings.TrimSpace(obj.GetKind())
		name := strings.TrimSpace(obj.GetName())
		if kind == "" || name == "" {
			continue
		}
		ns := strings.TrimSpace(obj.GetNamespace())
		key := fmt.Sprintf("%s/%s/%s", strings.ToLower(kind), ns, name)

		normalized := normalizeObjectForPlan(obj)
		encoded, err := json.Marshal(normalized.Object)
		if err != nil {
			return nil, fmt.Errorf("encode manifest object: %w", err)
		}
		out = append(out, manifestObject{
			Key:           key,
			Kind:          kind,
			Namespace:     ns,
			Name:          name,
			CanonicalJSON: encoded,
		})
	}

	return out, nil
}

func normalizeObjectForPlan(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return &unstructured.Unstructured{}
	}
	clone := obj.DeepCopy()

	unstructured.RemoveNestedField(clone.Object, "status")

	if meta, ok := clone.Object["metadata"].(map[string]interface{}); ok {
		delete(meta, "creationTimestamp")
		delete(meta, "generation")
		delete(meta, "managedFields")
		delete(meta, "resourceVersion")
		delete(meta, "uid")
		delete(meta, "selfLink")
		delete(meta, "finalizers")
		clone.Object["metadata"] = meta
	}

	return clone
}
