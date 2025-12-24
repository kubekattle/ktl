// File: internal/stack/merge.go
// Brief: Inheritance and merge rules.

package stack

import (
	"maps"
)

func mergeDefaults(dst *ResolvedRelease, d ReleaseDefaults) {
	if d.Cluster.Name != "" {
		dst.Cluster.Name = d.Cluster.Name
	}
	if d.Cluster.Kubeconfig != "" {
		dst.Cluster.Kubeconfig = d.Cluster.Kubeconfig
	}
	if d.Cluster.Context != "" {
		dst.Cluster.Context = d.Cluster.Context
	}
	if d.Namespace != "" {
		dst.Namespace = d.Namespace
	}
	if len(d.Values) > 0 {
		dst.Values = append(dst.Values, d.Values...)
	}
	if len(d.Tags) > 0 {
		dst.Tags = append(dst.Tags, d.Tags...)
	}
	if d.Set != nil {
		if dst.Set == nil {
			dst.Set = map[string]string{}
		}
		maps.Copy(dst.Set, d.Set)
	}

	mergeApply(&dst.Apply, d.Apply)
	mergeDelete(&dst.Delete, d.Delete)
}

func mergeApply(dst *ApplyOptions, src ApplyOptions) {
	if src.Atomic != nil {
		dst.Atomic = src.Atomic
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.Wait != nil {
		dst.Wait = src.Wait
	}
}

func mergeDelete(dst *DeleteOptions, src DeleteOptions) {
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
}

func mergeReleaseOverride(dst *ResolvedRelease, r ReleaseSpec) {
	if r.Name != "" {
		dst.Name = r.Name
	}
	if r.Chart != "" {
		dst.Chart = r.Chart
	}
	if r.Cluster.Name != "" {
		dst.Cluster.Name = r.Cluster.Name
	}
	if r.Cluster.Kubeconfig != "" {
		dst.Cluster.Kubeconfig = r.Cluster.Kubeconfig
	}
	if r.Cluster.Context != "" {
		dst.Cluster.Context = r.Cluster.Context
	}
	if r.Namespace != "" {
		dst.Namespace = r.Namespace
	}
	if len(r.Values) > 0 {
		dst.Values = append(dst.Values, r.Values...)
	}
	if r.Set != nil {
		if dst.Set == nil {
			dst.Set = map[string]string{}
		}
		for k, v := range r.Set {
			dst.Set[k] = v
		}
	}
	if len(r.Tags) > 0 {
		dst.Tags = append(dst.Tags, r.Tags...)
	}
	if len(r.Needs) > 0 {
		dst.Needs = append([]string(nil), r.Needs...)
	}
	mergeApply(&dst.Apply, r.Apply)
	mergeDelete(&dst.Delete, r.Delete)
}
