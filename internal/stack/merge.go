// File: internal/stack/merge.go
// Brief: Inheritance and merge rules.

package stack

import (
	"maps"
)

func mergeDefaults(dst *ResolvedRelease, baseDir string, d ReleaseDefaults) {
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
		dst.Values = append(dst.Values, resolvePaths(baseDir, d.Values)...)
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
	mergeVerify(&dst.Verify, d.Verify)
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
	if src.CreateNamespace != nil {
		dst.CreateNamespace = src.CreateNamespace
	}
}

func mergeDelete(dst *DeleteOptions, src DeleteOptions) {
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
}

func mergeVerify(dst *VerifyOptions, src VerifyOptions) {
	if src.Enabled != nil {
		dst.Enabled = src.Enabled
	}
	if src.FailOnWarnings != nil {
		dst.FailOnWarnings = src.FailOnWarnings
	}
	if src.WarnOnly != nil {
		dst.WarnOnly = src.WarnOnly
	}
	if src.EventsWindow != nil {
		dst.EventsWindow = src.EventsWindow
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if len(src.DenyReasons) > 0 {
		dst.DenyReasons = append([]string(nil), src.DenyReasons...)
	}
	if len(src.AllowReasons) > 0 {
		dst.AllowReasons = append([]string(nil), src.AllowReasons...)
	}
	if len(src.RequireConditions) > 0 {
		dst.RequireConditions = append([]VerifyConditionRequirement(nil), src.RequireConditions...)
	}
}

func mergeReleaseOverride(dst *ResolvedRelease, baseDir string, r ReleaseSpec) {
	if r.Name != "" {
		dst.Name = r.Name
	}
	if r.Chart != "" {
		dst.Chart = resolvePath(baseDir, r.Chart)
	}
	if r.ChartVersion != "" {
		dst.ChartVersion = r.ChartVersion
	}
	if r.Wave != 0 {
		dst.Wave = r.Wave
	}
	if r.Critical {
		dst.Critical = true
	}
	if r.Parallelism != "" {
		dst.Parallelism = r.Parallelism
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
		dst.Values = append(dst.Values, resolvePaths(baseDir, r.Values)...)
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
	mergeHooks(dst, baseDir, r.Hooks)
	mergeApply(&dst.Apply, r.Apply)
	mergeDelete(&dst.Delete, r.Delete)
	mergeVerify(&dst.Verify, r.Verify)
}

func resolvePaths(baseDir string, vals []string) []string {
	if len(vals) == 0 {
		return nil
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		out = append(out, resolvePath(baseDir, v))
	}
	return out
}
