package stack

import (
	"path/filepath"
	"testing"
)

func TestMapChangedFiles_MapsToNearestReleaseDir(t *testing.T) {
	root := t.TempDir()
	u := &Universe{
		RootDir: root,
		Stacks:  map[string]StackFile{root: {Name: "demo"}},
	}
	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Profile:   "",
		Nodes: []*ResolvedRelease{
			{ID: "c/ns/a", Name: "a", Dir: filepath.Join(root, "services", "a"), Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
			{ID: "c/ns/b", Name: "b", Dir: filepath.Join(root, "services", "b"), Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
		},
	}
	changed := []string{
		filepath.Join("services", "a", "Chart.yaml"),
		filepath.Join("services", "a", "templates", "cm.yaml"),
		filepath.Join("services", "b", "values.yaml"),
		filepath.Join("README.md"),
	}
	m := mapChangedFiles(u, p, changed)
	if got := join(m[changed[0]]); got != "c/ns/a|" {
		t.Fatalf("map %s -> %v", changed[0], m[changed[0]])
	}
	if got := join(m[changed[1]]); got != "c/ns/a|" {
		t.Fatalf("map %s -> %v", changed[1], m[changed[1]])
	}
	if got := join(m[changed[2]]); got != "c/ns/b|" {
		t.Fatalf("map %s -> %v", changed[2], m[changed[2]])
	}
	if _, ok := m[changed[3]]; ok {
		t.Fatalf("unexpected mapping for %s: %v", changed[3], m[changed[3]])
	}
}
