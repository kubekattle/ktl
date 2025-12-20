//go:build linux

// File: internal/workflows/buildsvc/sandbox_binds_linux.go
// Brief: Internal buildsvc package implementation for 'sandbox binds linux'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type sandboxBind struct {
	flag string
	spec string
}

var dockerSocketCandidates = []string{"/var/run/docker.sock", "/run/docker.sock"}
var systemDirBinds = []string{
	"/usr/bin",
	"/usr/lib",
	"/usr/libexec",
	"/usr/libexec/docker",
	"/usr/libexec/docker/cli-plugins",
	"/usr/lib/docker",
	"/lib",
	"/lib64",
}

func buildSandboxBinds(contextDir, cacheDir, builderAddr, exePath, homeDir string, extra []string) []sandboxBind {
	homeBound := homeDir != "" && pathExists(homeDir)
	binds := make([]sandboxBind, 0, 5+len(extra)+len(dockerSocketCandidates))
	if contextDir != "" {
		if !(homeBound && pathWithin(homeDir, contextDir)) {
			binds = append(binds, makeSandboxBind(contextDir, contextDir, false))
		}
	}
	cacheCovered := false
	if homeDir != "" {
		if rel, err := filepath.Rel(homeDir, cacheDir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			cacheCovered = true
		}
	}
	if cacheDir != "" && !cacheCovered {
		binds = append(binds, makeSandboxBind(cacheDir, cacheDir, false))
	}
	if sock := builderSocketPath(builderAddr); sock != "" && fileExists(sock) {
		binds = append(binds, makeSandboxBind(sock, sock, true))
	}
	if exePath != "" && fileExists(exePath) {
		binds = append(binds, makeSandboxBind(exePath, exePath, true))
	}
	for _, candidate := range dockerSocketCandidates {
		if fileExists(candidate) {
			binds = append(binds, makeSandboxBind(candidate, candidate, false))
		}
	}
	for _, dir := range systemDirBinds {
		if pathExists(dir) {
			binds = append(binds, makeSandboxBind(dir, dir, true))
		}
	}
	if homeBound {
		binds = append(binds, makeSandboxBind(homeDir, homeDir, false))
		dockerCfg := filepath.Join(homeDir, ".docker")
		if pathExists(dockerCfg) {
			binds = append(binds, makeSandboxBind(dockerCfg, dockerCfg, false))
		}
	}
	for _, spec := range extra {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		binds = append(binds, sandboxBind{flag: "--bindmount", spec: spec})
	}
	return dedupeBinds(binds)
}

func makeSandboxBind(host, guest string, readOnly bool) sandboxBind {
	flag := "--bindmount"
	if readOnly {
		flag = "--bindmount_ro"
	}
	return sandboxBind{flag: flag, spec: fmt.Sprintf("%s:%s", host, guest)}
}

func builderSocketPath(addr string) string {
	if strings.HasPrefix(addr, "unix://") {
		return strings.TrimPrefix(addr, "unix://")
	}
	if strings.HasPrefix(addr, "npipe") {
		return ""
	}
	return ""
}

func dedupeBinds(in []sandboxBind) []sandboxBind {
	seen := make(map[string]struct{})
	out := make([]sandboxBind, 0, len(in))
	for _, bind := range in {
		key := bind.flag + "|" + bind.spec
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, bind)
	}
	return out
}

func pathWithin(base, target string) bool {
	if base == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	prefix := ".." + string(filepath.Separator)
	return rel != ".." && !strings.HasPrefix(rel, prefix)
}

func sandboxDisabled() bool {
	return os.Getenv(sandboxDisableEnvKey) == "1" || os.Getenv(legacySandboxDisableEnv) == "1"
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
