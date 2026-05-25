// File: internal/stack/types.go
// Brief: Stack and release configuration types.

package stack

import "time"

type APIVersionKind struct {
	APIVersion string `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`
	Kind       string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

type ClusterTarget struct {
	Name       string `yaml:"name,omitempty" json:"name,omitempty"`
	Kubeconfig string `yaml:"kubeconfig,omitempty" json:"kubeconfig,omitempty"`
	Context    string `yaml:"context,omitempty" json:"context,omitempty"`
}

type ApplyOptions struct {
	Atomic          *bool          `yaml:"atomic,omitempty" json:"atomic,omitempty"`
	Timeout         *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Wait            *bool          `yaml:"wait,omitempty" json:"wait,omitempty"`
	CreateNamespace *bool          `yaml:"createNamespace,omitempty" json:"createNamespace,omitempty"`
}

type DeleteOptions struct {
	Timeout *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

type VerifyOptions struct {
	// Enabled toggles post-apply verification for this release.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// FailOnWarnings fails the release when matching Warning events are observed.
	FailOnWarnings *bool `yaml:"failOnWarnings,omitempty" json:"failOnWarnings,omitempty"`
	// WarnOnly records verify findings but never fails the release.
	WarnOnly *bool `yaml:"warnOnly,omitempty" json:"warnOnly,omitempty"`
	// EventsWindow limits how far back to consider Warning events (prevents old noisy events
	// from failing new runs). Defaults to 15m when enabled.
	EventsWindow *time.Duration `yaml:"eventsWindow,omitempty" json:"eventsWindow,omitempty"`
	// Timeout bounds how long verify may run for this release. Defaults to 2m when enabled.
	Timeout *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// DenyReasons fails when a Warning event reason matches any entry (case-insensitive).
	// When empty, all Warning reasons are considered.
	DenyReasons []string `yaml:"denyReasons,omitempty" json:"denyReasons,omitempty"`
	// AllowReasons allows only these Warning event reasons when non-empty (case-insensitive).
	AllowReasons []string `yaml:"allowReasons,omitempty" json:"allowReasons,omitempty"`

	// RequireConditions enforces status.conditions on matching custom resources (CRs).
	RequireConditions []VerifyConditionRequirement `yaml:"requireConditions,omitempty" json:"requireConditions,omitempty"`
}

type VerifyConditionRequirement struct {
	Group         string `yaml:"group,omitempty" json:"group,omitempty"`   // e.g. example.com
	Kind          string `yaml:"kind,omitempty" json:"kind,omitempty"`     // e.g. Widget
	ConditionType string `yaml:"type,omitempty" json:"type,omitempty"`     // e.g. Ready
	RequireStatus string `yaml:"status,omitempty" json:"status,omitempty"` // True|False|Unknown
	AllowMissing  bool   `yaml:"allowMissing,omitempty" json:"allowMissing,omitempty"`
}

type ReleaseDefaults struct {
	Cluster    ClusterTarget     `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace  string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Values     []string          `yaml:"values,omitempty" json:"values,omitempty"`
	Set        map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Apply      ApplyOptions      `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete     DeleteOptions     `yaml:"delete,omitempty" json:"delete,omitempty"`
	Verify     VerifyOptions     `yaml:"verify,omitempty" json:"verify,omitempty"`
	Tags       []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Extra      map[string]any    `yaml:",inline" json:"-"`
	RawIgnored map[string]any    `yaml:"-" json:"-"`
}

type StackProfile struct {
	Defaults ReleaseDefaults  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Runner   RunnerConfig     `yaml:"runner,omitempty" json:"runner,omitempty"`
	CLI      StackCLIConfig   `yaml:"cli,omitempty" json:"cli,omitempty"`
	Hooks    StackHooksConfig `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

type StackFile struct {
	APIVersionKind `yaml:",inline" json:",inline"`

	Name           string                  `yaml:"name,omitempty" json:"name,omitempty"`
	DefaultProfile string                  `yaml:"defaultProfile,omitempty" json:"defaultProfile,omitempty"`
	Profiles       map[string]StackProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`

	Defaults ReleaseDefaults  `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Runner   RunnerConfig     `yaml:"runner,omitempty" json:"runner,omitempty"`
	CLI      StackCLIConfig   `yaml:"cli,omitempty" json:"cli,omitempty"`
	Hooks    StackHooksConfig `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Releases []ReleaseSpec    `yaml:"releases,omitempty" json:"releases,omitempty"`
	Nodes    []NodeSpec       `yaml:"nodes,omitempty" json:"nodes,omitempty"`
}

type StackHooksConfig struct {
	PreApply   []HookSpec `yaml:"preApply,omitempty" json:"preApply,omitempty"`
	PostApply  []HookSpec `yaml:"postApply,omitempty" json:"postApply,omitempty"`
	PreDelete  []HookSpec `yaml:"preDelete,omitempty" json:"preDelete,omitempty"`
	PostDelete []HookSpec `yaml:"postDelete,omitempty" json:"postDelete,omitempty"`
}

type HookSpec struct {
	Name    string         `yaml:"name,omitempty" json:"name,omitempty"`
	Type    string         `yaml:"type,omitempty" json:"type,omitempty"` // kubectl|script|http
	RunOnce bool           `yaml:"runOnce,omitempty" json:"runOnce,omitempty"`
	When    string         `yaml:"when,omitempty" json:"when,omitempty"` // success|failure|always
	Timeout *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry   *int           `yaml:"retry,omitempty" json:"retry,omitempty"` // max attempts, includes the initial attempt

	Kubeconfig string `yaml:"kubeconfig,omitempty" json:"kubeconfig,omitempty"`
	Context    string `yaml:"context,omitempty" json:"context,omitempty"`
	Namespace  string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	Kubectl *KubectlHookConfig `yaml:"kubectl,omitempty" json:"kubectl,omitempty"`
	Script  *ScriptHookConfig  `yaml:"script,omitempty" json:"script,omitempty"`
	HTTP    *HTTPHookConfig    `yaml:"http,omitempty" json:"http,omitempty"`
}

type KubectlHookConfig struct {
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`
}

type ScriptHookConfig struct {
	Command []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	WorkDir string            `yaml:"workDir,omitempty" json:"workDir,omitempty"`
}

type HTTPHookConfig struct {
	Method  string            `yaml:"method,omitempty" json:"method,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty" json:"body,omitempty"`
}

// StackCLIConfig controls default CLI behavior for `torque stack ...` subcommands.
// Flags and environment variables can override these settings.
type StackCLIConfig struct {
	// Selector sets default release selection constraints.
	Selector StackSelectorConfig `yaml:"selector,omitempty" json:"selector,omitempty"`

	// InferDeps controls whether selection includes inferred edges via manifest rendering.
	InferDeps       *bool `yaml:"inferDeps,omitempty" json:"inferDeps,omitempty"`
	InferConfigRefs *bool `yaml:"inferConfigRefs,omitempty" json:"inferConfigRefs,omitempty"`

	// Output sets default output format for commands that support it (e.g. plan/runs).
	Output string `yaml:"output,omitempty" json:"output,omitempty"`

	// Apply/Delete are CLI defaults specific to the run commands.
	Apply  StackApplyCLIConfig  `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete StackDeleteCLIConfig `yaml:"delete,omitempty" json:"delete,omitempty"`
	Resume StackResumeCLIConfig `yaml:"resume,omitempty" json:"resume,omitempty"`
}

type StackSelectorConfig struct {
	Clusters  []string `yaml:"clusters,omitempty" json:"clusters,omitempty"`
	Tags      []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	FromPaths []string `yaml:"fromPaths,omitempty" json:"fromPaths,omitempty"`
	Releases  []string `yaml:"releases,omitempty" json:"releases,omitempty"`
	GitRange  string   `yaml:"gitRange,omitempty" json:"gitRange,omitempty"`

	GitIncludeDeps       *bool `yaml:"gitIncludeDeps,omitempty" json:"gitIncludeDeps,omitempty"`
	GitIncludeDependents *bool `yaml:"gitIncludeDependents,omitempty" json:"gitIncludeDependents,omitempty"`

	IncludeDeps       *bool `yaml:"includeDeps,omitempty" json:"includeDeps,omitempty"`
	IncludeDependents *bool `yaml:"includeDependents,omitempty" json:"includeDependents,omitempty"`

	AllowMissingDeps *bool `yaml:"allowMissingDeps,omitempty" json:"allowMissingDeps,omitempty"`
}

type StackApplyCLIConfig struct {
	DryRun *bool `yaml:"dryRun,omitempty" json:"dryRun,omitempty"`
	Diff   *bool `yaml:"diff,omitempty" json:"diff,omitempty"`

	FailFast *bool              `yaml:"failFast,omitempty" json:"failFast,omitempty"`
	Retry    *int               `yaml:"retry,omitempty" json:"retry,omitempty"`
	Lock     StackLockCLIConfig `yaml:"lock,omitempty" json:"lock,omitempty"`
}

type StackDeleteCLIConfig struct {
	ConfirmThreshold *int `yaml:"confirmThreshold,omitempty" json:"confirmThreshold,omitempty"`

	FailFast *bool              `yaml:"failFast,omitempty" json:"failFast,omitempty"`
	Retry    *int               `yaml:"retry,omitempty" json:"retry,omitempty"`
	Lock     StackLockCLIConfig `yaml:"lock,omitempty" json:"lock,omitempty"`
}

type StackResumeCLIConfig struct {
	AllowDrift  *bool `yaml:"allowDrift,omitempty" json:"allowDrift,omitempty"`
	RerunFailed *bool `yaml:"rerunFailed,omitempty" json:"rerunFailed,omitempty"`
}

type StackLockCLIConfig struct {
	Enabled  *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Takeover *bool          `yaml:"takeover,omitempty" json:"takeover,omitempty"`
	TTL      *time.Duration `yaml:"ttl,omitempty" json:"ttl,omitempty"`
	Owner    *string        `yaml:"owner,omitempty" json:"owner,omitempty"`
}

type RunnerConfig struct {
	Concurrency            *int           `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	ProgressiveConcurrency *bool          `yaml:"progressiveConcurrency,omitempty" json:"progressiveConcurrency,omitempty"`
	KubeQPS                *float32       `yaml:"kubeQPS,omitempty" json:"kubeQPS,omitempty"`
	KubeBurst              *int           `yaml:"kubeBurst,omitempty" json:"kubeBurst,omitempty"`
	Limits                 RunnerLimits   `yaml:"limits,omitempty" json:"limits,omitempty"`
	Adaptive               RunnerAdaptive `yaml:"adaptive,omitempty" json:"adaptive,omitempty"`
	Extra                  map[string]any `yaml:",inline" json:"-"`
	RawIgnored             map[string]any `yaml:"-" json:"-"`
}

type RunnerLimits struct {
	MaxParallelPerNamespace *int           `yaml:"maxParallelPerNamespace,omitempty" json:"maxParallelPerNamespace,omitempty"`
	MaxParallelKind         map[string]int `yaml:"maxParallelKind,omitempty" json:"maxParallelKind,omitempty"`
	ParallelismGroupLimit   *int           `yaml:"parallelismGroupLimit,omitempty" json:"parallelismGroupLimit,omitempty"`
}

type RunnerAdaptive struct {
	Mode               string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	Min                *int     `yaml:"min,omitempty" json:"min,omitempty"`
	Window             *int     `yaml:"window,omitempty" json:"window,omitempty"`
	RampAfterSuccesses *int     `yaml:"rampAfterSuccesses,omitempty" json:"rampAfterSuccesses,omitempty"`
	RampMaxFailureRate *float64 `yaml:"rampMaxFailureRate,omitempty" json:"rampMaxFailureRate,omitempty"`
	CooldownSevere     *int     `yaml:"cooldownSevere,omitempty" json:"cooldownSevere,omitempty"`
}

type RunnerResolved struct {
	Concurrency            int                    `json:"concurrency"`
	ProgressiveConcurrency bool                   `json:"progressiveConcurrency"`
	KubeQPS                float32                `json:"kubeQPS,omitempty"`
	KubeBurst              int                    `json:"kubeBurst,omitempty"`
	Limits                 RunnerLimitsResolved   `json:"limits,omitempty"`
	Adaptive               RunnerAdaptiveResolved `json:"adaptive,omitempty"`
}

type RunnerLimitsResolved struct {
	MaxParallelPerNamespace int            `json:"maxParallelPerNamespace,omitempty"`
	MaxParallelKind         map[string]int `json:"maxParallelKind,omitempty"`
	ParallelismGroupLimit   int            `json:"parallelismGroupLimit,omitempty"`
}

type RunnerAdaptiveResolved struct {
	Mode               string  `json:"mode,omitempty"`
	Min                int     `json:"min,omitempty"`
	Window             int     `json:"window,omitempty"`
	RampAfterSuccesses int     `json:"rampAfterSuccesses,omitempty"`
	RampMaxFailureRate float64 `json:"rampMaxFailureRate,omitempty"`
	CooldownSevere     int     `json:"cooldownSevere,omitempty"`
}

type ReleaseFile struct {
	APIVersionKind `yaml:",inline" json:",inline"`

	NodeKind     string            `yaml:"nodeKind,omitempty" json:"nodeKind,omitempty"`
	Name         string            `yaml:"name,omitempty" json:"name,omitempty"`
	Chart        string            `yaml:"chart,omitempty" json:"chart,omitempty"`
	ChartVersion string            `yaml:"chartVersion,omitempty" json:"chartVersion,omitempty"`
	Wave         int               `yaml:"wave,omitempty" json:"wave,omitempty"`
	Critical     bool              `yaml:"critical,omitempty" json:"critical,omitempty"`
	Parallelism  string            `yaml:"parallelismGroup,omitempty" json:"parallelismGroup,omitempty"`
	Cluster      ClusterTarget     `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace    string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Values       []string          `yaml:"values,omitempty" json:"values,omitempty"`
	Set          map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Tags         []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Needs        []string          `yaml:"needs,omitempty" json:"needs,omitempty"`
	Apply        ApplyOptions      `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete       DeleteOptions     `yaml:"delete,omitempty" json:"delete,omitempty"`
	Hooks        StackHooksConfig  `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Action       ActionSpec        `yaml:"action,omitempty" json:"action,omitempty"`
	Database     DatabaseSpec      `yaml:"database,omitempty" json:"database,omitempty"`
	Kubernetes   KubernetesSpec    `yaml:"kubernetes,omitempty" json:"kubernetes,omitempty"`
}

type ReleaseSpec struct {
	Kind         string            `yaml:"kind,omitempty" json:"kind,omitempty"`
	Name         string            `yaml:"name,omitempty" json:"name,omitempty"`
	Chart        string            `yaml:"chart,omitempty" json:"chart,omitempty"`
	ChartVersion string            `yaml:"chartVersion,omitempty" json:"chartVersion,omitempty"`
	Wave         int               `yaml:"wave,omitempty" json:"wave,omitempty"`
	Critical     bool              `yaml:"critical,omitempty" json:"critical,omitempty"`
	Parallelism  string            `yaml:"parallelismGroup,omitempty" json:"parallelismGroup,omitempty"`
	Cluster      ClusterTarget     `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace    string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Values       []string          `yaml:"values,omitempty" json:"values,omitempty"`
	Set          map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Tags         []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Needs        []string          `yaml:"needs,omitempty" json:"needs,omitempty"`
	Apply        ApplyOptions      `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete       DeleteOptions     `yaml:"delete,omitempty" json:"delete,omitempty"`
	Verify       VerifyOptions     `yaml:"verify,omitempty" json:"verify,omitempty"`
	Hooks        StackHooksConfig  `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Action       ActionSpec        `yaml:"action,omitempty" json:"action,omitempty"`
	Database     DatabaseSpec      `yaml:"database,omitempty" json:"database,omitempty"`
	Host         HostCommandSpec   `yaml:"host,omitempty" json:"host,omitempty"`
	Kubernetes   KubernetesSpec    `yaml:"kubernetes,omitempty" json:"kubernetes,omitempty"`
	Input        map[string]any    `yaml:"input,omitempty" json:"input,omitempty"`
}

// NodeSpec is the generic stack node config shape. ReleaseSpec remains as the
// backward-compatible Helm-oriented alias accepted under `releases:`.
type NodeSpec = ReleaseSpec

type ResolvedRelease struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind,omitempty"`
	Name      string        `json:"name"`
	Dir       string        `json:"dir"`
	Cluster   ClusterTarget `json:"cluster"`
	Namespace string        `json:"namespace"`

	Chart        string            `json:"chart"`
	ChartVersion string            `json:"chartVersion,omitempty"`
	Wave         int               `json:"wave,omitempty"`
	Critical     bool              `json:"critical,omitempty"`
	Parallelism  string            `json:"parallelismGroup,omitempty"`
	Values       []string          `json:"values"`
	Set          map[string]string `json:"set"`

	Tags  []string `json:"tags"`
	Needs []string `json:"needs"`

	Apply  ApplyOptions  `json:"apply"`
	Delete DeleteOptions `json:"delete"`
	Verify VerifyOptions `json:"verify,omitempty"`

	Hooks      StackHooksConfig `json:"hooks,omitempty"`
	Action     ActionSpec       `json:"action,omitempty"`
	Database   DatabaseSpec     `json:"database,omitempty"`
	Host       HostCommandSpec  `json:"host,omitempty"`
	Kubernetes KubernetesSpec   `json:"kubernetes,omitempty"`

	SelectedBy []string `json:"selectedBy,omitempty"`

	InferredNeeds       []InferredNeed `json:"inferredNeeds,omitempty"`
	InferredRole        string         `json:"inferredRole,omitempty"`
	InferredPrimaryKind string         `json:"inferredPrimaryKind,omitempty"`

	EffectiveInputHash string          `json:"effectiveInputHash,omitempty"`
	EffectiveInput     *EffectiveInput `json:"effectiveInput,omitempty"`
	ExecutionGroup     int             `json:"executionGroup,omitempty"`
}

type InferredNeed struct {
	Name    string           `json:"name"`
	Reasons []InferredReason `json:"reasons,omitempty"`
}

type InferredReason struct {
	Type     string `json:"type"`
	Evidence string `json:"evidence,omitempty"`
}

type EffectiveInput struct {
	APIVersion string `json:"apiVersion"`

	StackGitCommit string `json:"stackGitCommit,omitempty"`
	StackGitDirty  bool   `json:"stackGitDirty,omitempty"`

	TorqueVersion   string `json:"torqueVersion,omitempty"`
	TorqueGitCommit string `json:"torqueGitCommit,omitempty"`

	NodeID string `json:"nodeId"`
	Kind   string `json:"kind,omitempty"`

	Chart EffectiveChartInput `json:"chart"`

	Values []FileDigest `json:"values,omitempty"`

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

type HostCommandSpec struct {
	Transport       string         `yaml:"transport,omitempty" json:"transport,omitempty"`
	TargetID        string         `yaml:"targetId,omitempty" json:"targetId,omitempty"`
	Target          string         `yaml:"target,omitempty" json:"target,omitempty"`
	TargetEnv       string         `yaml:"targetEnv,omitempty" json:"targetEnv,omitempty"`
	Command         string         `yaml:"command,omitempty" json:"command,omitempty"`
	DeleteCommand   string         `yaml:"deleteCommand,omitempty" json:"deleteCommand,omitempty"`
	Timeout         *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Path            string         `yaml:"path,omitempty" json:"path,omitempty"`
	SourcePath      string         `yaml:"sourcePath,omitempty" json:"sourcePath,omitempty"`
	Content         string         `yaml:"content,omitempty" json:"content,omitempty"`
	Template        string         `yaml:"template,omitempty" json:"template,omitempty"`
	TemplatePath    string         `yaml:"templatePath,omitempty" json:"templatePath,omitempty"`
	Data            map[string]any `yaml:"data,omitempty" json:"data,omitempty"`
	Mode            string         `yaml:"mode,omitempty" json:"mode,omitempty"`
	Owner           string         `yaml:"owner,omitempty" json:"owner,omitempty"`
	Group           string         `yaml:"group,omitempty" json:"group,omitempty"`
	Validate        string         `yaml:"validate,omitempty" json:"validate,omitempty"`
	Backup          bool           `yaml:"backup,omitempty" json:"backup,omitempty"`
	BackupPath      string         `yaml:"backupPath,omitempty" json:"backupPath,omitempty"`
	RemoveOnDelete  bool           `yaml:"removeOnDelete,omitempty" json:"removeOnDelete,omitempty"`
	RestoreOnDelete bool           `yaml:"restoreOnDelete,omitempty" json:"restoreOnDelete,omitempty"`
	PackageName     string         `yaml:"package,omitempty" json:"package,omitempty"`
	PackageManager  string         `yaml:"packageManager,omitempty" json:"packageManager,omitempty"`
	State           string         `yaml:"state,omitempty" json:"state,omitempty"`
	Version         string         `yaml:"version,omitempty" json:"version,omitempty"`
	UpdateCache     bool           `yaml:"updateCache,omitempty" json:"updateCache,omitempty"`
	Purge           bool           `yaml:"purge,omitempty" json:"purge,omitempty"`
	ServiceName     string         `yaml:"service,omitempty" json:"service,omitempty"`
	ServiceManager  string         `yaml:"serviceManager,omitempty" json:"serviceManager,omitempty"`
	Enabled         *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	StopOnDelete    bool           `yaml:"stopOnDelete,omitempty" json:"stopOnDelete,omitempty"`
	DisableOnDelete bool           `yaml:"disableOnDelete,omitempty" json:"disableOnDelete,omitempty"`
	UserName        string         `yaml:"user,omitempty" json:"user,omitempty"`
	GroupName       string         `yaml:"groupName,omitempty" json:"groupName,omitempty"`
	UserGroup       string         `yaml:"userGroup,omitempty" json:"userGroup,omitempty"`
	UID             *int           `yaml:"uid,omitempty" json:"uid,omitempty"`
	GID             *int           `yaml:"gid,omitempty" json:"gid,omitempty"`
	Home            string         `yaml:"home,omitempty" json:"home,omitempty"`
	Shell           string         `yaml:"shell,omitempty" json:"shell,omitempty"`
	Comment         string         `yaml:"comment,omitempty" json:"comment,omitempty"`
	Groups          []string       `yaml:"groups,omitempty" json:"groups,omitempty"`
	CreateHome      bool           `yaml:"createHome,omitempty" json:"createHome,omitempty"`
	RemoveHome      bool           `yaml:"removeHome,omitempty" json:"removeHome,omitempty"`
	System          bool           `yaml:"system,omitempty" json:"system,omitempty"`
	CronName        string         `yaml:"cronName,omitempty" json:"cronName,omitempty"`
	CronSchedule    string         `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	CronUser        string         `yaml:"cronUser,omitempty" json:"cronUser,omitempty"`
	CronCommand     string         `yaml:"cronCommand,omitempty" json:"cronCommand,omitempty"`
}

type KubernetesSpec struct {
	Provider     string                `yaml:"provider,omitempty" json:"provider,omitempty"`
	Certificates KubernetesCertSpec    `yaml:"certificates,omitempty" json:"certificates,omitempty"`
	Cluster      KubernetesClusterSpec `yaml:"cluster,omitempty" json:"cluster,omitempty"`
}

type KubernetesClusterSpec struct {
	Transport         string               `yaml:"transport,omitempty" json:"transport,omitempty"`
	Target            string               `yaml:"target,omitempty" json:"target,omitempty"`
	TargetEnv         string               `yaml:"targetEnv,omitempty" json:"targetEnv,omitempty"`
	Timeout           *time.Duration       `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Kubeconfig        string               `yaml:"kubeconfig,omitempty" json:"kubeconfig,omitempty"`
	KubeconfigEnv     string               `yaml:"kubeconfigEnv,omitempty" json:"kubeconfigEnv,omitempty"`
	Context           string               `yaml:"context,omitempty" json:"context,omitempty"`
	MinReadyNodes     int                  `yaml:"minReadyNodes,omitempty" json:"minReadyNodes,omitempty"`
	Namespaces        []string             `yaml:"namespaces,omitempty" json:"namespaces,omitempty"`
	StableIterations  int                  `yaml:"stableIterations,omitempty" json:"stableIterations,omitempty"`
	StableInterval    *time.Duration       `yaml:"stableInterval,omitempty" json:"stableInterval,omitempty"`
	ConfigCommand     string               `yaml:"configCommand,omitempty" json:"configCommand,omitempty"`
	APICommand        string               `yaml:"apiCommand,omitempty" json:"apiCommand,omitempty"`
	NodesCommand      string               `yaml:"nodesCommand,omitempty" json:"nodesCommand,omitempty"`
	NamespacesCommand string               `yaml:"namespacesCommand,omitempty" json:"namespacesCommand,omitempty"`
	PodsCommand       string               `yaml:"podsCommand,omitempty" json:"podsCommand,omitempty"`
	AppProbes         []KubernetesAppProbe `yaml:"appProbes,omitempty" json:"appProbes,omitempty"`
}

type KubernetesAppProbe struct {
	ID      string `yaml:"id,omitempty" json:"id,omitempty"`
	Command string `yaml:"command,omitempty" json:"command,omitempty"`
	Expect  string `yaml:"expect,omitempty" json:"expect,omitempty"`
}

type KubernetesCertSpec struct {
	Targets            []KubernetesCertTarget        `yaml:"targets,omitempty" json:"targets,omitempty"`
	TargetsFrom        KubernetesCertTargetsFromSpec `yaml:"targetsFrom,omitempty" json:"targetsFrom,omitempty"`
	RenewBefore        *time.Duration                `yaml:"renewBefore,omitempty" json:"renewBefore,omitempty"`
	Force              bool                          `yaml:"force,omitempty" json:"force,omitempty"`
	ForceOnceID        string                        `yaml:"forceOnceId,omitempty" json:"forceOnceId,omitempty"`
	StatePath          string                        `yaml:"statePath,omitempty" json:"statePath,omitempty"`
	Services           []string                      `yaml:"services,omitempty" json:"services,omitempty"`
	Order              string                        `yaml:"order,omitempty" json:"order,omitempty"`
	BatchSize          int                           `yaml:"batchSize,omitempty" json:"batchSize,omitempty"`
	HealthCheckCommand string                        `yaml:"healthCheckCommand,omitempty" json:"healthCheckCommand,omitempty"`
	VerifyCommand      string                        `yaml:"verifyCommand,omitempty" json:"verifyCommand,omitempty"`
	Policy             KubernetesLifecyclePolicySpec `yaml:"policy,omitempty" json:"policy,omitempty"`
}

type KubernetesLifecyclePolicySpec struct {
	MaxUnavailable           int                                   `yaml:"maxUnavailable,omitempty" json:"maxUnavailable,omitempty"`
	RequireFreshInspect      bool                                  `yaml:"requireFreshInspect,omitempty" json:"requireFreshInspect,omitempty"`
	MaxInspectAge            *time.Duration                        `yaml:"maxInspectAge,omitempty" json:"maxInspectAge,omitempty"`
	RequireHealthyInspect    bool                                  `yaml:"requireHealthyInspect,omitempty" json:"requireHealthyInspect,omitempty"`
	RequireSupportedProvider bool                                  `yaml:"requireSupportedProvider,omitempty" json:"requireSupportedProvider,omitempty"`
	MaintenanceWindow        KubernetesMaintenanceWindowSpec       `yaml:"maintenanceWindow,omitempty" json:"maintenanceWindow,omitempty"`
	AppProbes                []KubernetesAppProbe                  `yaml:"appProbes,omitempty" json:"appProbes,omitempty"`
	Override                 KubernetesLifecyclePolicyOverrideSpec `yaml:"override,omitempty" json:"override,omitempty"`
}

type KubernetesMaintenanceWindowSpec struct {
	Start    string   `yaml:"start,omitempty" json:"start,omitempty"`
	End      string   `yaml:"end,omitempty" json:"end,omitempty"`
	TimeZone string   `yaml:"timeZone,omitempty" json:"timeZone,omitempty"`
	Days     []string `yaml:"days,omitempty" json:"days,omitempty"`
}

type KubernetesLifecyclePolicyOverrideSpec struct {
	Reason    string                                     `yaml:"reason,omitempty" json:"reason,omitempty"`
	Ticket    string                                     `yaml:"ticket,omitempty" json:"ticket,omitempty"`
	ChangeID  string                                     `yaml:"changeId,omitempty" json:"changeId,omitempty"`
	Approver  string                                     `yaml:"approver,omitempty" json:"approver,omitempty"`
	ExpiresAt string                                     `yaml:"expiresAt,omitempty" json:"expiresAt,omitempty"`
	Scope     KubernetesLifecyclePolicyOverrideScopeSpec `yaml:"scope,omitempty" json:"scope,omitempty"`
}

type KubernetesLifecyclePolicyOverrideScopeSpec struct {
	NodeID          string   `yaml:"nodeId,omitempty" json:"nodeId,omitempty"`
	RunID           string   `yaml:"runId,omitempty" json:"runId,omitempty"`
	IntentDigest    string   `yaml:"intentDigest,omitempty" json:"intentDigest,omitempty"`
	TargetIDs       []string `yaml:"targetIds,omitempty" json:"targetIds,omitempty"`
	TargetSetDigest string   `yaml:"targetSetDigest,omitempty" json:"targetSetDigest,omitempty"`
}

type KubernetesCertTargetsFromSpec struct {
	SourceNode          string         `yaml:"sourceNode,omitempty" json:"sourceNode,omitempty"`
	SourceNodeID        string         `yaml:"sourceNodeId,omitempty" json:"sourceNodeId,omitempty"`
	Artifact            string         `yaml:"artifact,omitempty" json:"artifact,omitempty"`
	Roles               []string       `yaml:"roles,omitempty" json:"roles,omitempty"`
	IncludeNotReady     bool           `yaml:"includeNotReady,omitempty" json:"includeNotReady,omitempty"`
	AddressType         string         `yaml:"addressType,omitempty" json:"addressType,omitempty"`
	Transport           string         `yaml:"transport,omitempty" json:"transport,omitempty"`
	Target              string         `yaml:"target,omitempty" json:"target,omitempty"`
	TargetEnv           string         `yaml:"targetEnv,omitempty" json:"targetEnv,omitempty"`
	TargetTemplate      string         `yaml:"targetTemplate,omitempty" json:"targetTemplate,omitempty"`
	Timeout             *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Provider            string         `yaml:"provider,omitempty" json:"provider,omitempty"`
	Service             string         `yaml:"service,omitempty" json:"service,omitempty"`
	NodeAddressTemplate string         `yaml:"nodeAddressTemplate,omitempty" json:"nodeAddressTemplate,omitempty"`
	NodeIdentityFile    string         `yaml:"nodeIdentityFile,omitempty" json:"nodeIdentityFile,omitempty"`
	NodeSSHOptions      string         `yaml:"nodeSshOptions,omitempty" json:"nodeSshOptions,omitempty"`
	InspectCommand      string         `yaml:"inspectCommand,omitempty" json:"inspectCommand,omitempty"`
	RenewCommand        string         `yaml:"renewCommand,omitempty" json:"renewCommand,omitempty"`
	RestartCommand      string         `yaml:"restartCommand,omitempty" json:"restartCommand,omitempty"`
}

type KubernetesCertTarget struct {
	ID               string         `yaml:"id,omitempty" json:"id,omitempty"`
	Role             string         `yaml:"role,omitempty" json:"role,omitempty"`
	Provider         string         `yaml:"provider,omitempty" json:"provider,omitempty"`
	Transport        string         `yaml:"transport,omitempty" json:"transport,omitempty"`
	Target           string         `yaml:"target,omitempty" json:"target,omitempty"`
	TargetEnv        string         `yaml:"targetEnv,omitempty" json:"targetEnv,omitempty"`
	Timeout          *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Service          string         `yaml:"service,omitempty" json:"service,omitempty"`
	NodeAddress      string         `yaml:"nodeAddress,omitempty" json:"nodeAddress,omitempty"`
	NodeIdentityFile string         `yaml:"nodeIdentityFile,omitempty" json:"nodeIdentityFile,omitempty"`
	NodeSSHOptions   string         `yaml:"nodeSshOptions,omitempty" json:"nodeSshOptions,omitempty"`
	InspectCommand   string         `yaml:"inspectCommand,omitempty" json:"inspectCommand,omitempty"`
	RenewCommand     string         `yaml:"renewCommand,omitempty" json:"renewCommand,omitempty"`
	RestartCommand   string         `yaml:"restartCommand,omitempty" json:"restartCommand,omitempty"`
}

type ActionSpec struct {
	Idempotent bool              `yaml:"idempotent,omitempty" json:"idempotent,omitempty"`
	Apply      *ScriptHookConfig `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete     *ScriptHookConfig `yaml:"delete,omitempty" json:"delete,omitempty"`
	Plugin     *ActionPluginSpec `yaml:"plugin,omitempty" json:"plugin,omitempty"`
}

type ActionPluginSpec struct {
	Command []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	WorkDir string            `yaml:"workDir,omitempty" json:"workDir,omitempty"`
	Timeout *time.Duration    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Phases  []string          `yaml:"phases,omitempty" json:"phases,omitempty"`
	Config  map[string]any    `yaml:"config,omitempty" json:"config,omitempty"`
}

type BackfillSpec struct {
	CheckpointTable string         `yaml:"checkpointTable,omitempty" json:"checkpointTable,omitempty"`
	CheckpointKey   string         `yaml:"checkpointKey,omitempty" json:"checkpointKey,omitempty"`
	StartSQL        string         `yaml:"startSQL,omitempty" json:"startSQL,omitempty"`
	EndSQL          string         `yaml:"endSQL,omitempty" json:"endSQL,omitempty"`
	BatchSQL        string         `yaml:"batchSQL,omitempty" json:"batchSQL,omitempty"`
	BatchSize       int64          `yaml:"batchSize,omitempty" json:"batchSize,omitempty"`
	BatchSleep      *time.Duration `yaml:"batchSleep,omitempty" json:"batchSleep,omitempty"`
	MaxBatches      int            `yaml:"maxBatches,omitempty" json:"maxBatches,omitempty"`
}

type DatabaseSpec struct {
	Driver              string         `yaml:"driver,omitempty" json:"driver,omitempty"`
	DSN                 string         `yaml:"dsn,omitempty" json:"-"`
	DSNEnv              string         `yaml:"dsnEnv,omitempty" json:"dsnEnv,omitempty"`
	MetadataTable       string         `yaml:"metadataTable,omitempty" json:"metadataTable,omitempty"`
	RestorePointSQL     string         `yaml:"restorePointSQL,omitempty" json:"restorePointSQL,omitempty"`
	ExpandSQL           string         `yaml:"expandSQL,omitempty" json:"expandSQL,omitempty"`
	ContractSQL         string         `yaml:"contractSQL,omitempty" json:"contractSQL,omitempty"`
	PrepareSQL          string         `yaml:"prepareSQL,omitempty" json:"prepareSQL,omitempty"`
	ArmSQL              string         `yaml:"armSQL,omitempty" json:"armSQL,omitempty"`
	CommitSQL           string         `yaml:"commitSQL,omitempty" json:"commitSQL,omitempty"`
	VerifySQL           string         `yaml:"verifySQL,omitempty" json:"verifySQL,omitempty"`
	FinalizeSQL         string         `yaml:"finalizeSQL,omitempty" json:"finalizeSQL,omitempty"`
	VerifyExpectMessage string         `yaml:"verifyExpectMessage,omitempty" json:"verifyExpectMessage,omitempty"`
	StabilizationWindow *time.Duration `yaml:"stabilizationWindow,omitempty" json:"stabilizationWindow,omitempty"`
	RequireFencing      *bool          `yaml:"requireFencing,omitempty" json:"requireFencing,omitempty"`
	AmbiguityPolicy     string         `yaml:"ambiguityPolicy,omitempty" json:"ambiguityPolicy,omitempty"`
	Backfill            BackfillSpec   `yaml:"backfill,omitempty" json:"backfill,omitempty"`
}

type EffectiveChartInput struct {
	Ref             string `json:"ref"`
	Version         string `json:"version,omitempty"`
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
	Digest          string `json:"digest,omitempty"`
}

type EffectiveApplyInput struct {
	Atomic          bool   `json:"atomic"`
	Wait            bool   `json:"wait"`
	CreateNamespace bool   `json:"createNamespace"`
	Timeout         string `json:"timeout"`
	Digest          string `json:"digest,omitempty"`
}

type EffectiveDeleteInput struct {
	Timeout string `json:"timeout"`
	Digest  string `json:"digest,omitempty"`
}

type EffectiveVerifyInput struct {
	Enabled           bool                         `json:"enabled"`
	FailOnWarnings    bool                         `json:"failOnWarnings"`
	WarnOnly          bool                         `json:"warnOnly"`
	EventsWindow      string                       `json:"eventsWindow"`
	Timeout           string                       `json:"timeout"`
	DenyReasons       []string                     `json:"denyReasons,omitempty"`
	AllowReasons      []string                     `json:"allowReasons,omitempty"`
	RequireConditions []VerifyConditionRequirement `json:"requireConditions,omitempty"`
	Digest            string                       `json:"digest,omitempty"`
}

type FileDigest struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}
