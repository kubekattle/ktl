package secretstore

// Config describes available secret providers.
type Config struct {
	DefaultProvider string                    `yaml:"defaultProvider,omitempty" json:"defaultProvider,omitempty"`
	Providers       map[string]ProviderConfig `yaml:"providers,omitempty" json:"providers,omitempty"`
}

// ProviderConfig captures provider-specific settings.
type ProviderConfig struct {
	Type                string `yaml:"type,omitempty" json:"type,omitempty"`
	Path                string `yaml:"path,omitempty" json:"path,omitempty"`
	Address             string `yaml:"address,omitempty" json:"address,omitempty"`
	Token               string `yaml:"token,omitempty" json:"token,omitempty"`
	Namespace           string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Mount               string `yaml:"mount,omitempty" json:"mount,omitempty"`
	KVVersion           int    `yaml:"kvVersion,omitempty" json:"kvVersion,omitempty"`
	Key                 string `yaml:"key,omitempty" json:"key,omitempty"`
	AuthMethod          string `yaml:"authMethod,omitempty" json:"authMethod,omitempty"`
	AuthMount           string `yaml:"authMount,omitempty" json:"authMount,omitempty"`
	RoleID              string `yaml:"roleId,omitempty" json:"roleId,omitempty"`
	SecretID            string `yaml:"secretId,omitempty" json:"secretId,omitempty"`
	KubernetesRole      string `yaml:"kubernetesRole,omitempty" json:"kubernetesRole,omitempty"`
	KubernetesToken     string `yaml:"kubernetesToken,omitempty" json:"kubernetesToken,omitempty"`
	KubernetesTokenPath string `yaml:"kubernetesTokenPath,omitempty" json:"kubernetesTokenPath,omitempty"`
	AWSRole             string `yaml:"awsRole,omitempty" json:"awsRole,omitempty"`
	AWSRegion           string `yaml:"awsRegion,omitempty" json:"awsRegion,omitempty"`
	AWSHeaderValue      string `yaml:"awsHeaderValue,omitempty" json:"awsHeaderValue,omitempty"`
}

// Empty reports whether the configuration declares any providers or defaults.
func (c Config) Empty() bool {
	return c.DefaultProvider == "" && len(c.Providers) == 0
}

// MergeConfig merges two configs, preferring non-empty values from b.
func MergeConfig(a, b Config) Config {
	out := a
	if b.DefaultProvider != "" {
		out.DefaultProvider = b.DefaultProvider
	}
	if len(b.Providers) > 0 {
		if out.Providers == nil {
			out.Providers = map[string]ProviderConfig{}
		}
		for name, cfg := range b.Providers {
			out.Providers[name] = cfg
		}
	}
	return out
}
