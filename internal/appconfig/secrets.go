package appconfig

// SecretsConfig defines named secret providers for deploy-time resolution.
type SecretsConfig struct {
	DefaultProvider string                    `yaml:"defaultProvider,omitempty"`
	Providers       map[string]SecretProvider `yaml:"providers,omitempty"`
}

// SecretProvider defines a single secret provider.
type SecretProvider struct {
	Type                string `yaml:"type,omitempty"`
	Path                string `yaml:"path,omitempty"`
	Address             string `yaml:"address,omitempty"`
	Token               string `yaml:"token,omitempty"`
	Namespace           string `yaml:"namespace,omitempty"`
	Mount               string `yaml:"mount,omitempty"`
	KVVersion           int    `yaml:"kvVersion,omitempty"`
	Key                 string `yaml:"key,omitempty"`
	AuthMethod          string `yaml:"authMethod,omitempty"`
	AuthMount           string `yaml:"authMount,omitempty"`
	RoleID              string `yaml:"roleId,omitempty"`
	SecretID            string `yaml:"secretId,omitempty"`
	KubernetesRole      string `yaml:"kubernetesRole,omitempty"`
	KubernetesToken     string `yaml:"kubernetesToken,omitempty"`
	KubernetesTokenPath string `yaml:"kubernetesTokenPath,omitempty"`
	AWSRole             string `yaml:"awsRole,omitempty"`
	AWSRegion           string `yaml:"awsRegion,omitempty"`
	AWSHeaderValue      string `yaml:"awsHeaderValue,omitempty"`
}

func mergeSecrets(a, b SecretsConfig) SecretsConfig {
	out := a
	if b.DefaultProvider != "" {
		out.DefaultProvider = b.DefaultProvider
	}
	if len(b.Providers) > 0 {
		if out.Providers == nil {
			out.Providers = map[string]SecretProvider{}
		}
		for name, cfg := range b.Providers {
			out.Providers[name] = cfg
		}
	}
	return out
}
