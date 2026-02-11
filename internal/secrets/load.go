package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxSecretsConfigBytes = 2 << 20 // 2 MiB

func LoadConfig(ctx context.Context, ref string) (Config, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Config{}, nil
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return loadConfigFromURL(ctx, ref)
	}
	return loadConfigFromFile(ref)
}

func loadConfigFromFile(path string) (Config, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Config{}, errors.New("secrets config path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return parseConfigByExt(path, raw)
}

func loadConfigFromURL(ctx context.Context, url string) (Config, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Config{}, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Config{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Config{}, fmt.Errorf("fetch secrets config: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSecretsConfigBytes+1))
	if err != nil {
		return Config{}, err
	}
	if len(body) > maxSecretsConfigBytes {
		return Config{}, fmt.Errorf("secrets config too large (>%d bytes)", maxSecretsConfigBytes)
	}
	ext := filepath.Ext(strings.TrimSpace(url))
	if ext == "" {
		ext = ".yaml"
	}
	return parseConfigByExt(ext, body)
}

func parseConfigByExt(name string, raw []byte) (Config, error) {
	name = strings.TrimSpace(name)
	ext := strings.ToLower(filepath.Ext(name))
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return Config{}, nil
	}
	var cfg Config
	switch ext {
	case ".json":
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse secrets config json: %w", err)
		}
	default:
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse secrets config yaml: %w", err)
		}
	}
	return cfg, nil
}
