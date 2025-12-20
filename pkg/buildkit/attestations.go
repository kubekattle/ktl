package buildkit

import (
	"bytes"
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

type inTotoStatement struct {
	PredicateType string `json:"predicateType"`
	Subject       []struct {
		Digest map[string]string `json:"digest"`
	} `json:"subject"`
}

func subjectDigestMatches(statement inTotoStatement, subject v1.Hash) bool {
	for _, entry := range statement.Subject {
		for alg, hex := range entry.Digest {
			if strings.EqualFold(strings.TrimSpace(alg), subject.Algorithm) && strings.EqualFold(strings.TrimSpace(hex), subject.Hex) {
				return true
			}
		}
	}
	return false
}

func uniquePath(dir, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "attestation.json"
	}
	candidate := filepath.Join(dir, base)
	_, err := os.Stat(candidate)
	if err == nil {
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		for i := 2; i < 10_000; i++ {
			next := filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
			_, serr := os.Stat(next)
			if serr == nil {
				continue
			}
			if serr != nil && !os.IsNotExist(serr) {
				return "", serr
			}
			return next, nil
		}
		return "", fmt.Errorf("unable to allocate unique path for %s", candidate)
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return candidate, nil
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

	destDir = strings.TrimSpace(destDir)
	if destDir == "" {
		return nil, fmt.Errorf("destination directory is required")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create attestation dir: %w", err)
	}

	out := []AttestationFile{}
	for _, desc := range manifest.Manifests {
		subject := ""
		if desc.Annotations != nil && desc.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			subject = strings.TrimSpace(desc.Annotations["vnd.docker.reference.digest"])
		}

		img, err := lp.Image(desc.Digest)
		if err != nil {
			return nil, fmt.Errorf("load attestation manifest %s: %w", desc.Digest.String(), err)
		}
		raw, err := img.RawManifest()
		if err != nil {
			return nil, fmt.Errorf("read attestation manifest JSON %s: %w", desc.Digest.String(), err)
		}
		var parsed v1.Manifest
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("parse attestation manifest JSON %s: %w", desc.Digest.String(), err)
		}
		if subject == "" && parsed.Subject != nil {
			subject = strings.TrimSpace(parsed.Subject.Digest.String())
		}
		if subject == "" {
			continue
		}
		subjectHash, err := v1.NewHash(subject)
		if err != nil {
			return nil, fmt.Errorf("parse subject digest %s: %w", subject, err)
		}

		for _, layerDesc := range parsed.Layers {
			if strings.TrimSpace(string(layerDesc.MediaType)) != "application/vnd.in-toto+json" {
				continue
			}
			layerDigest := strings.TrimSpace(layerDesc.Digest.String())
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
			body, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr != nil {
				return nil, fmt.Errorf("read attestation blob %s: %w", layerDigest, readErr)
			}
			var statement inTotoStatement
			if err := json.Unmarshal(body, &statement); err != nil {
				return nil, fmt.Errorf("parse in-toto statement %s: %w", layerDigest, err)
			}
			if !subjectDigestMatches(statement, subjectHash) {
				return nil, fmt.Errorf("attestation %s subject digest does not match referenced subject %s", layerDigest, subject)
			}
			if predicateType == "" {
				predicateType = strings.TrimSpace(statement.PredicateType)
			}
			filename := fmt.Sprintf(
				"%s_%s.json",
				sanitizeFilenamePart(subjectHash.Hex),
				sanitizeFilenamePart(predicateType),
			)
			dst, err := uniquePath(destDir, filename)
			if err != nil {
				return nil, err
			}
			if err := copyToFile(dst, bytes.NewReader(body)); err != nil {
				return nil, fmt.Errorf("write attestation blob %s: %w", layerDigest, err)
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
