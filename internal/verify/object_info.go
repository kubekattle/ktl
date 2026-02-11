package verify

import "fmt"

type objectInfo struct {
	obj         map[string]any
	subject     Subject
	labels      map[string]string
	annotations map[string]string
}

func buildObjectInfos(objects []map[string]any) []objectInfo {
	infos := make([]objectInfo, 0, len(objects))
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		sub := subjectFromObject(obj)
		infos = append(infos, objectInfo{
			obj:         obj,
			subject:     sub,
			labels:      labelsFromObject(obj),
			annotations: annotationsFromObject(obj),
		})
	}
	return infos
}

func subjectFromObject(obj map[string]any) Subject {
	sub := Subject{}
	if v, ok := obj["kind"]; ok && v != nil {
		sub.Kind = fmt.Sprintf("%v", v)
	}
	if meta, ok := obj["metadata"].(map[string]any); ok && meta != nil {
		if v, ok := meta["name"]; ok && v != nil {
			sub.Name = fmt.Sprintf("%v", v)
		}
		if v, ok := meta["namespace"]; ok && v != nil {
			sub.Namespace = fmt.Sprintf("%v", v)
		}
	}
	return sub
}

func labelsFromObject(obj map[string]any) map[string]string {
	meta, ok := obj["metadata"].(map[string]any)
	if !ok || meta == nil {
		return nil
	}
	labelsRaw, ok := meta["labels"].(map[string]any)
	if !ok || labelsRaw == nil {
		return nil
	}
	out := make(map[string]string, len(labelsRaw))
	for k, v := range labelsRaw {
		key := fmt.Sprintf("%v", k)
		if key == "" {
			continue
		}
		out[key] = fmt.Sprintf("%v", v)
	}
	return out
}

func annotationsFromObject(obj map[string]any) map[string]string {
	meta, ok := obj["metadata"].(map[string]any)
	if !ok || meta == nil {
		return nil
	}
	annRaw, ok := meta["annotations"].(map[string]any)
	if !ok || annRaw == nil {
		return nil
	}
	out := make(map[string]string, len(annRaw))
	for k, v := range annRaw {
		key := fmt.Sprintf("%v", k)
		if key == "" {
			continue
		}
		out[key] = fmt.Sprintf("%v", v)
	}
	return out
}
