package buildkit

import (
	"path/filepath"
	"testing"
)

func TestDefaultLocalTagUsesDirectoryName(t *testing.T) {
	contextDir := filepath.Join("..", "..", "testdata", "build", "dockerfile")
	tag := DefaultLocalTag(contextDir)
	if tag != "ktl.local/dockerfile:dev" {
		t.Fatalf("expected tag ktl.local/dockerfile:dev, got %s", tag)
	}
}

func TestNormalizePlatforms(t *testing.T) {
	platforms := NormalizePlatforms([]string{"linux/amd64", "linux/arm64", "linux/amd64"})
	if len(platforms) != 2 {
		t.Fatalf("expected 2 unique platforms, got %d", len(platforms))
	}
}
