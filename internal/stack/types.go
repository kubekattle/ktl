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
	// EventsWindow limits how far back to consider Warning events (prevents old noisy events
	// from failing new runs). Defaults to 15m when enabled.
	EventsWindow *time.Duration `yaml:"eventsWindow,omitempty" json:"eventsWindow,omitempty"`
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

// StackCLIConfig controls default CLI behavior for `ktl stack ...` subcommands.
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
}

type ReleaseSpec struct {
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
}

type ResolvedRelease struct {
	ID        string        `json:"id"`
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

	Hooks StackHooksConfig `json:"hooks,omitempty"`

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

	KtlVersion   string `json:"ktlVersion,omitempty"`
	KtlGitCommit string `json:"ktlGitCommit,omitempty"`

	NodeID string `json:"nodeId"`

	Chart EffectiveChartInput `json:"chart"`

	Values []FileDigest `json:"values,omitempty"`

	SetDigest     string `json:"setDigest,omitempty"`
	ClusterDigest string `json:"clusterDigest,omitempty"`

	Apply  EffectiveApplyInput  `json:"apply"`
	Delete EffectiveDeleteInput `json:"delete"`
	Verify EffectiveVerifyInput `json:"verify"`
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
	Enabled        bool   `json:"enabled"`
	FailOnWarnings bool   `json:"failOnWarnings"`
	EventsWindow   string `json:"eventsWindow"`
	Digest         string `json:"digest,omitempty"`
}

type FileDigest struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}
