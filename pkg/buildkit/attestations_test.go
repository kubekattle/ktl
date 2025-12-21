package buildkit

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func writeBlob(t *testing.T, lp layout.Path, b []byte) v1.Hash {
	t.Helper()
	sum := sha256.Sum256(b)
	h := v1.Hash{Algorithm: "sha256", Hex: fmtHex(sum[:])}
	blobPath := filepath.Join(string(lp), "blobs", h.Algorithm, h.Hex)
	if err := os.MkdirAll(filepath.Dir(blobPath), 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}
	if err := os.WriteFile(blobPath, b, 0o644); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	return h
}

func fmtHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}

func appendSubjectImage(t *testing.T, lp layout.Path) v1.Hash {
	t.Helper()
	if err := lp.AppendImage(empty.Image, layout.WithPlatform(v1.Platform{OS: "linux", Architecture: "amd64"})); err != nil {
		t.Fatalf("append subject image: %v", err)
	}
	idx, err := lp.ImageIndex()
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	im, err := idx.IndexManifest()
	if err != nil {
		t.Fatalf("index manifest: %v", err)
	}
	for _, d := range im.Manifests {
		if d.Platform != nil && d.Platform.OS == "linux" && d.Platform.Architecture == "amd64" {
			return d.Digest
		}
	}
	t.Fatalf("subject descriptor not found in index")
	return v1.Hash{}
}

func appendAttestation(t *testing.T, lp layout.Path, subject v1.Hash, predicateType string, useLegacyAnnotations bool) {
	t.Helper()
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": predicateType,
		"subject": []any{
			map[string]any{
				"name": "_",
				"digest": map[string]string{
					subject.Algorithm: subject.Hex,
				},
			},
		},
		"predicate": map[string]any{"example": true},
	}
	stmtBytes, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal statement: %v", err)
	}
	stmtHash := writeBlob(t, lp, stmtBytes)

	cfgBytes := []byte(`{"architecture":"unknown","os":"unknown","config":{},"rootfs":{"type":"layers","diff_ids":[]}}`)
	cfgHash := writeBlob(t, lp, cfgBytes)

	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config: v1.Descriptor{
			MediaType: types.OCIConfigJSON,
			Digest:    cfgHash,
			Size:      int64(len(cfgBytes)),
		},
		Layers: []v1.Descriptor{{
			MediaType: types.MediaType("application/vnd.in-toto+json"),
			Digest:    stmtHash,
			Size:      int64(len(stmtBytes)),
			Annotations: map[string]string{
				"in-toto.io/predicate-type": predicateType,
			},
		}},
	}
	if !useLegacyAnnotations {
		manifest.Subject = &v1.Descriptor{
			MediaType: types.OCIManifestSchema1,
			Digest:    subject,
			Size:      0,
		}
	}

	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestHash := writeBlob(t, lp, rawManifest)

	desc := v1.Descriptor{
		MediaType: types.OCIManifestSchema1,
		Digest:    manifestHash,
		Size:      int64(len(rawManifest)),
		Platform:  &v1.Platform{OS: "unknown", Architecture: "unknown"},
	}
	if useLegacyAnnotations {
		desc.Annotations = map[string]string{
			"vnd.docker.reference.type":   "attestation-manifest",
			"vnd.docker.reference.digest": subject.String(),
		}
	}
	if err := lp.AppendDescriptor(desc); err != nil {
		t.Fatalf("append attestation descriptor: %v", err)
	}
}

func appendAttestationWithoutStatementSubject(t *testing.T, lp layout.Path, subject v1.Hash, predicateType string) {
	t.Helper()
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": predicateType,
		"subject":       []any{},
		"predicate":     map[string]any{"example": true},
	}
	stmtBytes, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal statement: %v", err)
	}
	stmtHash := writeBlob(t, lp, stmtBytes)

	cfgBytes := []byte(`{"architecture":"unknown","os":"unknown","config":{},"rootfs":{"type":"layers","diff_ids":[]}}`)
	cfgHash := writeBlob(t, lp, cfgBytes)

	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config: v1.Descriptor{
			MediaType: types.OCIConfigJSON,
			Digest:    cfgHash,
			Size:      int64(len(cfgBytes)),
		},
		Layers: []v1.Descriptor{{
			MediaType: types.MediaType("application/vnd.in-toto+json"),
			Digest:    stmtHash,
			Size:      int64(len(stmtBytes)),
			Annotations: map[string]string{
				"in-toto.io/predicate-type": predicateType,
			},
		}},
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestHash := writeBlob(t, lp, rawManifest)

	desc := v1.Descriptor{
		MediaType: types.OCIManifestSchema1,
		Digest:    manifestHash,
		Size:      int64(len(rawManifest)),
		Platform:  &v1.Platform{OS: "unknown", Architecture: "unknown"},
		Annotations: map[string]string{
			"vnd.docker.reference.type":   "attestation-manifest",
			"vnd.docker.reference.digest": subject.String(),
		},
	}
	if err := lp.AppendDescriptor(desc); err != nil {
		t.Fatalf("append attestation descriptor: %v", err)
	}
}

func TestWriteAttestationsFromOCI_LegacyAndOCISubject(t *testing.T) {
	tmp := t.TempDir()
	lp, err := layout.Write(tmp, empty.Index)
	if err != nil {
		t.Fatalf("layout write: %v", err)
	}
	subject := appendSubjectImage(t, lp)
	appendAttestation(t, lp, subject, "https://spdx.dev/Document", true)
	appendAttestation(t, lp, subject, "https://slsa.dev/provenance/v1", false)

	outDir := filepath.Join(tmp, "out")
	files, err := WriteAttestationsFromOCI(tmp, outDir)
	if err != nil {
		t.Fatalf("WriteAttestationsFromOCI: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 attestation files, got %d", len(files))
	}
	for _, f := range files {
		if f.SubjectDigest == "" || f.PredicateType == "" || f.Path == "" {
			t.Fatalf("unexpected attestation file: %#v", f)
		}
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatalf("expected file to exist: %s: %v", f.Path, err)
		}
	}
}

func TestWriteAttestationsFromOCI_AllowsEmptyStatementSubject(t *testing.T) {
	tmp := t.TempDir()
	lp, err := layout.Write(tmp, empty.Index)
	if err != nil {
		t.Fatalf("layout write: %v", err)
	}
	subject := appendSubjectImage(t, lp)
	appendAttestationWithoutStatementSubject(t, lp, subject, "https://spdx.dev/Document")

	outDir := filepath.Join(tmp, "out")
	files, err := WriteAttestationsFromOCI(tmp, outDir)
	if err != nil {
		t.Fatalf("WriteAttestationsFromOCI: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 attestation file, got %d", len(files))
	}
}

func TestWriteAttestationsFromOCI_RejectsMismatchedSubject(t *testing.T) {
	tmp := t.TempDir()
	lp, err := layout.Write(tmp, empty.Index)
	if err != nil {
		t.Fatalf("layout write: %v", err)
	}
	subject := appendSubjectImage(t, lp)

	other := v1.Hash{Algorithm: subject.Algorithm, Hex: strings.Repeat("0", len(subject.Hex))}
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v0.1",
		"predicateType": "https://spdx.dev/Document",
		"subject": []any{
			map[string]any{"name": "_", "digest": map[string]string{other.Algorithm: other.Hex}},
		},
		"predicate": map[string]any{"example": true},
	}
	stmtBytes, _ := json.Marshal(statement)
	stmtHash := writeBlob(t, lp, stmtBytes)
	cfgBytes := []byte(`{"architecture":"unknown","os":"unknown","config":{},"rootfs":{"type":"layers","diff_ids":[]}}`)
	cfgHash := writeBlob(t, lp, cfgBytes)
	manifest := v1.Manifest{
		SchemaVersion: 2,
		MediaType:     types.OCIManifestSchema1,
		Config:        v1.Descriptor{MediaType: types.OCIConfigJSON, Digest: cfgHash, Size: int64(len(cfgBytes))},
		Layers: []v1.Descriptor{{
			MediaType:    types.MediaType("application/vnd.in-toto+json"),
			Digest:       stmtHash,
			Size:         int64(len(stmtBytes)),
			Annotations:  map[string]string{"in-toto.io/predicate-type": "https://spdx.dev/Document"},
			ArtifactType: "",
		}},
	}
	rawManifest, _ := json.Marshal(manifest)
	manifestHash := writeBlob(t, lp, rawManifest)
	desc := v1.Descriptor{
		MediaType: types.OCIManifestSchema1,
		Digest:    manifestHash,
		Size:      int64(len(rawManifest)),
		Annotations: map[string]string{
			"vnd.docker.reference.type":   "attestation-manifest",
			"vnd.docker.reference.digest": subject.String(),
		},
		Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"},
	}
	if err := lp.AppendDescriptor(desc); err != nil {
		t.Fatalf("append descriptor: %v", err)
	}

	_, err = WriteAttestationsFromOCI(tmp, filepath.Join(tmp, "out"))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestWriteAttestationsFromOCI_WalksNestedIndexes(t *testing.T) {
	tmp := t.TempDir()
	lp, err := layout.Write(tmp, empty.Index)
	if err != nil {
		t.Fatalf("layout write: %v", err)
	}

	subject := appendSubjectImage(t, lp)
	appendAttestation(t, lp, subject, "https://spdx.dev/Document", true)

	if err := lp.AppendIndex(empty.Index); err != nil {
		t.Fatalf("append nested index: %v", err)
	}

	outDir := filepath.Join(tmp, "out")
	files, err := WriteAttestationsFromOCI(tmp, outDir)
	if err != nil {
		t.Fatalf("WriteAttestationsFromOCI: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 attestation file, got %d", len(files))
	}
}
