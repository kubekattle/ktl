package registry

import (
	"path/filepath"
	"testing"
)

func TestRecordAndResolveLayoutFromTestdata(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	layoutPath := filepath.Join("..", "..", "testdata", "build", "dockerfile", "dist", "oci", "dockerfile")
	ref := "ktl.local/test:dev"
	if err := RecordLayout(ref, layoutPath); err != nil {
		t.Fatalf("RecordLayout error: %v", err)
	}
	rec, err := ResolveLayout(ref)
	if err != nil {
		t.Fatalf("ResolveLayout error: %v", err)
	}
	absLayout, _ := filepath.Abs(layoutPath)
	if rec.LayoutPath != absLayout {
		t.Fatalf("expected layout path %s, got %s", absLayout, rec.LayoutPath)
	}
}

func TestRecordBuildAndListRepository(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	layoutPath := filepath.Join("..", "..", "testdata", "build", "dockerfile", "dist", "oci", "dockerfile")
	tags := []string{"registry.example.com/app:dev", "registry.example.com/app:latest"}
	if err := RecordBuild(tags, layoutPath); err != nil {
		t.Fatalf("RecordBuild error: %v", err)
	}
	records, err := ListRepository("registry.example.com/app")
	if err != nil {
		t.Fatalf("ListRepository error: %v", err)
	}
	if len(records) != len(tags) {
		t.Fatalf("expected %d records, got %d", len(tags), len(records))
	}
}
