package buildkit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
)

type AttestationFile struct {
	Path          string
	PredicateType string
	SubjectDigest string
	LayerDigest   string
}

var sanitizeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeFilenamePart(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	v = sanitizeFilenameRe.ReplaceAllString(v, "_")
	v = strings.Trim(v, "._-")
	if v == "" {
		return "unknown"
	}
	return v
}

func copyToFile(dst string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// WriteAttestationsFromOCI extracts in-toto attestation blobs from an OCI layout directory
// (including BuildKit-attached SBOM/provenance attestations) and writes them to destDir.
//
// The output is meant to be used for later publication (e.g. signing/transparency log upload)
// without requiring immediate registry referrer support.
func WriteAttestationsFromOCI(ociLayoutDir, destDir string) ([]AttestationFile, error) {
	lp, err := layout.FromPath(ociLayoutDir)
	if err != nil {
		return nil, fmt.Errorf("open OCI layout %s: %w", ociLayoutDir, err)
	}
	idx, err := lp.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("load OCI index: %w", err)
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read OCI index manifest: %w", err)
	}

	platformByDigest := map[string]string{}
	for _, desc := range manifest.Manifests {
		if desc.Platform == nil {
			continue
		}
		if desc.Platform.OS == "" || desc.Platform.Architecture == "" {
			continue
		}
		platformByDigest[desc.Digest.String()] = fmt.Sprintf("%s-%s", desc.Platform.OS, desc.Platform.Architecture)
	}

	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return nil, fmt.Errorf("destination directory is required")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create attestation dir: %w", err)
	}

	type manifestLayer struct {
		MediaType    string            `json:"mediaType"`
		Digest       string            `json:"digest"`
		Annotations  map[string]string `json:"annotations"`
		Platform     any               `json:"platform,omitempty"`
		ArtifactType string            `json:"artifactType,omitempty"`
	}
	type imageManifest struct {
		Layers []manifestLayer `json:"layers"`
	}

	out := []AttestationFile{}
	for _, desc := range manifest.Manifests {
		if desc.Annotations == nil {
			continue
		}
		if desc.Annotations["vnd.docker.reference.type"] != "attestation-manifest" {
			continue
		}
		subject := strings.TrimSpace(desc.Annotations["vnd.docker.reference.digest"])
		if subject == "" {
			continue
		}
		platform := platformByDigest[subject]
		if platform == "" {
			platform = "unknown"
		}

		img, err := lp.Image(desc.Digest)
		if err != nil {
			return nil, fmt.Errorf("load attestation manifest %s: %w", desc.Digest.String(), err)
		}
		raw, err := img.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("read attestation manifest JSON %s: %w", desc.Digest.String(), err)
		}
		var parsed imageManifest
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("parse attestation manifest JSON %s: %w", desc.Digest.String(), err)
		}

		for _, layerDesc := range parsed.Layers {
			if strings.TrimSpace(layerDesc.MediaType) != "application/vnd.in-toto+json" {
				continue
			}
			layerDigest := strings.TrimSpace(layerDesc.Digest)
			if layerDigest == "" {
				continue
			}
			predicateType := ""
			if layerDesc.Annotations != nil {
				predicateType = strings.TrimSpace(layerDesc.Annotations["in-toto.io/predicate-type"])
			}

			hash, err := v1.NewHash(layerDigest)
			if err != nil {
				return nil, fmt.Errorf("parse layer digest %s: %w", layerDigest, err)
			}
			rc, err := lp.Blob(hash)
			if err != nil {
				return nil, fmt.Errorf("open attestation blob %s: %w", layerDigest, err)
			}
			filename := fmt.Sprintf(
				"%s_%s_%s.json",
				sanitizeFilenamePart(platform),
				sanitizeFilenamePart(predicateType),
				sanitizeFilenamePart(hash.Hex),
			)
			dst := filepath.Join(destDir, filename)
			copyErr := copyToFile(dst, rc)
			_ = rc.Close()
			if copyErr != nil {
				return nil, fmt.Errorf("write attestation blob %s: %w", layerDigest, copyErr)
			}
			out = append(out, AttestationFile{
				Path:          dst,
				PredicateType: predicateType,
				SubjectDigest: subject,
				LayerDigest:   layerDigest,
			})
		}
	}
	return out, nil
}
