package secretstore

import (
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// LoadConfig loads a secrets provider config from a file.
// The file can either include a top-level "secrets" key or be a raw secrets config.
func LoadConfig(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, fmt.Errorf("secret config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Config{}, nil
	}
	var rawMap map[string]interface{}
	if err := yaml.Unmarshal(raw, &rawMap); err != nil {
		return Config{}, fmt.Errorf("parse secrets config: %w", err)
	}
	if _, ok := rawMap["secrets"]; ok {
		var wrapper struct {
			Secrets Config `yaml:"secrets"`
		}
		if err := yaml.Unmarshal(raw, &wrapper); err != nil {
			return Config{}, fmt.Errorf("parse secrets config: %w", err)
		}
		return wrapper.Secrets, nil
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse secrets config: %w", err)
	}
	return cfg, nil
}
