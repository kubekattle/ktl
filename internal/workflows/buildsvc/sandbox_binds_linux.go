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

var etcFileBinds = []string{
	"/etc/passwd",
	"/etc/group",
	"/etc/nsswitch.conf",
	"/etc/ld.so.cache",
	"/etc/ld.so.conf",
	"/etc/ld.so.conf.d",
	"/etc/docker",
}

func buildSandboxBinds(hostContextDir, guestContextDir, hostCacheDir, guestCacheDir, builderAddr string, bindHome bool, homeDir string, extra []string) []sandboxBind {
	homeBound := bindHome && homeDir != "" && pathExists(homeDir)
	binds := make([]sandboxBind, 0, 6+len(extra)+len(dockerSocketCandidates))
	if hostContextDir != "" && guestContextDir != "" {
		binds = append(binds, makeSandboxBind(hostContextDir, guestContextDir, false))
	}
	if hostCacheDir != "" && guestCacheDir != "" {
		binds = append(binds, makeSandboxBind(hostCacheDir, guestCacheDir, false))
	}
	if sock := builderSocketPath(builderAddr); sock != "" && fileExists(sock) {
		binds = append(binds, makeSandboxBind(sock, sock, true))
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
	for _, path := range etcFileBinds {
		if pathExists(path) {
			binds = append(binds, makeSandboxBind(path, path, true))
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
		if fileExists(host) {
			// Some nsjail/kernel combinations reject MS_REMOUNT for file bind mounts.
			// Fall back to a regular bind mount for compatibility.
			flag = "--bindmount"
		}
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
