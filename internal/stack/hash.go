// File: internal/stack/hash.go
// Brief: Effective input hashing for drift detection and resume safety.

package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/version"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
)

type EffectiveInputHashOptions struct {
	StackRoot             string
	IncludeValuesContents bool
	StackGitIdentity      *GitIdentity
}

type EffectiveActionInput struct {
	Idempotent   bool   `json:"idempotent"`
	ApplyDigest  string `json:"applyDigest,omitempty"`
	DeleteDigest string `json:"deleteDigest,omitempty"`
	PluginDigest string `json:"pluginDigest,omitempty"`
	Digest       string `json:"digest,omitempty"`
}

type EffectiveDatabaseInput struct {
	Driver              string                 `json:"driver,omitempty"`
	DSNRef              string                 `json:"dsnRef,omitempty"`
	MetadataTable       string                 `json:"metadataTable,omitempty"`
	RestorePointDigest  string                 `json:"restorePointDigest,omitempty"`
	ExpandDigest        string                 `json:"expandDigest,omitempty"`
	ContractDigest      string                 `json:"contractDigest,omitempty"`
	PrepareDigest       string                 `json:"prepareDigest,omitempty"`
	ArmDigest           string                 `json:"armDigest,omitempty"`
	CommitDigest        string                 `json:"commitDigest,omitempty"`
	VerifyDigest        string                 `json:"verifyDigest,omitempty"`
	FinalizeDigest      string                 `json:"finalizeDigest,omitempty"`
	VerifyExpectMessage string                 `json:"verifyExpectMessage,omitempty"`
	Backfill            EffectiveBackfillInput `json:"backfill,omitempty"`
	StabilizationWindow string                 `json:"stabilizationWindow,omitempty"`
	RequireFencing      bool                   `json:"requireFencing,omitempty"`
	AmbiguityPolicy     string                 `json:"ambiguityPolicy,omitempty"`
	Digest              string                 `json:"digest,omitempty"`
}

type EffectiveHostInput struct {
	Transport          string   `json:"transport,omitempty"`
	TargetID           string   `json:"targetId,omitempty"`
	TargetDigest       string   `json:"targetDigest,omitempty"`
	TargetEnv          string   `json:"targetEnv,omitempty"`
	CommandDigest      string   `json:"commandDigest,omitempty"`
	DeleteDigest       string   `json:"deleteDigest,omitempty"`
	PathDigest         string   `json:"pathDigest,omitempty"`
	SourcePathDigest   string   `json:"sourcePathDigest,omitempty"`
	SourceDigest       string   `json:"sourceDigest,omitempty"`
	ContentDigest      string   `json:"contentDigest,omitempty"`
	TemplateDigest     string   `json:"templateDigest,omitempty"`
	TemplatePathDigest string   `json:"templatePathDigest,omitempty"`
	DataDigest         string   `json:"dataDigest,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	Owner              string   `json:"owner,omitempty"`
	Group              string   `json:"group,omitempty"`
	ValidateDigest     string   `json:"validateDigest,omitempty"`
	Backup             bool     `json:"backup,omitempty"`
	BackupPathDigest   string   `json:"backupPathDigest,omitempty"`
	RemoveOnDelete     bool     `json:"removeOnDelete,omitempty"`
	RestoreOnDelete    bool     `json:"restoreOnDelete,omitempty"`
	PackageDigest      string   `json:"packageDigest,omitempty"`
	PackageManager     string   `json:"packageManager,omitempty"`
	State              string   `json:"state,omitempty"`
	VersionDigest      string   `json:"versionDigest,omitempty"`
	UpdateCache        bool     `json:"updateCache,omitempty"`
	Purge              bool     `json:"purge,omitempty"`
	ServiceDigest      string   `json:"serviceDigest,omitempty"`
	ServiceManager     string   `json:"serviceManager,omitempty"`
	Enabled            *bool    `json:"enabled,omitempty"`
	StopOnDelete       bool     `json:"stopOnDelete,omitempty"`
	DisableOnDelete    bool     `json:"disableOnDelete,omitempty"`
	UserDigest         string   `json:"userDigest,omitempty"`
	GroupNameDigest    string   `json:"groupNameDigest,omitempty"`
	UserGroupDigest    string   `json:"userGroupDigest,omitempty"`
	UID                *int     `json:"uid,omitempty"`
	GID                *int     `json:"gid,omitempty"`
	HomeDigest         string   `json:"homeDigest,omitempty"`
	ShellDigest        string   `json:"shellDigest,omitempty"`
	CommentDigest      string   `json:"commentDigest,omitempty"`
	Groups             []string `json:"groups,omitempty"`
	CreateHome         bool     `json:"createHome,omitempty"`
	RemoveHome         bool     `json:"removeHome,omitempty"`
	System             bool     `json:"system,omitempty"`
	CronNameDigest     string   `json:"cronNameDigest,omitempty"`
	CronScheduleDigest string   `json:"cronScheduleDigest,omitempty"`
	CronUserDigest     string   `json:"cronUserDigest,omitempty"`
	CronCommandDigest  string   `json:"cronCommandDigest,omitempty"`
	UnitNameDigest     string   `json:"unitNameDigest,omitempty"`
	Timeout            string   `json:"timeout,omitempty"`
	Digest             string   `json:"digest,omitempty"`
}

type EffectiveKubernetesInput struct {
	Provider            string   `json:"provider,omitempty"`
	ClusterDigest       string   `json:"clusterDigest,omitempty"`
	ManifestDigest      string   `json:"manifestDigest,omitempty"`
	TargetDigests       []string `json:"targetDigests,omitempty"`
	RenewBefore         string   `json:"renewBefore,omitempty"`
	Force               bool     `json:"force,omitempty"`
	ForceOnceID         string   `json:"forceOnceId,omitempty"`
	StatePathDigest     string   `json:"statePathDigest,omitempty"`
	Services            []string `json:"services,omitempty"`
	Order               string   `json:"order,omitempty"`
	BatchSize           int      `json:"batchSize,omitempty"`
	HealthDigest        string   `json:"healthDigest,omitempty"`
	VerifyCommandDigest string   `json:"verifyCommandDigest,omitempty"`
	PolicyDigest        string   `json:"policyDigest,omitempty"`
	Digest              string   `json:"digest,omitempty"`
}

type EffectiveBackfillInput struct {
	CheckpointTable string `json:"checkpointTable,omitempty"`
	CheckpointKey   string `json:"checkpointKey,omitempty"`
	StartDigest     string `json:"startDigest,omitempty"`
	EndDigest       string `json:"endDigest,omitempty"`
	BatchDigest     string `json:"batchDigest,omitempty"`
	BatchSize       int64  `json:"batchSize,omitempty"`
	BatchSleep      string `json:"batchSleep,omitempty"`
	MaxBatches      int    `json:"maxBatches,omitempty"`
}

func ComputeEffectiveInputHash(stackRoot string, n *ResolvedRelease, includeValuesContents bool) (string, *EffectiveInput, error) {
	return ComputeEffectiveInputHashWithOptions(n, EffectiveInputHashOptions{
		StackRoot:             stackRoot,
		IncludeValuesContents: includeValuesContents,
	})
}

func ComputeEffectiveInputHashWithOptions(n *ResolvedRelease, opts EffectiveInputHashOptions) (string, *EffectiveInput, error) {
	stackRoot := strings.TrimSpace(opts.StackRoot)
	if stackRoot == "" {
		stackRoot = "."
	}
	if n == nil {
		return "", nil, fmt.Errorf("nil node")
	}

	gid := GitIdentity{}
	if opts.StackGitIdentity != nil {
		gid = *opts.StackGitIdentity
	} else {
		var err error
		gid, err = GitIdentityForRoot(stackRoot)
		if err != nil {
			return "", nil, err
		}
	}

	values, err := digestValues(n.Values, opts.IncludeValuesContents)
	if err != nil {
		return "", nil, err
	}

	settings := cli.New()

	setDigest := digestSet(n.Set)
	clusterDigest := digestCluster(n)
	apply := digestApply(n)
	deleteInput := digestDelete(n)
	verify := digestVerify(n)

	input := &EffectiveInput{
		APIVersion: "torque.dev/stack-effective-input/v1",

		StackGitCommit: gid.Commit,
		StackGitDirty:  gid.Dirty,

		TorqueVersion:   version.Version,
		TorqueGitCommit: version.GitCommit,

		NodeID: n.ID,
		Kind:   normalizeNodeKind(n.Kind),

		Values: values,

		SetDigest:     setDigest,
		ClusterDigest: clusterDigest,

		Apply:  apply,
		Delete: deleteInput,
		Verify: verify,
	}
	switch normalizeNodeKind(n.Kind) {
	case NodeKindHelm:
		chartInput, err := digestChart(n.Chart, n.ChartVersion, settings)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(n.ChartVersion) == "" && strings.TrimSpace(chartInput.ResolvedVersion) != "" && !isExistingPath(n.Chart) {
			n.ChartVersion = chartInput.ResolvedVersion
			chartInput.Version = n.ChartVersion
		}
		input.Chart = chartInput
	case NodeKindAction, NodeKindActionPlugin:
		actionInput, err := digestActionSpec(n.Action)
		if err != nil {
			return "", nil, err
		}
		input.ActionDigest = actionInput.Digest
	case NodeKindDBRestorePoint, NodeKindDBSchemaExpand, NodeKindDBBackfill, NodeKindDBVerify, NodeKindDBCutover, NodeKindDBSchemaContract:
		dbInput, err := digestDatabaseSpec(n.Database)
		if err != nil {
			return "", nil, err
		}
		input.DatabaseDigest = dbInput.Digest
	case NodeKindHostCommandRun, NodeKindHostFileRender, NodeKindHostFileCopy, NodeKindHostPackageInstall, NodeKindHostServiceManage, NodeKindHostUserManage, NodeKindHostCronManage, NodeKindHostSystemdUnit:
		hostInput, err := digestHostCommandSpec(n.Host)
		if err != nil {
			return "", nil, err
		}
		if normalizeNodeKind(n.Kind) == NodeKindHostFileCopy {
			if err := addHostFileCopySourceDigest(n, stackRoot, &hostInput); err != nil {
				return "", nil, err
			}
		}
		input.HostDigest = hostInput.Digest
	case NodeKindK8sCertInspect, NodeKindK8sCertRenew:
		kubernetesInput, err := digestKubernetesSpec(n.Kubernetes)
		if err != nil {
			return "", nil, err
		}
		input.KubernetesDigest = kubernetesInput.Digest
	case NodeKindK8sClusterInspect, NodeKindK8sClusterVerify, NodeKindK8sManifestApply, NodeKindK8sManifestDelete:
		kubernetesInput, err := digestKubernetesSpec(n.Kubernetes)
		if err != nil {
			return "", nil, err
		}
		input.KubernetesDigest = kubernetesInput.Digest
	}

	type effectiveInputHashV1 struct {
		APIVersion string `json:"apiVersion"`

		StackGitCommit string `json:"stackGitCommit,omitempty"`
		StackGitDirty  bool   `json:"stackGitDirty,omitempty"`

		TorqueVersion   string `json:"torqueVersion,omitempty"`
		TorqueGitCommit string `json:"torqueGitCommit,omitempty"`

		NodeID string `json:"nodeId"`
		Kind   string `json:"kind,omitempty"`

		ChartDigest          string `json:"chartDigest,omitempty"`
		ChartVersion         string `json:"chartVersion,omitempty"`
		ChartResolvedVersion string `json:"chartResolvedVersion,omitempty"`

		ValuesDigests []string `json:"valuesDigests,omitempty"`

		SetDigest        string `json:"setDigest,omitempty"`
		ClusterDigest    string `json:"clusterDigest,omitempty"`
		ActionDigest     string `json:"actionDigest,omitempty"`
		DatabaseDigest   string `json:"databaseDigest,omitempty"`
		HostDigest       string `json:"hostDigest,omitempty"`
		KubernetesDigest string `json:"kubernetesDigest,omitempty"`

		Apply  EffectiveApplyInput  `json:"apply"`
		Delete EffectiveDeleteInput `json:"delete"`
		Verify EffectiveVerifyInput `json:"verify"`
	}

	valuesDigests := make([]string, 0, len(input.Values))
	for _, v := range input.Values {
		valuesDigests = append(valuesDigests, v.Digest)
	}

	hashInput := effectiveInputHashV1{
		APIVersion: "torque.dev/stack-effective-input-hash/v1",

		StackGitCommit: input.StackGitCommit,
		StackGitDirty:  input.StackGitDirty,

		TorqueVersion:   input.TorqueVersion,
		TorqueGitCommit: input.TorqueGitCommit,

		NodeID: input.NodeID,
		Kind:   input.Kind,

		ChartDigest:          input.Chart.Digest,
		ChartVersion:         input.Chart.Version,
		ChartResolvedVersion: input.Chart.ResolvedVersion,

		ValuesDigests: valuesDigests,

		SetDigest:        input.SetDigest,
		ClusterDigest:    input.ClusterDigest,
		ActionDigest:     input.ActionDigest,
		DatabaseDigest:   input.DatabaseDigest,
		HostDigest:       input.HostDigest,
		KubernetesDigest: input.KubernetesDigest,

		Apply:  input.Apply,
		Delete: input.Delete,
		Verify: input.Verify,
	}

	raw, err := json.Marshal(hashInput)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), input, nil
}

func isLocalPath(p string) bool {
	// Values in stack v1 are filesystem paths; keep an escape hatch anyway.
	return p != "" && !strings.Contains(p, "://")
}

func isExistingPath(p string) bool {
	pp := strings.TrimSpace(p)
	if pp == "" {
		return false
	}
	if _, err := os.Stat(pp); err == nil {
		return true
	}
	return false
}

func digestValues(paths []string, includeContents bool) ([]FileDigest, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]FileDigest, 0, len(paths))
	for _, p := range paths {
		d := FileDigest{Path: p}
		if includeContents && isLocalPath(p) {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("read values file %s: %w", p, err)
			}
			sum := sha256.Sum256(b)
			d.Digest = "sha256:" + hex.EncodeToString(sum[:])
		}
		out = append(out, d)
	}
	return out, nil
}

func digestChart(chartRef string, chartVersion string, settings *cli.EnvSettings) (EffectiveChartInput, error) {
	ref := strings.TrimSpace(chartRef)
	if ref == "" {
		return EffectiveChartInput{}, fmt.Errorf("chart ref is required")
	}
	v := strings.TrimSpace(chartVersion)

	chartPath := ref
	if !isExistingPath(ref) {
		cpo := action.ChartPathOptions{Version: v}
		located, err := cpo.LocateChart(ref, settings)
		if err != nil {
			return EffectiveChartInput{}, fmt.Errorf("locate chart %s: %w", ref, err)
		}
		chartPath = located
	}

	ch, err := loader.Load(chartPath)
	if err != nil {
		return EffectiveChartInput{}, fmt.Errorf("load chart %s: %w", chartPath, err)
	}

	resolvedVersion := ""
	if ch.Metadata != nil {
		resolvedVersion = strings.TrimSpace(ch.Metadata.Version)
	}

	return EffectiveChartInput{
		Ref:             ref,
		Version:         v,
		ResolvedVersion: resolvedVersion,
		Digest:          digestHelmChart(ch),
	}, nil
}

func digestActionSpec(spec ActionSpec) (EffectiveActionInput, error) {
	input := EffectiveActionInput{Idempotent: spec.Idempotent}
	if spec.Apply != nil {
		input.ApplyDigest = digestScriptHookConfig(*spec.Apply)
	}
	if spec.Delete != nil {
		input.DeleteDigest = digestScriptHookConfig(*spec.Delete)
	}
	if spec.Plugin != nil {
		input.PluginDigest = digestActionPluginSpec(*spec.Plugin)
	}
	sum, err := hashJSONStable(input)
	if err != nil {
		return EffectiveActionInput{}, err
	}
	input.Digest = sum
	return input, nil
}

func digestDatabaseSpec(spec DatabaseSpec) (EffectiveDatabaseInput, error) {
	input := EffectiveDatabaseInput{
		Driver:              strings.TrimSpace(spec.Driver),
		MetadataTable:       strings.TrimSpace(spec.MetadataTable),
		RestorePointDigest:  digestString(spec.RestorePointSQL),
		ExpandDigest:        digestString(spec.ExpandSQL),
		ContractDigest:      digestString(spec.ContractSQL),
		PrepareDigest:       digestString(spec.PrepareSQL),
		ArmDigest:           digestString(spec.ArmSQL),
		CommitDigest:        digestString(spec.CommitSQL),
		VerifyDigest:        digestString(spec.VerifySQL),
		FinalizeDigest:      digestString(spec.FinalizeSQL),
		VerifyExpectMessage: strings.TrimSpace(spec.VerifyExpectMessage),
		AmbiguityPolicy:     strings.TrimSpace(spec.AmbiguityPolicy),
		Backfill: EffectiveBackfillInput{
			CheckpointTable: strings.TrimSpace(spec.Backfill.CheckpointTable),
			CheckpointKey:   strings.TrimSpace(spec.Backfill.CheckpointKey),
			StartDigest:     digestString(spec.Backfill.StartSQL),
			EndDigest:       digestString(spec.Backfill.EndSQL),
			BatchDigest:     digestString(spec.Backfill.BatchSQL),
			BatchSize:       spec.Backfill.BatchSize,
			MaxBatches:      spec.Backfill.MaxBatches,
		},
	}
	if spec.StabilizationWindow != nil {
		input.StabilizationWindow = spec.StabilizationWindow.String()
	}
	if spec.Backfill.BatchSleep != nil {
		input.Backfill.BatchSleep = spec.Backfill.BatchSleep.String()
	}
	if spec.RequireFencing != nil {
		input.RequireFencing = *spec.RequireFencing
	}
	if strings.TrimSpace(spec.DSNEnv) != "" {
		input.DSNRef = "env:" + strings.TrimSpace(spec.DSNEnv)
	} else if strings.TrimSpace(spec.DSN) != "" {
		input.DSNRef = "sha256:" + hashBytes([]byte(spec.DSN))
	}
	sum, err := hashJSONStable(input)
	if err != nil {
		return EffectiveDatabaseInput{}, err
	}
	input.Digest = sum
	return input, nil
}

func digestHostCommandSpec(spec HostCommandSpec) (EffectiveHostInput, error) {
	input := EffectiveHostInput{
		Transport:          strings.TrimSpace(spec.Transport),
		TargetID:           strings.TrimSpace(spec.TargetID),
		TargetEnv:          strings.TrimSpace(spec.TargetEnv),
		CommandDigest:      digestString(spec.Command),
		DeleteDigest:       digestString(spec.DeleteCommand),
		PathDigest:         digestString(spec.Path),
		SourcePathDigest:   digestString(spec.SourcePath),
		ContentDigest:      digestString(spec.Content),
		TemplateDigest:     digestString(spec.Template),
		TemplatePathDigest: digestString(spec.TemplatePath),
		Mode:               strings.TrimSpace(spec.Mode),
		Owner:              strings.TrimSpace(spec.Owner),
		Group:              strings.TrimSpace(spec.Group),
		ValidateDigest:     digestString(spec.Validate),
		Backup:             spec.Backup,
		BackupPathDigest:   digestString(spec.BackupPath),
		RemoveOnDelete:     spec.RemoveOnDelete,
		RestoreOnDelete:    spec.RestoreOnDelete,
		PackageDigest:      digestString(spec.PackageName),
		PackageManager:     strings.TrimSpace(spec.PackageManager),
		State:              strings.TrimSpace(spec.State),
		VersionDigest:      digestString(spec.Version),
		UpdateCache:        spec.UpdateCache,
		Purge:              spec.Purge,
		ServiceDigest:      digestString(spec.ServiceName),
		ServiceManager:     strings.TrimSpace(spec.ServiceManager),
		Enabled:            spec.Enabled,
		StopOnDelete:       spec.StopOnDelete,
		DisableOnDelete:    spec.DisableOnDelete,
		UserDigest:         digestString(spec.UserName),
		GroupNameDigest:    digestString(spec.GroupName),
		UserGroupDigest:    digestString(spec.UserGroup),
		UID:                spec.UID,
		GID:                spec.GID,
		HomeDigest:         digestString(spec.Home),
		ShellDigest:        digestString(spec.Shell),
		CommentDigest:      digestString(spec.Comment),
		Groups:             append([]string(nil), spec.Groups...),
		CreateHome:         spec.CreateHome,
		RemoveHome:         spec.RemoveHome,
		System:             spec.System,
		CronNameDigest:     digestString(spec.CronName),
		CronScheduleDigest: digestString(spec.CronSchedule),
		CronUserDigest:     digestString(spec.CronUser),
		CronCommandDigest:  digestString(spec.CronCommand),
		UnitNameDigest:     digestString(spec.UnitName),
	}
	if strings.TrimSpace(spec.Target) != "" {
		input.TargetDigest = digestString(spec.Target)
	}
	if spec.Data != nil {
		sum, err := hashJSONStable(spec.Data)
		if err != nil {
			return EffectiveHostInput{}, err
		}
		input.DataDigest = sum
	}
	if spec.Timeout != nil {
		input.Timeout = spec.Timeout.String()
	}
	sum, err := hashJSONStable(input)
	if err != nil {
		return EffectiveHostInput{}, err
	}
	input.Digest = sum
	return input, nil
}

func addHostFileCopySourceDigest(n *ResolvedRelease, stackRoot string, input *EffectiveHostInput) error {
	if n == nil || input == nil || strings.TrimSpace(n.Host.SourcePath) == "" {
		return nil
	}
	sourcePath := strings.TrimSpace(n.Host.SourcePath)
	if !filepath.IsAbs(sourcePath) {
		base := strings.TrimSpace(n.Dir)
		if base == "" {
			base = stackRoot
		}
		sourcePath = filepath.Join(base, sourcePath)
	}
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read host.file.copy source for digest: %w", err)
	}
	input.SourceDigest = "sha256:" + hashBytes(raw)
	sum, err := hashJSONStable(input)
	if err != nil {
		return err
	}
	input.Digest = sum
	return nil
}

func digestKubernetesSpec(spec KubernetesSpec) (EffectiveKubernetesInput, error) {
	input := EffectiveKubernetesInput{
		Provider:            strings.TrimSpace(spec.Provider),
		Force:               spec.Certificates.Force,
		ForceOnceID:         strings.TrimSpace(spec.Certificates.ForceOnceID),
		Services:            append([]string(nil), spec.Certificates.Services...),
		Order:               strings.TrimSpace(spec.Certificates.Order),
		BatchSize:           spec.Certificates.BatchSize,
		HealthDigest:        digestString(spec.Certificates.HealthCheckCommand),
		VerifyCommandDigest: digestString(spec.Certificates.VerifyCommand),
	}
	if targetsFromDigest, err := digestKubernetesCertTargetsFromSpec(spec.Certificates.TargetsFrom); err != nil {
		return EffectiveKubernetesInput{}, err
	} else if targetsFromDigest != "" {
		input.TargetDigests = append(input.TargetDigests, targetsFromDigest)
	}
	if policyDigest, err := digestKubernetesLifecyclePolicySpec(spec.Certificates.Policy); err != nil {
		return EffectiveKubernetesInput{}, err
	} else if policyDigest != "" {
		input.PolicyDigest = policyDigest
	}
	clusterInput, err := digestKubernetesClusterSpec(spec.Cluster)
	if err != nil {
		return EffectiveKubernetesInput{}, err
	}
	input.ClusterDigest = clusterInput
	manifestInput, err := digestKubernetesManifestSpec(spec.Manifest)
	if err != nil {
		return EffectiveKubernetesInput{}, err
	}
	input.ManifestDigest = manifestInput
	if spec.Certificates.RenewBefore != nil {
		input.RenewBefore = spec.Certificates.RenewBefore.String()
	}
	if strings.TrimSpace(spec.Certificates.StatePath) != "" {
		input.StatePathDigest = digestString(spec.Certificates.StatePath)
	}
	for _, target := range spec.Certificates.Targets {
		targetHash := struct {
			ID                string `json:"id,omitempty"`
			Role              string `json:"role,omitempty"`
			Provider          string `json:"provider,omitempty"`
			Transport         string `json:"transport,omitempty"`
			TargetDigest      string `json:"targetDigest,omitempty"`
			TargetEnv         string `json:"targetEnv,omitempty"`
			Service           string `json:"service,omitempty"`
			NodeAddressDigest string `json:"nodeAddressDigest,omitempty"`
			NodeIdentityFile  string `json:"nodeIdentityFile,omitempty"`
			NodeSSHOptions    string `json:"nodeSshOptions,omitempty"`
			InspectDigest     string `json:"inspectDigest,omitempty"`
			RenewDigest       string `json:"renewDigest,omitempty"`
			RestartDigest     string `json:"restartDigest,omitempty"`
			Timeout           string `json:"timeout,omitempty"`
		}{
			ID:               strings.TrimSpace(target.ID),
			Role:             strings.TrimSpace(target.Role),
			Provider:         strings.TrimSpace(target.Provider),
			Transport:        strings.TrimSpace(target.Transport),
			TargetEnv:        strings.TrimSpace(target.TargetEnv),
			Service:          strings.TrimSpace(target.Service),
			NodeIdentityFile: strings.TrimSpace(target.NodeIdentityFile),
			NodeSSHOptions:   strings.TrimSpace(target.NodeSSHOptions),
			InspectDigest:    digestString(target.InspectCommand),
			RenewDigest:      digestString(target.RenewCommand),
			RestartDigest:    digestString(target.RestartCommand),
		}
		if strings.TrimSpace(target.Target) != "" {
			targetHash.TargetDigest = digestString(target.Target)
		}
		if strings.TrimSpace(target.NodeAddress) != "" {
			targetHash.NodeAddressDigest = digestString(target.NodeAddress)
		}
		if target.Timeout != nil {
			targetHash.Timeout = target.Timeout.String()
		}
		raw, err := json.Marshal(targetHash)
		if err != nil {
			return EffectiveKubernetesInput{}, err
		}
		input.TargetDigests = append(input.TargetDigests, "sha256:"+hashBytes(raw))
	}
	sum, err := hashJSONStable(input)
	if err != nil {
		return EffectiveKubernetesInput{}, err
	}
	input.Digest = sum
	return input, nil
}

func digestKubernetesCertTargetsFromSpec(spec KubernetesCertTargetsFromSpec) (string, error) {
	if strings.TrimSpace(spec.SourceNode) == "" && strings.TrimSpace(spec.SourceNodeID) == "" {
		return "", nil
	}
	input := struct {
		SourceNode                string   `json:"sourceNode,omitempty"`
		SourceNodeID              string   `json:"sourceNodeId,omitempty"`
		Artifact                  string   `json:"artifact,omitempty"`
		Roles                     []string `json:"roles,omitempty"`
		IncludeNotReady           bool     `json:"includeNotReady,omitempty"`
		AddressType               string   `json:"addressType,omitempty"`
		Transport                 string   `json:"transport,omitempty"`
		TargetDigest              string   `json:"targetDigest,omitempty"`
		TargetEnv                 string   `json:"targetEnv,omitempty"`
		TargetTemplateDigest      string   `json:"targetTemplateDigest,omitempty"`
		Timeout                   string   `json:"timeout,omitempty"`
		Provider                  string   `json:"provider,omitempty"`
		Service                   string   `json:"service,omitempty"`
		NodeAddressTemplateDigest string   `json:"nodeAddressTemplateDigest,omitempty"`
		NodeIdentityFile          string   `json:"nodeIdentityFile,omitempty"`
		NodeSSHOptions            string   `json:"nodeSshOptions,omitempty"`
		InspectDigest             string   `json:"inspectDigest,omitempty"`
		RenewDigest               string   `json:"renewDigest,omitempty"`
		RestartDigest             string   `json:"restartDigest,omitempty"`
	}{
		SourceNode:       strings.TrimSpace(spec.SourceNode),
		SourceNodeID:     strings.TrimSpace(spec.SourceNodeID),
		Artifact:         strings.TrimSpace(spec.Artifact),
		Roles:            append([]string(nil), spec.Roles...),
		IncludeNotReady:  spec.IncludeNotReady,
		AddressType:      strings.TrimSpace(spec.AddressType),
		Transport:        strings.TrimSpace(spec.Transport),
		TargetEnv:        strings.TrimSpace(spec.TargetEnv),
		Provider:         strings.TrimSpace(spec.Provider),
		Service:          strings.TrimSpace(spec.Service),
		NodeIdentityFile: strings.TrimSpace(spec.NodeIdentityFile),
		NodeSSHOptions:   strings.TrimSpace(spec.NodeSSHOptions),
		InspectDigest:    digestString(spec.InspectCommand),
		RenewDigest:      digestString(spec.RenewCommand),
		RestartDigest:    digestString(spec.RestartCommand),
	}
	if strings.TrimSpace(spec.Target) != "" {
		input.TargetDigest = digestString(spec.Target)
	}
	if strings.TrimSpace(spec.TargetTemplate) != "" {
		input.TargetTemplateDigest = digestString(spec.TargetTemplate)
	}
	if strings.TrimSpace(spec.NodeAddressTemplate) != "" {
		input.NodeAddressTemplateDigest = digestString(spec.NodeAddressTemplate)
	}
	if spec.Timeout != nil {
		input.Timeout = spec.Timeout.String()
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return "sha256:" + hashBytes(raw), nil
}

func digestKubernetesLifecyclePolicySpec(spec KubernetesLifecyclePolicySpec) (string, error) {
	if !kubernetesLifecyclePolicyConfigured(spec) {
		return "", nil
	}
	type appProbeInput struct {
		ID            string `json:"id,omitempty"`
		CommandDigest string `json:"commandDigest,omitempty"`
		ExpectDigest  string `json:"expectDigest,omitempty"`
	}
	input := struct {
		MaxUnavailable           int    `json:"maxUnavailable,omitempty"`
		RequireFreshInspect      bool   `json:"requireFreshInspect,omitempty"`
		MaxInspectAge            string `json:"maxInspectAge,omitempty"`
		RequireHealthyInspect    bool   `json:"requireHealthyInspect,omitempty"`
		RequireSupportedProvider bool   `json:"requireSupportedProvider,omitempty"`
		MaintenanceWindow        struct {
			Start    string   `json:"start,omitempty"`
			End      string   `json:"end,omitempty"`
			TimeZone string   `json:"timeZone,omitempty"`
			Days     []string `json:"days,omitempty"`
		} `json:"maintenanceWindow,omitempty"`
		AppProbes []appProbeInput `json:"appProbes,omitempty"`
	}{
		MaxUnavailable:           spec.MaxUnavailable,
		RequireFreshInspect:      spec.RequireFreshInspect,
		RequireHealthyInspect:    spec.RequireHealthyInspect,
		RequireSupportedProvider: spec.RequireSupportedProvider,
	}
	if spec.MaxInspectAge != nil {
		input.MaxInspectAge = spec.MaxInspectAge.String()
	}
	input.MaintenanceWindow.Start = strings.TrimSpace(spec.MaintenanceWindow.Start)
	input.MaintenanceWindow.End = strings.TrimSpace(spec.MaintenanceWindow.End)
	input.MaintenanceWindow.TimeZone = strings.TrimSpace(spec.MaintenanceWindow.TimeZone)
	input.MaintenanceWindow.Days = append([]string(nil), spec.MaintenanceWindow.Days...)
	for _, probe := range spec.AppProbes {
		input.AppProbes = append(input.AppProbes, appProbeInput{
			ID:            strings.TrimSpace(probe.ID),
			CommandDigest: digestString(probe.Command),
			ExpectDigest:  digestString(probe.Expect),
		})
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return "sha256:" + hashBytes(raw), nil
}

func digestKubernetesClusterSpec(spec KubernetesClusterSpec) (string, error) {
	type appProbeInput struct {
		ID            string `json:"id,omitempty"`
		CommandDigest string `json:"commandDigest,omitempty"`
		ExpectDigest  string `json:"expectDigest,omitempty"`
	}
	input := struct {
		Transport         string          `json:"transport,omitempty"`
		TargetDigest      string          `json:"targetDigest,omitempty"`
		TargetEnv         string          `json:"targetEnv,omitempty"`
		Timeout           string          `json:"timeout,omitempty"`
		KubeconfigDigest  string          `json:"kubeconfigDigest,omitempty"`
		KubeconfigEnv     string          `json:"kubeconfigEnv,omitempty"`
		Context           string          `json:"context,omitempty"`
		KubectlCommand    string          `json:"kubectlCommand,omitempty"`
		MinReadyNodes     int             `json:"minReadyNodes,omitempty"`
		Namespaces        []string        `json:"namespaces,omitempty"`
		StableIterations  int             `json:"stableIterations,omitempty"`
		StableInterval    string          `json:"stableInterval,omitempty"`
		ConfigCommand     string          `json:"configCommand,omitempty"`
		APICommand        string          `json:"apiCommand,omitempty"`
		NodesCommand      string          `json:"nodesCommand,omitempty"`
		NamespacesCommand string          `json:"namespacesCommand,omitempty"`
		PodsCommand       string          `json:"podsCommand,omitempty"`
		AppProbes         []appProbeInput `json:"appProbes,omitempty"`
	}{
		Transport:         strings.TrimSpace(spec.Transport),
		TargetEnv:         strings.TrimSpace(spec.TargetEnv),
		KubeconfigEnv:     strings.TrimSpace(spec.KubeconfigEnv),
		Context:           strings.TrimSpace(spec.Context),
		KubectlCommand:    strings.TrimSpace(spec.KubectlCommand),
		MinReadyNodes:     spec.MinReadyNodes,
		Namespaces:        append([]string(nil), spec.Namespaces...),
		StableIterations:  spec.StableIterations,
		ConfigCommand:     digestString(spec.ConfigCommand),
		APICommand:        digestString(spec.APICommand),
		NodesCommand:      digestString(spec.NodesCommand),
		NamespacesCommand: digestString(spec.NamespacesCommand),
		PodsCommand:       digestString(spec.PodsCommand),
	}
	if strings.TrimSpace(spec.Target) != "" {
		input.TargetDigest = digestString(spec.Target)
	}
	if strings.TrimSpace(spec.Kubeconfig) != "" {
		input.KubeconfigDigest = digestString(spec.Kubeconfig)
	}
	if spec.Timeout != nil {
		input.Timeout = spec.Timeout.String()
	}
	if spec.StableInterval != nil {
		input.StableInterval = spec.StableInterval.String()
	}
	for _, probe := range spec.AppProbes {
		input.AppProbes = append(input.AppProbes, appProbeInput{
			ID:            strings.TrimSpace(probe.ID),
			CommandDigest: digestString(probe.Command),
			ExpectDigest:  digestString(probe.Expect),
		})
	}
	return hashJSONStable(input)
}

func digestKubernetesManifestSpec(spec KubernetesManifestSpec) (string, error) {
	input := struct {
		Namespace          string `json:"namespace,omitempty"`
		ContentDigest      string `json:"contentDigest,omitempty"`
		PathDigest         string `json:"pathDigest,omitempty"`
		TemplateDigest     string `json:"templateDigest,omitempty"`
		TemplatePathDigest string `json:"templatePathDigest,omitempty"`
		DataDigest         string `json:"dataDigest,omitempty"`
		FieldManager       string `json:"fieldManager,omitempty"`
		ForceConflicts     bool   `json:"forceConflicts,omitempty"`
		RemoveOnDelete     bool   `json:"removeOnDelete,omitempty"`
		PrunePolicy        string `json:"prunePolicy,omitempty"`
	}{
		Namespace:          strings.TrimSpace(spec.Namespace),
		ContentDigest:      digestString(spec.Content),
		PathDigest:         digestString(spec.Path),
		TemplateDigest:     digestString(spec.Template),
		TemplatePathDigest: digestString(spec.TemplatePath),
		FieldManager:       strings.TrimSpace(spec.FieldManager),
		ForceConflicts:     spec.ForceConflicts,
		RemoveOnDelete:     spec.RemoveOnDelete,
		PrunePolicy:        strings.TrimSpace(spec.PrunePolicy),
	}
	if len(spec.Data) > 0 {
		raw, err := json.Marshal(spec.Data)
		if err != nil {
			return "", err
		}
		input.DataDigest = "sha256:" + hashBytes(raw)
	}
	return hashJSONStable(input)
}

func digestScriptHookConfig(cfg ScriptHookConfig) string {
	type scriptHash struct {
		Command []string          `json:"command,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
		WorkDir string            `json:"workDir,omitempty"`
	}
	raw, _ := json.Marshal(scriptHash{
		Command: append([]string(nil), cfg.Command...),
		Env:     cfg.Env,
		WorkDir: strings.TrimSpace(cfg.WorkDir),
	})
	return "sha256:" + hashBytes(raw)
}

func digestActionPluginSpec(cfg ActionPluginSpec) string {
	type pluginHash struct {
		Command []string          `json:"command,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
		WorkDir string            `json:"workDir,omitempty"`
		Timeout string            `json:"timeout,omitempty"`
		Phases  []string          `json:"phases,omitempty"`
		Config  map[string]any    `json:"config,omitempty"`
	}
	timeout := ""
	if cfg.Timeout != nil {
		timeout = cfg.Timeout.String()
	}
	raw, _ := json.Marshal(pluginHash{
		Command: append([]string(nil), cfg.Command...),
		Env:     cfg.Env,
		WorkDir: strings.TrimSpace(cfg.WorkDir),
		Timeout: timeout,
		Phases:  normalizedActionPluginPhasesForHash(cfg.Phases),
		Config:  cfg.Config,
	})
	return "sha256:" + hashBytes(raw)
}

func digestString(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return "sha256:" + hashBytes([]byte(v))
}

func hashJSONStable(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return "sha256:" + hashBytes(raw), nil
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func digestHelmChart(ch *chart.Chart) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("torque.stack-chart.v1")
	if ch == nil {
		return "sha256:" + hex.EncodeToString(h.Sum(nil))
	}
	if ch.Metadata != nil {
		write("name:" + ch.Metadata.Name)
		write("version:" + ch.Metadata.Version)
		write("apiVersion:" + ch.Metadata.APIVersion)
	}
	files := append([]*chart.File(nil), ch.Raw...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		write(f.Name)
		write(hex.EncodeToString(sum[:]))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestSet(m map[string]string) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("torque.stack-set.v1")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k)
		write(m[k])
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestCluster(n *ResolvedRelease) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("torque.stack-cluster.v1")
	write(n.Cluster.Name)
	write(n.Cluster.Context)
	write(n.Namespace)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func digestApply(n *ResolvedRelease) EffectiveApplyInput {
	timeout := 5 * time.Minute
	if n.Apply.Timeout != nil {
		timeout = *n.Apply.Timeout
	}
	wait := true
	if n.Apply.Wait != nil {
		wait = *n.Apply.Wait
	}
	atomic := true
	if n.Apply.Atomic != nil {
		atomic = *n.Apply.Atomic
	}
	createNamespace := false
	if n.Apply.CreateNamespace != nil {
		createNamespace = *n.Apply.CreateNamespace
	}

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("torque.stack-apply.v1")
	write(fmt.Sprintf("atomic=%t", atomic))
	write(fmt.Sprintf("wait=%t", wait))
	write(fmt.Sprintf("createNamespace=%t", createNamespace))
	write("timeout=" + timeout.String())

	return EffectiveApplyInput{
		Atomic:          atomic,
		Wait:            wait,
		CreateNamespace: createNamespace,
		Timeout:         timeout.String(),
		Digest:          "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
}

func digestDelete(n *ResolvedRelease) EffectiveDeleteInput {
	timeout := 5 * time.Minute
	if n.Delete.Timeout != nil {
		timeout = *n.Delete.Timeout
	}
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("torque.stack-delete.v1")
	write("timeout=" + timeout.String())
	return EffectiveDeleteInput{
		Timeout: timeout.String(),
		Digest:  "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
}

func digestVerify(n *ResolvedRelease) EffectiveVerifyInput {
	enabled := false
	if n.Verify.Enabled != nil {
		enabled = *n.Verify.Enabled
	}
	failOnWarnings := true
	if n.Verify.FailOnWarnings != nil {
		failOnWarnings = *n.Verify.FailOnWarnings
	}
	warnOnly := false
	if n.Verify.WarnOnly != nil {
		warnOnly = *n.Verify.WarnOnly
	}
	window := 15 * time.Minute
	if n.Verify.EventsWindow != nil && *n.Verify.EventsWindow > 0 {
		window = *n.Verify.EventsWindow
	}
	timeout := 2 * time.Minute
	if n.Verify.Timeout != nil && *n.Verify.Timeout > 0 {
		timeout = *n.Verify.Timeout
	}

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("torque.stack-verify.v1")
	write(fmt.Sprintf("enabled=%t", enabled))
	write(fmt.Sprintf("failOnWarnings=%t", failOnWarnings))
	write(fmt.Sprintf("warnOnly=%t", warnOnly))
	write("eventsWindow=" + window.String())
	write("timeout=" + timeout.String())
	if len(n.Verify.DenyReasons) > 0 {
		for _, r := range n.Verify.DenyReasons {
			write("denyReason=" + strings.ToLower(strings.TrimSpace(r)))
		}
	}
	if len(n.Verify.AllowReasons) > 0 {
		for _, r := range n.Verify.AllowReasons {
			write("allowReason=" + strings.ToLower(strings.TrimSpace(r)))
		}
	}
	if len(n.Verify.RequireConditions) > 0 {
		// Keep deterministic order.
		reqs := append([]VerifyConditionRequirement(nil), n.Verify.RequireConditions...)
		sort.Slice(reqs, func(i, j int) bool {
			a := strings.ToLower(reqs[i].Group) + "/" + strings.ToLower(reqs[i].Kind) + "/" + strings.ToLower(reqs[i].ConditionType)
			b := strings.ToLower(reqs[j].Group) + "/" + strings.ToLower(reqs[j].Kind) + "/" + strings.ToLower(reqs[j].ConditionType)
			if a != b {
				return a < b
			}
			return strings.ToLower(reqs[i].RequireStatus) < strings.ToLower(reqs[j].RequireStatus)
		})
		for _, r := range reqs {
			write("req=" + strings.ToLower(strings.TrimSpace(r.Group)) + "/" + strings.ToLower(strings.TrimSpace(r.Kind)))
			write("cond=" + strings.ToLower(strings.TrimSpace(r.ConditionType)) + "=" + strings.ToLower(strings.TrimSpace(r.RequireStatus)))
			write(fmt.Sprintf("allowMissing=%t", r.AllowMissing))
		}
	}

	return EffectiveVerifyInput{
		Enabled:        enabled,
		FailOnWarnings: failOnWarnings,
		WarnOnly:       warnOnly,
		EventsWindow:   window.String(),
		Timeout:        timeout.String(),
		DenyReasons:    append([]string(nil), n.Verify.DenyReasons...),
		AllowReasons:   append([]string(nil), n.Verify.AllowReasons...),
		RequireConditions: func() []VerifyConditionRequirement {
			if len(n.Verify.RequireConditions) == 0 {
				return nil
			}
			return append([]VerifyConditionRequirement(nil), n.Verify.RequireConditions...)
		}(),
		Digest: "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}
}
