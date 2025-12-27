package stack

import (
	"os"
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

func TestMapChangedFiles_MapsToSharedValuesDir(t *testing.T) {
	root := t.TempDir()
	u := &Universe{
		RootDir: root,
		Stacks:  map[string]StackFile{root: {Name: "demo"}},
	}
	shared := filepath.Join(root, "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	values := filepath.Join(shared, "values.yaml")
	if err := os.WriteFile(values, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Nodes: []*ResolvedRelease{
			{
				ID:        "c/ns/a",
				Name:      "a",
				Dir:       filepath.Join(root, "apps", "a"),
				Cluster:   ClusterTarget{Name: "c"},
				Namespace: "ns",
				Values:    []string{values},
			},
		},
	}
	changed := []string{filepath.Join("shared", "values.yaml")}
	m := mapChangedFiles(u, p, changed)
	if got := join(m[changed[0]]); got != "c/ns/a|" {
		t.Fatalf("map %s -> %v", changed[0], m[changed[0]])
	}
}

func TestMapChangedFiles_MapsStackYAMLToSubtree(t *testing.T) {
	root := t.TempDir()
	u := &Universe{
		RootDir: root,
		Stacks: map[string]StackFile{
			root: {Name: "demo"},
		},
	}
	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Nodes: []*ResolvedRelease{
			{ID: "c/ns/a", Name: "a", Dir: filepath.Join(root, "services", "a"), Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
			{ID: "c/ns/b", Name: "b", Dir: filepath.Join(root, "services", "b"), Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
			{ID: "c/ns/c", Name: "c", Dir: filepath.Join(root, "other", "c"), Cluster: ClusterTarget{Name: "c"}, Namespace: "ns"},
		},
	}
	changed := []string{filepath.Join("services", "stack.yaml")}
	m := mapChangedFiles(u, p, changed)
	if got := join(m[changed[0]]); got != "c/ns/a|c/ns/b|" {
		t.Fatalf("map %s -> %v", changed[0], m[changed[0]])
	}
}

func TestMapChangedFiles_MapsToLocalChartPath(t *testing.T) {
	root := t.TempDir()
	u := &Universe{
		RootDir: root,
		Stacks:  map[string]StackFile{root: {Name: "demo"}},
	}
	charts := filepath.Join(root, "charts", "base")
	if err := os.MkdirAll(filepath.Join(charts, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(charts, "Chart.yaml"), []byte("name: base\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Plan{
		StackRoot: root,
		StackName: "demo",
		Nodes: []*ResolvedRelease{
			{
				ID:        "c/ns/a",
				Name:      "a",
				Dir:       filepath.Join(root, "apps", "a"),
				Cluster:   ClusterTarget{Name: "c"},
				Namespace: "ns",
				Chart:     charts,
			},
		},
	}
	changed := []string{filepath.Join("charts", "base", "Chart.yaml")}
	m := mapChangedFiles(u, p, changed)
	if got := join(m[changed[0]]); got != "c/ns/a|" {
		t.Fatalf("map %s -> %v", changed[0], m[changed[0]])
	}
}
