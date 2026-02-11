package compose

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePinnedBaseImagesWithOptions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM --platform=linux/amd64 alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}
	if err := validatePinnedBaseImagesWithOptions(path, false); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
