//go:build linux

package buildsvc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBuildSandboxBinds(t *testing.T) {
	dir := t.TempDir()
	builderSock := filepath.Join(dir, "buildkit.sock")
	if err := os.WriteFile(builderSock, []byte{}, 0o644); err != nil {
		t.Fatalf("write fake builder socket: %v", err)
	}

	binds := buildSandboxBinds("/workspace", "/cache", "unix://"+builderSock, builderSock, "", []string{"/tmp/data:/tmp/data"})
	if len(binds) < 4 {
		t.Fatalf("expected at least 4 binds, got %d", len(binds))
	}
	if binds[0].flag != "--bindmount" || binds[0].spec != "/workspace:/workspace" {
		t.Fatalf("unexpected context bind: %#v", binds[0])
	}
	if binds[1].flag != "--bindmount" || binds[1].spec != "/cache:/cache" {
		t.Fatalf("unexpected cache bind: %#v", binds[1])
	}
	if binds[2].flag != "--bindmount_ro" || binds[2].spec != builderSock+":"+builderSock {
		t.Fatalf("unexpected builder bind: %#v", binds[2])
	}
	if binds[len(binds)-1].spec != "/tmp/data:/tmp/data" {
		t.Fatalf("expected extra bind at end, got %#v", binds[len(binds)-1])
	}
}

func TestBuildSandboxBindsAddsDockerSocket(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socket, []byte{}, 0o644); err != nil {
		t.Fatalf("write fake socket: %v", err)
	}
	original := dockerSocketCandidates
	dockerSocketCandidates = []string{socket}
	t.Cleanup(func() { dockerSocketCandidates = original })

	binds := buildSandboxBinds("/workspace", "/cache", "unix:///run/buildkit.sock", "", "", nil)
	found := false
	for _, bind := range binds {
		if bind.spec == socket+":"+socket {
			found = true
			if bind.flag != "--bindmount" {
				t.Fatalf("docker socket should be rw bind, got %q", bind.flag)
			}
		}
	}
	if !found {
		t.Fatalf("expected docker socket %s to be bound: %#v", socket, binds)
	}
}

func TestDedupBinds(t *testing.T) {
	binds := dedupeBinds([]sandboxBind{
		{flag: "--bindmount", spec: "/a:/a"},
		{flag: "--bindmount", spec: "/a:/a"},
		{flag: "--bindmount_ro", spec: "/b:/b"},
	})
	if len(binds) != 2 {
		t.Fatalf("expected 2 binds, got %d", len(binds))
	}
}

func TestEnsureDefaultSandboxConfigLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	path, err := ensureDefaultSandboxConfig()
	if err != nil {
		t.Fatalf("ensureDefaultSandboxConfig: %v", err)
	}
	if path == "" {
		t.Fatalf("expected config path")
	}
}
