package secrets

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScanOCIForSecrets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	layerBytes := gzipTar(map[string]string{
		"app/keys.pem": "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n",
	})
	layerHex := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.WriteFile(filepath.Join(blobs, layerHex), layerBytes, 0o644); err != nil {
		t.Fatalf("write layer: %v", err)
	}

	manHex := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	man := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"size":      2,
		},
		"layers": []any{
			map[string]any{
				"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
				"digest":    "sha256:" + layerHex,
				"size":      len(layerBytes),
			},
		},
	}
	manRaw, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(blobs, manHex), manRaw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	idx := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests": []any{
			map[string]any{
				"mediaType": "application/vnd.oci.image.manifest.v1+json",
				"digest":    "sha256:" + manHex,
				"size":      len(manRaw),
			},
		},
	}
	idxRaw, _ := json.Marshal(idx)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), idxRaw, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	findings, err := ScanOCIForSecrets(dir, 0)
	if err != nil {
		t.Fatalf("ScanOCIForSecrets: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected findings")
	}
}

func gzipTar(files map[string]string) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, body := range files {
		b := []byte(body)
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b))})
		_, _ = tw.Write(b)
	}
	_ = tw.Close()
	_ = gw.Close()
	return buf.Bytes()
}
