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
	dst.Module = mergeModuleSpec(dst.Module, r.Module, r.Input, baseDir)
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
	dst.MySQL = mergeMySQLSpec(dst.MySQL, r.MySQL)
	dst.Postgres = mergePostgresSpec(dst.Postgres, r.Postgres)
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

func mergeModuleSpec(dst ModuleSpec, src ModuleSpec, input map[string]any, baseDir string) ModuleSpec {
	if src.Source != "" {
		dst.Source = strings.TrimSpace(src.Source)
	}
	if src.Version != "" {
		dst.Version = strings.TrimSpace(src.Version)
	}
	if len(src.Command) > 0 {
		dst.Command = append([]string(nil), src.Command...)
	}
	if src.Env != nil {
		dst.Env = maps.Clone(src.Env)
	}
	if src.WorkDir != "" {
		dst.WorkDir = resolvePath(baseDir, src.WorkDir)
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if len(src.Phases) > 0 {
		dst.Phases = append([]string(nil), src.Phases...)
	}
	if src.Input != nil {
		dst.Input = maps.Clone(src.Input)
	} else if len(input) > 0 && len(src.Command) > 0 {
		dst.Input = maps.Clone(input)
	}
	return dst
}

func mergeMySQLSpec(dst MySQLSpec, src MySQLSpec) MySQLSpec {
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
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.NodeIdentityFile != "" {
		dst.NodeIdentityFile = strings.TrimSpace(src.NodeIdentityFile)
	}
	if src.NodeSSHOptions != "" {
		dst.NodeSSHOptions = strings.TrimSpace(src.NodeSSHOptions)
	}
	if len(src.Nodes) > 0 {
		dst.Nodes = append([]MySQLNodeSpec(nil), src.Nodes...)
	}
	if src.ExpectedClusterSize != 0 {
		dst.ExpectedClusterSize = src.ExpectedClusterSize
	}
	if src.ExpectedReplicatedNodes != 0 {
		dst.ExpectedReplicatedNodes = src.ExpectedReplicatedNodes
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.ProbeTable != "" {
		dst.ProbeTable = strings.TrimSpace(src.ProbeTable)
	}
	if src.ProbeID != "" {
		dst.ProbeID = strings.TrimSpace(src.ProbeID)
	}
	if src.ProbePayload != "" {
		dst.ProbePayload = src.ProbePayload
	}
	if src.StatusPath != "" {
		dst.StatusPath = strings.TrimSpace(src.StatusPath)
	}
	if src.InsertProbe {
		dst.InsertProbe = true
	}
	if src.RequireSynced != nil {
		requireSynced := *src.RequireSynced
		dst.RequireSynced = &requireSynced
	}
	if src.StableAttempts != 0 {
		dst.StableAttempts = src.StableAttempts
	}
	if src.StableInterval != nil {
		dst.StableInterval = src.StableInterval
	}
	return dst
}

func mergePostgresSpec(dst PostgresSpec, src PostgresSpec) PostgresSpec {
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
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.Host != "" {
		dst.Host = strings.TrimSpace(src.Host)
	}
	if src.HostEnv != "" {
		dst.HostEnv = strings.TrimSpace(src.HostEnv)
	}
	if src.Port != 0 {
		dst.Port = src.Port
	}
	if src.PortEnv != "" {
		dst.PortEnv = strings.TrimSpace(src.PortEnv)
	}
	if src.User != "" {
		dst.User = strings.TrimSpace(src.User)
	}
	if src.PasswordEnv != "" {
		dst.PasswordEnv = strings.TrimSpace(src.PasswordEnv)
	}
	if src.SSLMode != "" {
		dst.SSLMode = strings.TrimSpace(src.SSLMode)
	}
	if src.PSQLCommand != "" {
		dst.PSQLCommand = strings.TrimSpace(src.PSQLCommand)
	}
	if src.PGDumpCommand != "" {
		dst.PGDumpCommand = strings.TrimSpace(src.PGDumpCommand)
	}
	if src.PGRestoreCommand != "" {
		dst.PGRestoreCommand = strings.TrimSpace(src.PGRestoreCommand)
	}
	if src.RunAsUser != "" {
		dst.RunAsUser = strings.TrimSpace(src.RunAsUser)
	}
	if src.AgentPath != "" {
		dst.AgentPath = strings.TrimSpace(src.AgentPath)
	}
	if src.ExecutionMode != "" {
		dst.ExecutionMode = strings.TrimSpace(src.ExecutionMode)
	}
	dst.Role = mergePostgresRoleSpec(dst.Role, src.Role)
	dst.DatabaseRef = mergePostgresDatabaseSpec(dst.DatabaseRef, src.DatabaseRef)
	dst.Grant = mergePostgresGrantSpec(dst.Grant, src.Grant)
	dst.Schema = mergePostgresSchemaSpec(dst.Schema, src.Schema)
	dst.Extension = mergePostgresExtensionSpec(dst.Extension, src.Extension)
	dst.Replication = mergePostgresReplicationSpec(dst.Replication, src.Replication)
	dst.Backup = mergePostgresBackupSpec(dst.Backup, src.Backup)
	dst.Restore = mergePostgresRestoreSpec(dst.Restore, src.Restore)
	dst.Config = mergePostgresConfigSpec(dst.Config, src.Config)
	dst.Maintenance = mergePostgresMaintenanceSpec(dst.Maintenance, src.Maintenance)
	return dst
}

func mergePostgresRoleSpec(dst PostgresRoleSpec, src PostgresRoleSpec) PostgresRoleSpec {
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.Login != nil {
		v := *src.Login
		dst.Login = &v
	}
	if src.Superuser != nil {
		v := *src.Superuser
		dst.Superuser = &v
	}
	if src.PasswordEnv != "" {
		dst.PasswordEnv = strings.TrimSpace(src.PasswordEnv)
	}
	return dst
}

func mergePostgresDatabaseSpec(dst PostgresDatabaseSpec, src PostgresDatabaseSpec) PostgresDatabaseSpec {
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.Owner != "" {
		dst.Owner = strings.TrimSpace(src.Owner)
	}
	return dst
}

func mergePostgresGrantSpec(dst PostgresGrantSpec, src PostgresGrantSpec) PostgresGrantSpec {
	if src.Role != "" {
		dst.Role = strings.TrimSpace(src.Role)
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.Schema != "" {
		dst.Schema = strings.TrimSpace(src.Schema)
	}
	if src.ObjectType != "" {
		dst.ObjectType = strings.TrimSpace(src.ObjectType)
	}
	if len(src.Privileges) > 0 {
		dst.Privileges = append([]string(nil), src.Privileges...)
	}
	return dst
}

func mergePostgresSchemaSpec(dst PostgresSchemaSpec, src PostgresSchemaSpec) PostgresSchemaSpec {
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.Owner != "" {
		dst.Owner = strings.TrimSpace(src.Owner)
	}
	return dst
}

func mergePostgresExtensionSpec(dst PostgresExtensionSpec, src PostgresExtensionSpec) PostgresExtensionSpec {
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.Schema != "" {
		dst.Schema = strings.TrimSpace(src.Schema)
	}
	return dst
}

func mergePostgresReplicationSpec(dst PostgresReplicationSpec, src PostgresReplicationSpec) PostgresReplicationSpec {
	if src.ExpectedReplicas != 0 {
		dst.ExpectedReplicas = src.ExpectedReplicas
	}
	if src.RequireStreaming {
		dst.RequireStreaming = true
	}
	return dst
}

func mergePostgresBackupSpec(dst PostgresBackupSpec, src PostgresBackupSpec) PostgresBackupSpec {
	if src.ID != "" {
		dst.ID = strings.TrimSpace(src.ID)
	}
	if src.Path != "" {
		dst.Path = strings.TrimSpace(src.Path)
	}
	if src.File != "" {
		dst.File = strings.TrimSpace(src.File)
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.Format != "" {
		dst.Format = strings.TrimSpace(src.Format)
	}
	if src.Compress != 0 {
		dst.Compress = src.Compress
	}
	if src.SimulateDuration != nil {
		dst.SimulateDuration = src.SimulateDuration
	}
	if src.ManifestPath != "" {
		dst.ManifestPath = strings.TrimSpace(src.ManifestPath)
	}
	if src.CatalogPath != "" {
		dst.CatalogPath = strings.TrimSpace(src.CatalogPath)
	}
	if src.ExpectedSha256 != "" {
		dst.ExpectedSha256 = strings.TrimSpace(src.ExpectedSha256)
	}
	dst.Store = mergePostgresBackupStoreSpec(dst.Store, src.Store)
	return dst
}

func mergePostgresBackupStoreSpec(dst PostgresBackupStoreSpec, src PostgresBackupStoreSpec) PostgresBackupStoreSpec {
	if src.Type != "" {
		dst.Type = strings.TrimSpace(src.Type)
	}
	if src.Ref != "" {
		dst.Ref = strings.TrimSpace(src.Ref)
	}
	if src.RefEnv != "" {
		dst.RefEnv = strings.TrimSpace(src.RefEnv)
	}
	if src.Bucket != "" {
		dst.Bucket = strings.TrimSpace(src.Bucket)
	}
	if src.BucketEnv != "" {
		dst.BucketEnv = strings.TrimSpace(src.BucketEnv)
	}
	if src.Prefix != "" {
		dst.Prefix = strings.TrimSpace(src.Prefix)
	}
	if src.PrefixEnv != "" {
		dst.PrefixEnv = strings.TrimSpace(src.PrefixEnv)
	}
	if src.Region != "" {
		dst.Region = strings.TrimSpace(src.Region)
	}
	if src.RegionEnv != "" {
		dst.RegionEnv = strings.TrimSpace(src.RegionEnv)
	}
	if src.Endpoint != "" {
		dst.Endpoint = strings.TrimSpace(src.Endpoint)
	}
	if src.EndpointEnv != "" {
		dst.EndpointEnv = strings.TrimSpace(src.EndpointEnv)
	}
	if src.PathStyle {
		dst.PathStyle = true
	}
	if src.PartSizeBytes != 0 {
		dst.PartSizeBytes = src.PartSizeBytes
	}
	if src.SessionPath != "" {
		dst.SessionPath = strings.TrimSpace(src.SessionPath)
	}
	if src.AccessKeyIDEnv != "" {
		dst.AccessKeyIDEnv = strings.TrimSpace(src.AccessKeyIDEnv)
	}
	if src.SecretAccessKeyEnv != "" {
		dst.SecretAccessKeyEnv = strings.TrimSpace(src.SecretAccessKeyEnv)
	}
	if src.SessionTokenEnv != "" {
		dst.SessionTokenEnv = strings.TrimSpace(src.SessionTokenEnv)
	}
	return dst
}

func mergePostgresRestoreSpec(dst PostgresRestoreSpec, src PostgresRestoreSpec) PostgresRestoreSpec {
	if src.BackupFile != "" {
		dst.BackupFile = strings.TrimSpace(src.BackupFile)
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.VerifySQL != "" {
		dst.VerifySQL = src.VerifySQL
	}
	if src.Expect != "" {
		dst.Expect = strings.TrimSpace(src.Expect)
	}
	if src.Cleanup {
		dst.Cleanup = true
	}
	return dst
}

func mergePostgresConfigSpec(dst PostgresConfigSpec, src PostgresConfigSpec) PostgresConfigSpec {
	if src.Settings != nil {
		dst.Settings = maps.Clone(src.Settings)
	}
	if src.Reload {
		dst.Reload = true
	}
	return dst
}

func mergePostgresMaintenanceSpec(dst PostgresMaintenanceSpec, src PostgresMaintenanceSpec) PostgresMaintenanceSpec {
	if src.Action != "" {
		dst.Action = strings.TrimSpace(src.Action)
	}
	if src.Database != "" {
		dst.Database = strings.TrimSpace(src.Database)
	}
	if src.Table != "" {
		dst.Table = strings.TrimSpace(src.Table)
	}
	return dst
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
	if src.ServiceName != "" {
		dst.ServiceName = strings.TrimSpace(src.ServiceName)
	}
	if src.ServiceManager != "" {
		dst.ServiceManager = strings.TrimSpace(src.ServiceManager)
	}
	if src.Enabled != nil {
		enabled := *src.Enabled
		dst.Enabled = &enabled
	}
	if src.StopOnDelete {
		dst.StopOnDelete = true
	}
	if src.DisableOnDelete {
		dst.DisableOnDelete = true
	}
	if src.UserName != "" {
		dst.UserName = strings.TrimSpace(src.UserName)
	}
	if src.GroupName != "" {
		dst.GroupName = strings.TrimSpace(src.GroupName)
	}
	if src.UserGroup != "" {
		dst.UserGroup = strings.TrimSpace(src.UserGroup)
	}
	if src.UID != nil {
		uid := *src.UID
		dst.UID = &uid
	}
	if src.GID != nil {
		gid := *src.GID
		dst.GID = &gid
	}
	if src.Home != "" {
		dst.Home = strings.TrimSpace(src.Home)
	}
	if src.Shell != "" {
		dst.Shell = strings.TrimSpace(src.Shell)
	}
	if src.Comment != "" {
		dst.Comment = strings.TrimSpace(src.Comment)
	}
	if len(src.Groups) > 0 {
		dst.Groups = append([]string(nil), src.Groups...)
	}
	if src.CreateHome {
		dst.CreateHome = true
	}
	if src.RemoveHome {
		dst.RemoveHome = true
	}
	if src.System {
		dst.System = true
	}
	if src.CronName != "" {
		dst.CronName = strings.TrimSpace(src.CronName)
	}
	if src.CronSchedule != "" {
		dst.CronSchedule = strings.TrimSpace(src.CronSchedule)
	}
	if src.CronUser != "" {
		dst.CronUser = strings.TrimSpace(src.CronUser)
	}
	if src.CronCommand != "" {
		dst.CronCommand = src.CronCommand
	}
	if src.UnitName != "" {
		dst.UnitName = strings.TrimSpace(src.UnitName)
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
	if v := inputString(input, "service"); v != "" {
		dst.ServiceName = v
	}
	if v := inputString(input, "serviceManager"); v != "" {
		dst.ServiceManager = v
	}
	if v, ok := inputBool(input, "enabled"); ok {
		dst.Enabled = &v
	}
	if v, ok := inputBool(input, "stopOnDelete"); ok {
		dst.StopOnDelete = v
	}
	if v, ok := inputBool(input, "disableOnDelete"); ok {
		dst.DisableOnDelete = v
	}
	if v := inputString(input, "user"); v != "" {
		dst.UserName = v
	}
	if v := inputString(input, "groupName"); v != "" {
		dst.GroupName = v
	}
	if v := inputString(input, "userGroup"); v != "" {
		dst.UserGroup = v
	}
	if v, ok := inputInt(input, "uid"); ok {
		dst.UID = &v
	}
	if v, ok := inputInt(input, "gid"); ok {
		dst.GID = &v
	}
	if v := inputString(input, "home"); v != "" {
		dst.Home = v
	}
	if v := inputString(input, "shell"); v != "" {
		dst.Shell = v
	}
	if v := inputString(input, "comment"); v != "" {
		dst.Comment = v
	}
	if v := inputStringSlice(input, "groups"); len(v) > 0 {
		dst.Groups = v
	}
	if v, ok := inputBool(input, "createHome"); ok {
		dst.CreateHome = v
	}
	if v, ok := inputBool(input, "removeHome"); ok {
		dst.RemoveHome = v
	}
	if v, ok := inputBool(input, "system"); ok {
		dst.System = v
	}
	if v := inputString(input, "cronName"); v != "" {
		dst.CronName = v
	}
	if v := inputString(input, "schedule"); v != "" {
		dst.CronSchedule = v
	}
	if v := inputString(input, "cronUser"); v != "" {
		dst.CronUser = v
	}
	if v := inputString(input, "cronCommand"); v != "" {
		dst.CronCommand = v
	}
	if v := inputString(input, "unit"); v != "" {
		dst.UnitName = v
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

func inputInt(input map[string]any, key string) (int, bool) {
	if input == nil {
		return 0, false
	}
	value, ok := input[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func inputStringSlice(input map[string]any, key string) []string {
	if input == nil {
		return nil
	}
	value, ok := input[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := append([]string(nil), typed...)
		for i := range out {
			out[i] = strings.TrimSpace(out[i])
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
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
	dst.Manifest = mergeKubernetesManifestSpec(dst.Manifest, src.Manifest)
	dst.Resource = mergeKubernetesResourceSpec(dst.Resource, src.Resource)
	dst.Logs = mergeKubernetesLogsSpec(dst.Logs, src.Logs)
	dst.Events = mergeKubernetesEventsSpec(dst.Events, src.Events)
	return dst
}

func mergeKubernetesManifestSpec(dst KubernetesManifestSpec, src KubernetesManifestSpec) KubernetesManifestSpec {
	if src.Namespace != "" {
		dst.Namespace = strings.TrimSpace(src.Namespace)
	}
	if src.Content != "" {
		dst.Content = src.Content
	}
	if src.Path != "" {
		dst.Path = strings.TrimSpace(src.Path)
	}
	if src.Template != "" {
		dst.Template = src.Template
	}
	if src.TemplatePath != "" {
		dst.TemplatePath = strings.TrimSpace(src.TemplatePath)
	}
	if len(src.Data) > 0 {
		dst.Data = cloneAnyMap(src.Data)
	}
	if src.FieldManager != "" {
		dst.FieldManager = strings.TrimSpace(src.FieldManager)
	}
	if src.ForceConflicts {
		dst.ForceConflicts = true
	}
	if src.RemoveOnDelete {
		dst.RemoveOnDelete = true
	}
	if src.PrunePolicy != "" {
		dst.PrunePolicy = strings.TrimSpace(src.PrunePolicy)
	}
	return dst
}

func mergeKubernetesResourceSpec(dst KubernetesResourceSpec, src KubernetesResourceSpec) KubernetesResourceSpec {
	if src.Namespace != "" {
		dst.Namespace = strings.TrimSpace(src.Namespace)
	}
	if src.Resource != "" {
		dst.Resource = strings.TrimSpace(src.Resource)
	}
	if src.Kind != "" {
		dst.Kind = strings.TrimSpace(src.Kind)
	}
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.Selector != "" {
		dst.Selector = strings.TrimSpace(src.Selector)
	}
	if src.For != "" {
		dst.For = strings.TrimSpace(src.For)
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.EventLimit != 0 {
		dst.EventLimit = src.EventLimit
	}
	return dst
}

func mergeKubernetesLogsSpec(dst KubernetesLogsSpec, src KubernetesLogsSpec) KubernetesLogsSpec {
	if src.Namespace != "" {
		dst.Namespace = strings.TrimSpace(src.Namespace)
	}
	if src.Resource != "" {
		dst.Resource = strings.TrimSpace(src.Resource)
	}
	if src.Kind != "" {
		dst.Kind = strings.TrimSpace(src.Kind)
	}
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.Selector != "" {
		dst.Selector = strings.TrimSpace(src.Selector)
	}
	if src.Container != "" {
		dst.Container = strings.TrimSpace(src.Container)
	}
	if src.AllContainers {
		dst.AllContainers = true
	}
	if src.Previous {
		dst.Previous = true
	}
	if src.Timestamps {
		dst.Timestamps = true
	}
	if src.Prefix {
		dst.Prefix = true
	}
	if src.Since != nil {
		dst.Since = src.Since
	}
	if src.SinceTime != "" {
		dst.SinceTime = strings.TrimSpace(src.SinceTime)
	}
	if src.TailLines != 0 {
		dst.TailLines = src.TailLines
	}
	if src.LimitBytes != 0 {
		dst.LimitBytes = src.LimitBytes
	}
	if src.MaxLogRequests != 0 {
		dst.MaxLogRequests = src.MaxLogRequests
	}
	return dst
}

func mergeKubernetesEventsSpec(dst KubernetesEventsSpec, src KubernetesEventsSpec) KubernetesEventsSpec {
	if src.Namespace != "" {
		dst.Namespace = strings.TrimSpace(src.Namespace)
	}
	if src.Resource != "" {
		dst.Resource = strings.TrimSpace(src.Resource)
	}
	if src.Kind != "" {
		dst.Kind = strings.TrimSpace(src.Kind)
	}
	if src.Name != "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if src.FieldSelector != "" {
		dst.FieldSelector = strings.TrimSpace(src.FieldSelector)
	}
	if len(src.Types) > 0 {
		dst.Types = normalizeTrimmedStringSlice(src.Types)
	}
	if len(src.Reasons) > 0 {
		dst.Reasons = normalizeTrimmedStringSlice(src.Reasons)
	}
	if src.Since != nil {
		dst.Since = src.Since
	}
	if src.SinceTime != "" {
		dst.SinceTime = strings.TrimSpace(src.SinceTime)
	}
	if src.EventLimit != 0 {
		dst.EventLimit = src.EventLimit
	}
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
	if src.KubectlCommand != "" {
		dst.KubectlCommand = strings.TrimSpace(src.KubectlCommand)
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
