package secretstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

type fileProvider struct {
	path string
	data map[string]interface{}
}

func newFileProvider(path string, baseDir string) (*fileProvider, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("file provider path is required")
	}
	if baseDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	path = filepath.Clean(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secrets file %q: %w", path, err)
	}
	data := make(map[string]interface{})
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse secrets file %q: %w", path, err)
	}
	return &fileProvider{path: path, data: data}, nil
}

func (p *fileProvider) Resolve(ctx context.Context, secretPath string) (string, error) {
	_ = ctx
	secretPath = strings.TrimSpace(secretPath)
	if secretPath == "" {
		return "", fmt.Errorf("secret path is required")
	}
	parts := strings.Split(strings.TrimPrefix(secretPath, "/"), "/")
	var current interface{} = p.data
	for _, part := range parts {
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			val, ok := typed[part]
			if !ok {
				return "", fmt.Errorf("secret path %q not found in %s", secretPath, p.path)
			}
			current = val
		case map[interface{}]interface{}:
			val, ok := typed[part]
			if !ok {
				return "", fmt.Errorf("secret path %q not found in %s", secretPath, p.path)
			}
			current = val
		default:
			return "", fmt.Errorf("secret path %q does not resolve to a value in %s", secretPath, p.path)
		}
	}
	if current == nil {
		return "", fmt.Errorf("secret path %q resolves to empty value in %s", secretPath, p.path)
	}
	switch typed := current.(type) {
	case string:
		return typed, nil
	case []byte:
		return string(typed), nil
	default:
		return "", fmt.Errorf("secret path %q resolved to non-string value in %s", secretPath, p.path)
	}
}

func (p *fileProvider) List(ctx context.Context, secretPath string) ([]string, error) {
	_ = ctx
	secretPath = strings.TrimSpace(secretPath)
	parts := []string{}
	if secretPath != "" {
		parts = strings.Split(strings.TrimPrefix(secretPath, "/"), "/")
	}
	var current interface{} = p.data
	for _, part := range parts {
		if part == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]interface{}:
			val, ok := typed[part]
			if !ok {
				return nil, fmt.Errorf("secret path %q not found in %s", secretPath, p.path)
			}
			current = val
		case map[interface{}]interface{}:
			val, ok := typed[part]
			if !ok {
				return nil, fmt.Errorf("secret path %q not found in %s", secretPath, p.path)
			}
			current = val
		default:
			return nil, fmt.Errorf("secret path %q does not resolve to a map in %s", secretPath, p.path)
		}
	}
	switch typed := current.(type) {
	case map[string]interface{}:
		return sortedKeys(typed), nil
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, v := range typed {
			key, ok := k.(string)
			if !ok {
				continue
			}
			out[key] = v
		}
		return sortedKeys(out), nil
	default:
		return nil, fmt.Errorf("secret path %q does not resolve to a map in %s", secretPath, p.path)
	}
}

func sortedKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
