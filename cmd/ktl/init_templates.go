package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "embed"
	"gopkg.in/yaml.v3"
)

var (
	//go:embed templates/init/platform.yaml
	initTemplatePlatform string
	//go:embed templates/init/secure.yaml
	initTemplateSecure string
)

var embeddedInitTemplates = map[string]string{
	"platform": initTemplatePlatform,
	"secure":   initTemplateSecure,
}

func loadInitTemplate(ref string, repoRoot string) (map[string]any, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, "", nil
	}
	if looksLikeURL(ref) {
		raw, err := fetchTemplate(ref)
		if err != nil {
			return nil, "", err
		}
		mapped, err := parseTemplate(raw, ref)
		return mapped, ref, err
	}
	if path, ok := resolveTemplatePath(ref, repoRoot); ok {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read template %s: %w", path, err)
		}
		mapped, err := parseTemplate(raw, path)
		return mapped, path, err
	}
	if content, ok := embeddedInitTemplates[strings.ToLower(ref)]; ok {
		mapped, err := parseTemplate([]byte(content), "embedded:"+ref)
		return mapped, "embedded:" + ref, err
	}
	return nil, "", fmt.Errorf("unknown template %q (available: %s)", ref, strings.Join(listEmbeddedTemplates(), ", "))
}

func listEmbeddedTemplates() []string {
	names := make([]string, 0, len(embeddedInitTemplates))
	for name := range embeddedInitTemplates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolveTemplatePath(ref string, repoRoot string) (string, bool) {
	if ref == "" {
		return "", false
	}
	if fileExists(ref) {
		if abs, err := filepath.Abs(ref); err == nil {
			return abs, true
		}
		return ref, true
	}
	if repoRoot != "" {
		path := filepath.Join(repoRoot, ref)
		if fileExists(path) {
			return path, true
		}
	}
	return "", false
}

func parseTemplate(raw []byte, source string) (map[string]any, error) {
	var mapped map[string]any
	if err := yaml.Unmarshal(raw, &mapped); err != nil {
		return nil, fmt.Errorf("parse template %s: %w", source, err)
	}
	if mapped == nil {
		return nil, fmt.Errorf("template %s must be a YAML object", source)
	}
	return mapped, nil
}

func looksLikeURL(ref string) bool {
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

func fetchTemplate(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("template fetch failed: %s", resp.Status)
	}
	const maxSize = 1 << 20 // 1MB
	limited := io.LimitReader(resp.Body, maxSize)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("template fetch returned empty body")
	}
	return raw, nil
}
