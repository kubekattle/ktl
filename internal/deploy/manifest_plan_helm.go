// File: internal/deploy/manifest_plan_helm.go
// Brief: Helm-aware plan summarization using resource.Info (GVK+name+namespace).

package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/kube"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/resource"
)

func SummarizeManifestPlanWithHelmKube(client *kube.Client, previousManifest, proposedManifest string) (*PlanSummary, error) {
	if client == nil {
		return nil, fmt.Errorf("helm kube client is nil")
	}
	prev, err := buildPlanObjects(client, previousManifest)
	if err != nil {
		return nil, err
	}
	next, err := buildPlanObjects(client, proposedManifest)
	if err != nil {
		return nil, err
	}

	prevByKey := make(map[string]planObject, len(prev))
	prevByAltKey := make(map[string]planObject, len(prev))
	for _, obj := range prev {
		prevByKey[obj.Key] = obj
		if obj.AltKey != "" {
			prevByAltKey[obj.AltKey] = obj
		}
	}
	nextByKey := make(map[string]planObject, len(next))
	nextByAltKey := make(map[string]planObject, len(next))
	for _, obj := range next {
		nextByKey[obj.Key] = obj
		if obj.AltKey != "" {
			nextByAltKey[obj.AltKey] = obj
		}
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

	// Replace detection: correlate by group/kind/name/namespace, then check if the mapped GVK differs.
	if summary.Add > 0 && summary.Destroy > 0 {
		var kept []PlanChange
		seenReplace := make(map[string]bool)
		for _, ch := range summary.Changes {
			switch ch.Action {
			case PlanAdd:
				alt := fmt.Sprintf("%s/%s/%s/%s", strings.ToLower(ch.Group), strings.ToLower(ch.Kind), ch.Namespace, ch.Name)
				prevObj, ok := prevByAltKey[alt]
				if ok {
					nextObj, ok2 := nextByAltKey[alt]
					if ok2 && prevObj.Key != nextObj.Key {
						if !seenReplace[alt] {
							summary.Replace++
							seenReplace[alt] = true
						}
						summary.Add--
						continue
					}
				}
			case PlanDestroy:
				alt := fmt.Sprintf("%s/%s/%s/%s", strings.ToLower(ch.Group), strings.ToLower(ch.Kind), ch.Namespace, ch.Name)
				nextObj, ok := nextByAltKey[alt]
				if ok {
					prevObj, ok2 := prevByAltKey[alt]
					if ok2 && prevObj.Key != nextObj.Key {
						summary.Destroy--
						continue
					}
				}
			}
			kept = append(kept, ch)
		}
		summary.Changes = kept
		for alt := range seenReplace {
			obj := nextByAltKey[alt]
			summary.Changes = append(summary.Changes, obj.toPlanChange(PlanReplace))
		}
	}

	sort.Slice(summary.Changes, func(i, j int) bool {
		a := summary.Changes[i]
		b := summary.Changes[j]
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		if a.Group != b.Group {
			return a.Group < b.Group
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

func ListManifestResourcesWithHelmKube(client *kube.Client, manifest string) ([]PlanChange, error) {
	if client == nil {
		return nil, fmt.Errorf("helm kube client is nil")
	}
	objs, err := buildPlanObjects(client, manifest)
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
		if a.Group != b.Group {
			return a.Group < b.Group
		}
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

type planObject struct {
	Key           string
	AltKey        string
	Group         string
	Version       string
	Kind          string
	Namespace     string
	Name          string
	CanonicalJSON []byte
}

func (p planObject) toPlanChange(action PlanAction) PlanChange {
	return PlanChange{
		Action:    action,
		Group:     p.Group,
		Version:   p.Version,
		Kind:      p.Kind,
		Namespace: p.Namespace,
		Name:      p.Name,
	}
}

func buildPlanObjects(client *kube.Client, manifest string) ([]planObject, error) {
	manifest = strings.TrimSpace(manifest)
	if manifest == "" {
		return nil, nil
	}
	infos, err := client.Build(strings.NewReader(manifest), false)
	if err != nil {
		return nil, fmt.Errorf("helm kube build: %w", err)
	}

	out := make([]planObject, 0, len(infos))
	_ = infos.Visit(func(info *resource.Info, visitErr error) error {
		if visitErr != nil || info == nil || info.Object == nil || info.Mapping == nil {
			return nil
		}
		gvk := info.Mapping.GroupVersionKind
		group := strings.TrimSpace(gvk.Group)
		version := strings.TrimSpace(gvk.Version)
		kind := strings.TrimSpace(gvk.Kind)
		name := strings.TrimSpace(info.Name)
		ns := strings.TrimSpace(info.Namespace)
		if kind == "" || name == "" {
			return nil
		}

		obj, ok := info.Object.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
		normalized := normalizeObjectForPlan(obj)
		encoded, encErr := json.Marshal(normalized.Object)
		if encErr != nil {
			return nil
		}

		key := fmt.Sprintf("%s/%s/%s/%s/%s", strings.ToLower(group), strings.ToLower(version), strings.ToLower(kind), ns, name)
		altKey := fmt.Sprintf("%s/%s/%s/%s", strings.ToLower(group), strings.ToLower(kind), ns, name)
		out = append(out, planObject{
			Key:           key,
			AltKey:        altKey,
			Group:         group,
			Version:       version,
			Kind:          kind,
			Namespace:     ns,
			Name:          name,
			CanonicalJSON: encoded,
		})
		return nil
	})
	return out, nil
}
