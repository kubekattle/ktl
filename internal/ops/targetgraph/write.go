package targetgraph

import (
	"os"

	"gopkg.in/yaml.v3"
)

func (g TargetGraph) MarshalYAML() ([]byte, error) {
	raw, err := yaml.Marshal(g)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}
	return raw, nil
}

func (g TargetGraph) WriteFile(path string) error {
	raw, err := g.MarshalYAML()
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
