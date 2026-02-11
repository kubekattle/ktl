package buildkit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTopOCILayers_EmptyDir(t *testing.T) {
	_, err := TopOCILayers("", 5)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestTopOCILayers_IgnoresMissingIndex(t *testing.T) {
	tmp := t.TempDir()
	_, err := TopOCILayers(tmp, 5)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestTopOCILayers_ParsesLayers(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "blobs", "sha256"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Minimal index -> manifest.
	indexDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	manifestDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	layerDigest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	indexJSON := `{"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + manifestDigest + `"}]}`
	if err := os.WriteFile(filepath.Join(tmp, "index.json"), []byte(indexJSON), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	_ = indexDigest // reserved: keep strings looking like real layout; index.json is root.
	manifestJSON := `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"` + layerDigest + `","size":12345}]}`
	if err := os.WriteFile(filepath.Join(tmp, "blobs", "sha256", manifestDigest[len("sha256:"):]), []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	layers, err := TopOCILayers(tmp, 10)
	if err != nil {
		t.Fatalf("TopOCILayers: %v", err)
	}
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer, got %d", len(layers))
	}
	if layers[0].Digest != layerDigest || layers[0].Size != 12345 {
		t.Fatalf("unexpected layer: %#v", layers[0])
	}
}
