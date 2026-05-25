// File: internal/stack/merge.go
// Brief: Inheritance and merge rules.

package stack

import (
	"maps"
	"strings"
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
	if r.Kind != "" {
		dst.Kind = normalizeNodeKind(r.Kind)
	}
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
	if r.Action.Apply != nil {
		apply := *r.Action.Apply
		if apply.WorkDir != "" {
			apply.WorkDir = resolvePath(baseDir, apply.WorkDir)
		}
		dst.Action.Apply = &apply
	}
	if r.Action.Delete != nil {
		del := *r.Action.Delete
		if del.WorkDir != "" {
			del.WorkDir = resolvePath(baseDir, del.WorkDir)
		}
		dst.Action.Delete = &del
	}
	if r.Action.Plugin != nil {
		plugin := cloneActionPluginSpec(*r.Action.Plugin)
		if plugin.WorkDir != "" {
			plugin.WorkDir = resolvePath(baseDir, plugin.WorkDir)
		}
		dst.Action.Plugin = &plugin
	}
	if r.Action.Idempotent {
		dst.Action.Idempotent = true
	}
	if r.Database.Driver != "" {
		dst.Database.Driver = strings.TrimSpace(r.Database.Driver)
	}
	if r.Database.DSN != "" {
		dst.Database.DSN = r.Database.DSN
	}
	if r.Database.DSNEnv != "" {
		dst.Database.DSNEnv = strings.TrimSpace(r.Database.DSNEnv)
	}
	if r.Database.MetadataTable != "" {
		dst.Database.MetadataTable = strings.TrimSpace(r.Database.MetadataTable)
	}
	if r.Database.RestorePointSQL != "" {
		dst.Database.RestorePointSQL = r.Database.RestorePointSQL
	}
	if r.Database.ExpandSQL != "" {
		dst.Database.ExpandSQL = r.Database.ExpandSQL
	}
	if r.Database.ContractSQL != "" {
		dst.Database.ContractSQL = r.Database.ContractSQL
	}
	if r.Database.PrepareSQL != "" {
		dst.Database.PrepareSQL = r.Database.PrepareSQL
	}
	if r.Database.ArmSQL != "" {
		dst.Database.ArmSQL = r.Database.ArmSQL
	}
	if r.Database.CommitSQL != "" {
		dst.Database.CommitSQL = r.Database.CommitSQL
	}
	if r.Database.VerifySQL != "" {
		dst.Database.VerifySQL = r.Database.VerifySQL
	}
	if r.Database.FinalizeSQL != "" {
		dst.Database.FinalizeSQL = r.Database.FinalizeSQL
	}
	if r.Database.VerifyExpectMessage != "" {
		dst.Database.VerifyExpectMessage = strings.TrimSpace(r.Database.VerifyExpectMessage)
	}
	if r.Database.StabilizationWindow != nil {
		dst.Database.StabilizationWindow = r.Database.StabilizationWindow
	}
	if r.Database.RequireFencing != nil {
		dst.Database.RequireFencing = r.Database.RequireFencing
	}
	if r.Database.AmbiguityPolicy != "" {
		dst.Database.AmbiguityPolicy = strings.TrimSpace(r.Database.AmbiguityPolicy)
	}
	if r.Database.Backfill.CheckpointTable != "" {
		dst.Database.Backfill.CheckpointTable = strings.TrimSpace(r.Database.Backfill.CheckpointTable)
	}
	if r.Database.Backfill.CheckpointKey != "" {
		dst.Database.Backfill.CheckpointKey = strings.TrimSpace(r.Database.Backfill.CheckpointKey)
	}
	if r.Database.Backfill.StartSQL != "" {
		dst.Database.Backfill.StartSQL = r.Database.Backfill.StartSQL
	}
	if r.Database.Backfill.EndSQL != "" {
		dst.Database.Backfill.EndSQL = r.Database.Backfill.EndSQL
	}
	if r.Database.Backfill.BatchSQL != "" {
		dst.Database.Backfill.BatchSQL = r.Database.Backfill.BatchSQL
	}
	if r.Database.Backfill.BatchSize > 0 {
		dst.Database.Backfill.BatchSize = r.Database.Backfill.BatchSize
	}
	if r.Database.Backfill.BatchSleep != nil {
		dst.Database.Backfill.BatchSleep = r.Database.Backfill.BatchSleep
	}
	if r.Database.Backfill.MaxBatches > 0 {
		dst.Database.Backfill.MaxBatches = r.Database.Backfill.MaxBatches
	}
	dst.Host = mergeHostCommandSpec(dst.Host, r.Host, r.Input)
	dst.Kubernetes = mergeKubernetesSpec(dst.Kubernetes, r.Kubernetes)
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

func cloneActionPluginSpec(in ActionPluginSpec) ActionPluginSpec {
	out := in
	out.Command = append([]string(nil), in.Command...)
	out.Phases = append([]string(nil), in.Phases...)
	if in.Env != nil {
		out.Env = maps.Clone(in.Env)
	}
	if in.Config != nil {
		out.Config = maps.Clone(in.Config)
	}
	return out
}

func mergeHostCommandSpec(dst HostCommandSpec, src HostCommandSpec, input map[string]any) HostCommandSpec {
	if src.Transport != "" {
		dst.Transport = strings.TrimSpace(src.Transport)
	}
	if src.TargetID != "" {
		dst.TargetID = strings.TrimSpace(src.TargetID)
	}
	if src.Target != "" {
		dst.Target = strings.TrimSpace(src.Target)
	}
	if src.TargetEnv != "" {
		dst.TargetEnv = strings.TrimSpace(src.TargetEnv)
	}
	if src.Command != "" {
		dst.Command = src.Command
	}
	if src.DeleteCommand != "" {
		dst.DeleteCommand = src.DeleteCommand
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.Path != "" {
		dst.Path = strings.TrimSpace(src.Path)
	}
	if src.SourcePath != "" {
		dst.SourcePath = strings.TrimSpace(src.SourcePath)
	}
	if src.Content != "" {
		dst.Content = src.Content
	}
	if src.Template != "" {
		dst.Template = src.Template
	}
	if src.TemplatePath != "" {
		dst.TemplatePath = strings.TrimSpace(src.TemplatePath)
	}
	if src.Data != nil {
		dst.Data = maps.Clone(src.Data)
	}
	if src.Mode != "" {
		dst.Mode = strings.TrimSpace(src.Mode)
	}
	if src.Owner != "" {
		dst.Owner = strings.TrimSpace(src.Owner)
	}
	if src.Group != "" {
		dst.Group = strings.TrimSpace(src.Group)
	}
	if src.Validate != "" {
		dst.Validate = src.Validate
	}
	if src.Backup {
		dst.Backup = true
	}
	if src.BackupPath != "" {
		dst.BackupPath = strings.TrimSpace(src.BackupPath)
	}
	if src.RemoveOnDelete {
		dst.RemoveOnDelete = true
	}
	if src.RestoreOnDelete {
		dst.RestoreOnDelete = true
	}
	if src.PackageName != "" {
		dst.PackageName = strings.TrimSpace(src.PackageName)
	}
	if src.PackageManager != "" {
		dst.PackageManager = strings.TrimSpace(src.PackageManager)
	}
	if src.State != "" {
		dst.State = strings.TrimSpace(src.State)
	}
	if src.Version != "" {
		dst.Version = strings.TrimSpace(src.Version)
	}
	if src.UpdateCache {
		dst.UpdateCache = true
	}
	if src.Purge {
		dst.Purge = true
	}
	if len(input) == 0 {
		return dst
	}
	if v := inputString(input, "transport"); v != "" {
		dst.Transport = v
	}
	if v := firstNonEmptyString(inputString(input, "targetId"), inputString(input, "targetID"), inputString(input, "target_id")); v != "" {
		dst.TargetID = v
	}
	if v := inputString(input, "target"); v != "" {
		dst.Target = v
	}
	if v := inputString(input, "targetEnv"); v != "" {
		dst.TargetEnv = v
	}
	if v := inputString(input, "command"); v != "" {
		dst.Command = v
	}
	if v := inputString(input, "deleteCommand"); v != "" {
		dst.DeleteCommand = v
	}
	if v := inputString(input, "path"); v != "" {
		dst.Path = v
	}
	if v := inputString(input, "sourcePath"); v != "" {
		dst.SourcePath = v
	}
	if v := inputString(input, "content"); v != "" {
		dst.Content = v
	}
	if v := inputString(input, "template"); v != "" {
		dst.Template = v
	}
	if v := inputString(input, "templatePath"); v != "" {
		dst.TemplatePath = v
	}
	if v := inputMap(input, "data"); v != nil {
		dst.Data = v
	}
	if v := inputString(input, "mode"); v != "" {
		dst.Mode = v
	}
	if v := inputString(input, "owner"); v != "" {
		dst.Owner = v
	}
	if v := inputString(input, "group"); v != "" {
		dst.Group = v
	}
	if v := inputString(input, "validate"); v != "" {
		dst.Validate = v
	}
	if v, ok := inputBool(input, "backup"); ok {
		dst.Backup = v
	}
	if v := inputString(input, "backupPath"); v != "" {
		dst.BackupPath = v
	}
	if v, ok := inputBool(input, "removeOnDelete"); ok {
		dst.RemoveOnDelete = v
	}
	if v, ok := inputBool(input, "restoreOnDelete"); ok {
		dst.RestoreOnDelete = v
	}
	if v := inputString(input, "package"); v != "" {
		dst.PackageName = v
	}
	if v := inputString(input, "packageManager"); v != "" {
		dst.PackageManager = v
	}
	if v := inputString(input, "state"); v != "" {
		dst.State = v
	}
	if v := inputString(input, "version"); v != "" {
		dst.Version = v
	}
	if v, ok := inputBool(input, "updateCache"); ok {
		dst.UpdateCache = v
	}
	if v, ok := inputBool(input, "purge"); ok {
		dst.Purge = v
	}
	return dst
}

func inputString(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, ok := input[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func inputMap(input map[string]any, key string) map[string]any {
	if input == nil {
		return nil
	}
	value, ok := input[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return maps.Clone(typed)
	default:
		return nil
	}
}

func inputBool(input map[string]any, key string) (bool, bool) {
	if input == nil {
		return false, false
	}
	value, ok := input[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	default:
		return false, false
	}
}

func mergeKubernetesSpec(dst KubernetesSpec, src KubernetesSpec) KubernetesSpec {
	if src.Provider != "" {
		dst.Provider = strings.TrimSpace(src.Provider)
	}
	dst.Cluster = mergeKubernetesClusterSpec(dst.Cluster, src.Cluster)
	if len(src.Certificates.Targets) > 0 {
		dst.Certificates.Targets = append([]KubernetesCertTarget(nil), src.Certificates.Targets...)
	}
	dst.Certificates.TargetsFrom = mergeKubernetesCertTargetsFromSpec(dst.Certificates.TargetsFrom, src.Certificates.TargetsFrom)
	if src.Certificates.RenewBefore != nil {
		dst.Certificates.RenewBefore = src.Certificates.RenewBefore
	}
	if src.Certificates.Force {
		dst.Certificates.Force = true
	}
	if src.Certificates.ForceOnceID != "" {
		dst.Certificates.ForceOnceID = strings.TrimSpace(src.Certificates.ForceOnceID)
	}
	if src.Certificates.StatePath != "" {
		dst.Certificates.StatePath = strings.TrimSpace(src.Certificates.StatePath)
	}
	if len(src.Certificates.Services) > 0 {
		dst.Certificates.Services = append([]string(nil), src.Certificates.Services...)
	}
	if src.Certificates.Order != "" {
		dst.Certificates.Order = strings.TrimSpace(src.Certificates.Order)
	}
	if src.Certificates.BatchSize != 0 {
		dst.Certificates.BatchSize = src.Certificates.BatchSize
	}
	if src.Certificates.HealthCheckCommand != "" {
		dst.Certificates.HealthCheckCommand = strings.TrimSpace(src.Certificates.HealthCheckCommand)
	}
	if src.Certificates.VerifyCommand != "" {
		dst.Certificates.VerifyCommand = src.Certificates.VerifyCommand
	}
	dst.Certificates.Policy = mergeKubernetesLifecyclePolicySpec(dst.Certificates.Policy, src.Certificates.Policy)
	return dst
}

func mergeKubernetesLifecyclePolicySpec(dst KubernetesLifecyclePolicySpec, src KubernetesLifecyclePolicySpec) KubernetesLifecyclePolicySpec {
	if src.MaxUnavailable != 0 {
		dst.MaxUnavailable = src.MaxUnavailable
	}
	if src.RequireFreshInspect {
		dst.RequireFreshInspect = true
	}
	if src.MaxInspectAge != nil {
		dst.MaxInspectAge = src.MaxInspectAge
	}
	if src.RequireHealthyInspect {
		dst.RequireHealthyInspect = true
	}
	if src.RequireSupportedProvider {
		dst.RequireSupportedProvider = true
	}
	if src.MaintenanceWindow.Start != "" {
		dst.MaintenanceWindow.Start = strings.TrimSpace(src.MaintenanceWindow.Start)
	}
	if src.MaintenanceWindow.End != "" {
		dst.MaintenanceWindow.End = strings.TrimSpace(src.MaintenanceWindow.End)
	}
	if src.MaintenanceWindow.TimeZone != "" {
		dst.MaintenanceWindow.TimeZone = strings.TrimSpace(src.MaintenanceWindow.TimeZone)
	}
	if len(src.MaintenanceWindow.Days) > 0 {
		dst.MaintenanceWindow.Days = append([]string(nil), src.MaintenanceWindow.Days...)
	}
	if len(src.AppProbes) > 0 {
		dst.AppProbes = append([]KubernetesAppProbe(nil), src.AppProbes...)
	}
	dst.Override = mergeKubernetesLifecyclePolicyOverrideSpec(dst.Override, src.Override)
	return dst
}

func mergeKubernetesLifecyclePolicyOverrideSpec(dst KubernetesLifecyclePolicyOverrideSpec, src KubernetesLifecyclePolicyOverrideSpec) KubernetesLifecyclePolicyOverrideSpec {
	if src.Reason != "" {
		dst.Reason = strings.TrimSpace(src.Reason)
	}
	if src.Ticket != "" {
		dst.Ticket = strings.TrimSpace(src.Ticket)
	}
	if src.ChangeID != "" {
		dst.ChangeID = strings.TrimSpace(src.ChangeID)
	}
	if src.Approver != "" {
		dst.Approver = strings.TrimSpace(src.Approver)
	}
	if src.ExpiresAt != "" {
		dst.ExpiresAt = strings.TrimSpace(src.ExpiresAt)
	}
	if src.Scope.NodeID != "" {
		dst.Scope.NodeID = strings.TrimSpace(src.Scope.NodeID)
	}
	if src.Scope.RunID != "" {
		dst.Scope.RunID = strings.TrimSpace(src.Scope.RunID)
	}
	if src.Scope.IntentDigest != "" {
		dst.Scope.IntentDigest = strings.TrimSpace(src.Scope.IntentDigest)
	}
	if len(src.Scope.TargetIDs) > 0 {
		dst.Scope.TargetIDs = append([]string(nil), src.Scope.TargetIDs...)
	}
	if src.Scope.TargetSetDigest != "" {
		dst.Scope.TargetSetDigest = strings.TrimSpace(src.Scope.TargetSetDigest)
	}
	return dst
}

func mergeKubernetesCertTargetsFromSpec(dst KubernetesCertTargetsFromSpec, src KubernetesCertTargetsFromSpec) KubernetesCertTargetsFromSpec {
	if src.SourceNode != "" {
		dst.SourceNode = strings.TrimSpace(src.SourceNode)
	}
	if src.SourceNodeID != "" {
		dst.SourceNodeID = strings.TrimSpace(src.SourceNodeID)
	}
	if src.Artifact != "" {
		dst.Artifact = strings.TrimSpace(src.Artifact)
	}
	if len(src.Roles) > 0 {
		dst.Roles = append([]string(nil), src.Roles...)
	}
	if src.IncludeNotReady {
		dst.IncludeNotReady = true
	}
	if src.AddressType != "" {
		dst.AddressType = strings.TrimSpace(src.AddressType)
	}
	if src.Transport != "" {
		dst.Transport = strings.TrimSpace(src.Transport)
	}
	if src.Target != "" {
		dst.Target = strings.TrimSpace(src.Target)
	}
	if src.TargetEnv != "" {
		dst.TargetEnv = strings.TrimSpace(src.TargetEnv)
	}
	if src.TargetTemplate != "" {
		dst.TargetTemplate = strings.TrimSpace(src.TargetTemplate)
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.Provider != "" {
		dst.Provider = strings.TrimSpace(src.Provider)
	}
	if src.Service != "" {
		dst.Service = strings.TrimSpace(src.Service)
	}
	if src.NodeAddressTemplate != "" {
		dst.NodeAddressTemplate = strings.TrimSpace(src.NodeAddressTemplate)
	}
	if src.NodeIdentityFile != "" {
		dst.NodeIdentityFile = strings.TrimSpace(src.NodeIdentityFile)
	}
	if src.NodeSSHOptions != "" {
		dst.NodeSSHOptions = strings.TrimSpace(src.NodeSSHOptions)
	}
	if src.InspectCommand != "" {
		dst.InspectCommand = strings.TrimSpace(src.InspectCommand)
	}
	if src.RenewCommand != "" {
		dst.RenewCommand = strings.TrimSpace(src.RenewCommand)
	}
	if src.RestartCommand != "" {
		dst.RestartCommand = strings.TrimSpace(src.RestartCommand)
	}
	return dst
}

func mergeKubernetesClusterSpec(dst KubernetesClusterSpec, src KubernetesClusterSpec) KubernetesClusterSpec {
	if src.Transport != "" {
		dst.Transport = strings.TrimSpace(src.Transport)
	}
	if src.Target != "" {
		dst.Target = strings.TrimSpace(src.Target)
	}
	if src.TargetEnv != "" {
		dst.TargetEnv = strings.TrimSpace(src.TargetEnv)
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.Kubeconfig != "" {
		dst.Kubeconfig = strings.TrimSpace(src.Kubeconfig)
	}
	if src.KubeconfigEnv != "" {
		dst.KubeconfigEnv = strings.TrimSpace(src.KubeconfigEnv)
	}
	if src.Context != "" {
		dst.Context = strings.TrimSpace(src.Context)
	}
	if src.MinReadyNodes != 0 {
		dst.MinReadyNodes = src.MinReadyNodes
	}
	if len(src.Namespaces) > 0 {
		dst.Namespaces = append([]string(nil), src.Namespaces...)
	}
	if src.StableIterations != 0 {
		dst.StableIterations = src.StableIterations
	}
	if src.StableInterval != nil {
		dst.StableInterval = src.StableInterval
	}
	if src.ConfigCommand != "" {
		dst.ConfigCommand = strings.TrimSpace(src.ConfigCommand)
	}
	if src.APICommand != "" {
		dst.APICommand = strings.TrimSpace(src.APICommand)
	}
	if src.NodesCommand != "" {
		dst.NodesCommand = strings.TrimSpace(src.NodesCommand)
	}
	if src.NamespacesCommand != "" {
		dst.NamespacesCommand = strings.TrimSpace(src.NamespacesCommand)
	}
	if src.PodsCommand != "" {
		dst.PodsCommand = strings.TrimSpace(src.PodsCommand)
	}
	if len(src.AppProbes) > 0 {
		dst.AppProbes = append([]KubernetesAppProbe(nil), src.AppProbes...)
	}
	return dst
}
