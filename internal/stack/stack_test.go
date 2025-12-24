package stack

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCompile_MergesDefaultsAndProfiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: ktl.dev/v1
kind: Stack
name: demo
defaultProfile: dev
profiles:
  dev:
    defaults:
      values: [values-dev.yaml]
defaults:
  cluster: { name: c1 }
  namespace: ns1
  values: [values-common.yaml]
  set: { global.cluster: c1 }
`)
	writeFile(t, filepath.Join(root, "services", "stack.yaml"), `
defaults:
  tags: [svc]
`)
	writeFile(t, filepath.Join(root, "services", "redis", "release.yaml"), `
apiVersion: ktl.dev/v1
kind: Release
name: redis
chart: ./chart
values: [values-redis.yaml]
tags: [cache]
`)

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(p.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(p.Nodes))
	}
	n := p.Nodes[0]
	if n.ID != "c1/ns1/redis" {
		t.Fatalf("id=%q", n.ID)
	}
	wantValues := []string{
		filepath.Join(root, "values-common.yaml"),
		filepath.Join(root, "values-dev.yaml"),
		filepath.Join(root, "services", "redis", "values-redis.yaml"),
	}
	if got := join(n.Values); got != join(wantValues) {
		t.Fatalf("values=%v want=%v", n.Values, wantValues)
	}
	if len(n.Tags) != 2 || n.Tags[0] != "svc" || n.Tags[1] != "cache" {
		t.Fatalf("tags=%v", n.Tags)
	}
	if n.Set["global.cluster"] != "c1" {
		t.Fatalf("set=%v", n.Set)
	}
}

func TestSelect_ByTagAndIncludeDeps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stack.yaml"), `
apiVersion: ktl.dev/v1
kind: Stack
name: demo
defaults:
  cluster: { name: c1 }
  namespace: ns1
releases:
  - name: db
    chart: ./db
    tags: [core]
  - name: app
    chart: ./app
    tags: [app]
    needs: [db]
`)
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, err = Select(u, p, nil, Selector{Tags: []string{"app"}})
	if err == nil {
		t.Fatalf("expected missing deps error")
	}
	selected, err := Select(u, p, nil, Selector{Tags: []string{"app"}, IncludeDeps: true})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(selected.Nodes) != 2 {
		t.Fatalf("selected=%d", len(selected.Nodes))
	}
}

func join(vals []string) string {
	out := ""
	for _, v := range vals {
		out += v + "|"
	}
	return out
}
