package buildsvc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePinnedBaseImagesWithOptions(t *testing.T) {
	t.Parallel()

	t.Run("pinned_ok", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		if err := os.WriteFile(path, []byte("FROM alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"), 0o644); err != nil {
			t.Fatalf("write dockerfile: %v", err)
		}
		if err := validatePinnedBaseImagesWithOptions(path, false); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})

	t.Run("platform_prefix_unpinned_rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		if err := os.WriteFile(path, []byte("FROM --platform=$BUILDPLATFORM alpine:3.20\n"), 0o644); err != nil {
			t.Fatalf("write dockerfile: %v", err)
		}
		if err := validatePinnedBaseImagesWithOptions(path, false); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("scratch_ok", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		if err := os.WriteFile(path, []byte("FROM scratch\n"), 0o644); err != nil {
			t.Fatalf("write dockerfile: %v", err)
		}
		if err := validatePinnedBaseImagesWithOptions(path, false); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})

	t.Run("unpinned_allowed_with_override", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "Dockerfile")
		if err := os.WriteFile(path, []byte("FROM alpine:3.20\n"), 0o644); err != nil {
			t.Fatalf("write dockerfile: %v", err)
		}
		if err := validatePinnedBaseImagesWithOptions(path, true); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
}
