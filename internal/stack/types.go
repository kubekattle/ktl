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
	Atomic  *bool          `yaml:"atomic,omitempty" json:"atomic,omitempty"`
	Timeout *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Wait    *bool          `yaml:"wait,omitempty" json:"wait,omitempty"`
}

type DeleteOptions struct {
	Timeout *time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

type ReleaseDefaults struct {
	Cluster    ClusterTarget     `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace  string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Values     []string          `yaml:"values,omitempty" json:"values,omitempty"`
	Set        map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Apply      ApplyOptions      `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete     DeleteOptions     `yaml:"delete,omitempty" json:"delete,omitempty"`
	Tags       []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Extra      map[string]any    `yaml:",inline" json:"-"`
	RawIgnored map[string]any    `yaml:"-" json:"-"`
}

type StackProfile struct {
	Defaults ReleaseDefaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`
}

type StackFile struct {
	APIVersionKind `yaml:",inline" json:",inline"`

	Name           string                  `yaml:"name,omitempty" json:"name,omitempty"`
	DefaultProfile string                  `yaml:"defaultProfile,omitempty" json:"defaultProfile,omitempty"`
	Profiles       map[string]StackProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`

	Defaults ReleaseDefaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Releases []ReleaseSpec   `yaml:"releases,omitempty" json:"releases,omitempty"`
}

type ReleaseFile struct {
	APIVersionKind `yaml:",inline" json:",inline"`

	Name      string            `yaml:"name,omitempty" json:"name,omitempty"`
	Chart     string            `yaml:"chart,omitempty" json:"chart,omitempty"`
	Cluster   ClusterTarget     `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Values    []string          `yaml:"values,omitempty" json:"values,omitempty"`
	Set       map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Tags      []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Needs     []string          `yaml:"needs,omitempty" json:"needs,omitempty"`
	Apply     ApplyOptions      `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete    DeleteOptions     `yaml:"delete,omitempty" json:"delete,omitempty"`
}

type ReleaseSpec struct {
	Name      string            `yaml:"name,omitempty" json:"name,omitempty"`
	Chart     string            `yaml:"chart,omitempty" json:"chart,omitempty"`
	Cluster   ClusterTarget     `yaml:"cluster,omitempty" json:"cluster,omitempty"`
	Namespace string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Values    []string          `yaml:"values,omitempty" json:"values,omitempty"`
	Set       map[string]string `yaml:"set,omitempty" json:"set,omitempty"`
	Tags      []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Needs     []string          `yaml:"needs,omitempty" json:"needs,omitempty"`
	Apply     ApplyOptions      `yaml:"apply,omitempty" json:"apply,omitempty"`
	Delete    DeleteOptions     `yaml:"delete,omitempty" json:"delete,omitempty"`
}

type ResolvedRelease struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Dir       string        `json:"dir"`
	Cluster   ClusterTarget `json:"cluster"`
	Namespace string        `json:"namespace"`

	Chart  string            `json:"chart"`
	Values []string          `json:"values"`
	Set    map[string]string `json:"set"`

	Tags  []string `json:"tags"`
	Needs []string `json:"needs"`

	Apply  ApplyOptions  `json:"apply"`
	Delete DeleteOptions `json:"delete"`

	SelectedBy []string `json:"selectedBy,omitempty"`

	EffectiveInputHash string `json:"effectiveInputHash,omitempty"`
	ExecutionGroup     int    `json:"executionGroup,omitempty"`
}
