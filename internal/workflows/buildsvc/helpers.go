// File: internal/workflows/buildsvc/helpers.go
// Brief: Internal buildsvc package implementation for 'helpers'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/console"
	"github.com/example/ktl/internal/csvutil"
	"github.com/example/ktl/pkg/buildkit"
	"github.com/mattn/go-shellwords"
)

var composeDefaultFilenames = []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}

type buildMode string

const (
	modeAuto       buildMode = buildMode(ModeAuto)
	modeDockerfile buildMode = buildMode(ModeDockerfile)
	modeCompose    buildMode = buildMode(ModeCompose)
)

func parseKeyValueArgs(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}
	args := make(map[string]string, len(values))
	for _, raw := range values {
		key, val, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("invalid build argument %q (expected KEY=VALUE)", raw)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid build argument %q (empty key)", raw)
		}
		args[key] = val
	}
	return args, nil
}

// ParseKeyValueArgs exposes the argument parser for other command packages.
func ParseKeyValueArgs(values []string) (map[string]string, error) {
	return parseKeyValueArgs(values)
}

func parseCacheSpecs(values []string) ([]buildkit.CacheSpec, error) {
	specs := make([]buildkit.CacheSpec, 0, len(values))
	for _, raw := range values {
		attrs, err := parseKeyValueCSV(raw)
		if err != nil {
			return nil, err
		}
		typ := attrs["type"]
		if typ == "" {
			return nil, fmt.Errorf("cache spec %q missing type=<type>", raw)
		}
		delete(attrs, "type")
		specs = append(specs, buildkit.CacheSpec{Type: typ, Attrs: attrs})
	}
	return specs, nil
}

func parseKeyValueCSV(raw string) (map[string]string, error) {
	fields, err := csvutil.SplitFields(raw)
	if err != nil {
		return nil, err
	}
	attrs := make(map[string]string, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			attrs[strings.ToLower(field)] = ""
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		attrs[key] = strings.TrimSpace(value)
	}
	return attrs, nil
}

func expandPlatforms(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	out := make([]string, 0)
	for _, chunk := range values {
		for _, part := range strings.Split(chunk, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, ok := set[trimmed]; ok {
				continue
			}
			set[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out
}

// ExpandPlatforms exposes the platform normalization helper for other packages.
func ExpandPlatforms(values []string) []string {
	return expandPlatforms(values)
}

func selectBuildMode(contextAbs string, opts Options) (buildMode, []string, error) {
	mode, err := normalizeBuildMode(opts.BuildMode)
	if err != nil {
		return modeDockerfile, nil, err
	}

	if mode == modeDockerfile {
		return modeDockerfile, nil, nil
	}

	explicitFiles := len(opts.ComposeFiles) > 0
	if mode == modeCompose {
		files := opts.ComposeFiles
		if !explicitFiles {
			detected, err := findComposeFiles(contextAbs)
			if err != nil {
				return modeCompose, nil, err
			}
			if len(detected) == 0 {
				return modeCompose, nil, fmt.Errorf("compose mode selected but no compose files found in %s (looked for %s)", contextAbs, strings.Join(composeDefaultFilenames, ", "))
			}
			files = detected
		}
		absFiles, err := absolutePaths(files)
		if err != nil {
			return modeCompose, nil, err
		}
		return modeCompose, absFiles, nil
	}

	if explicitFiles {
		absFiles, err := absolutePaths(opts.ComposeFiles)
		if err != nil {
			return modeCompose, nil, err
		}
		return modeCompose, absFiles, nil
	}

	detected, err := findComposeFiles(contextAbs)
	if err != nil {
		return modeDockerfile, nil, err
	}
	if len(detected) == 0 {
		return modeDockerfile, nil, nil
	}

	dockerfilePath := opts.Dockerfile
	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(contextAbs, dockerfilePath)
	}
	if fileExists(dockerfilePath) {
		return modeDockerfile, nil, nil
	}

	return modeCompose, detected, nil
}

func absolutePaths(files []string) ([]string, error) {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, err
		}
		paths = append(paths, abs)
	}
	return paths, nil
}

func findComposeFiles(contextDir string) ([]string, error) {
	files := make([]string, 0, len(composeDefaultFilenames))
	for _, candidate := range composeDefaultFilenames {
		path := filepath.Join(contextDir, candidate)
		if fileExists(path) {
			files = append(files, path)
		}
	}
	return files, nil
}

func resolveConsoleFile(w io.Writer) console.File {
	if cf, ok := w.(console.File); ok {
		return cf
	}
	if f, ok := w.(*os.File); ok {
		return f
	}
	return os.Stderr
}

// ResolveConsoleFile exposes console detection for other workflows.
func ResolveConsoleFile(w io.Writer) console.File {
	return resolveConsoleFile(w)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func normalizeBuildMode(value string) (buildMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return modeAuto, nil
	case "docker", "dockerfile":
		return modeDockerfile, nil
	case "compose":
		return modeCompose, nil
	default:
		return modeDockerfile, fmt.Errorf("invalid build mode %q (expected auto, dockerfile, or compose)", value)
	}
}

func detectTTY(streams Streams) console.File {
	for _, c := range streams.terminalCandidates() {
		if cf, ok := c.(console.File); ok {
			return cf
		}
		if f, ok := c.(*os.File); ok {
			return f
		}
	}
	return nil
}

func parseInteractiveShell(raw string) ([]string, error) {
	args, err := shellwords.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse --interactive-shell: %w", err)
	}
	if len(args) == 0 {
		return nil, errors.New("--interactive-shell must contain at least one argument")
	}
	return args, nil
}
