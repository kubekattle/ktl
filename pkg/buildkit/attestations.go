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

	"github.com/google/go-containerregistry/pkg/v1/types"
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

type ociIndex struct {
	Manifests []struct {
		MediaType    string            `json:"mediaType"`
		Digest       string            `json:"digest"`
		Annotations  map[string]string `json:"annotations"`
		ArtifactType string            `json:"artifactType"`
		Platform     any               `json:"platform"`
	} `json:"manifests"`
}

type ociManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Subject       *struct {
		Digest string `json:"digest"`
	} `json:"subject"`
	Layers []struct {
		MediaType    string            `json:"mediaType"`
		Digest       string            `json:"digest"`
		Annotations  map[string]string `json:"annotations"`
		ArtifactType string            `json:"artifactType"`
	} `json:"layers"`
}

type parsedDigest struct {
	Algorithm string
	Hex       string
}

func (d parsedDigest) String() string { return d.Algorithm + ":" + d.Hex }

func parseSHA256Digest(value string) (parsedDigest, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return parsedDigest{}, fmt.Errorf("invalid digest %q", value)
	}
	alg := strings.TrimSpace(parts[0])
	hex := strings.TrimSpace(parts[1])
	if alg != "sha256" {
		return parsedDigest{}, fmt.Errorf("unsupported digest algorithm %q", alg)
	}
	if len(hex) != 64 {
		return parsedDigest{}, fmt.Errorf("invalid sha256 hex length for %q", value)
	}
	for _, r := range hex {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return parsedDigest{}, fmt.Errorf("invalid sha256 hex for %q", value)
		}
	}
	return parsedDigest{Algorithm: "sha256", Hex: strings.ToLower(hex)}, nil
}

func blobPath(layoutDir string, digest parsedDigest) string {
	return filepath.Join(layoutDir, "blobs", digest.Algorithm, digest.Hex)
}

func readJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func subjectDigestMatches(statement inTotoStatement, subject parsedDigest) bool {
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
	ociLayoutDir = strings.TrimSpace(ociLayoutDir)
	if ociLayoutDir == "" {
		return nil, fmt.Errorf("open OCI layout: path is empty")
	}
	var root ociIndex
	if err := readJSON(filepath.Join(ociLayoutDir, "index.json"), &root); err != nil {
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

	seen := map[string]struct{}{}
	var walkIndex func(idx ociIndex) error
	var walkDescriptor func(mediaType, digest string, annotations map[string]string) error

	walkIndex = func(idx ociIndex) error {
		for _, m := range idx.Manifests {
			if err := walkDescriptor(m.MediaType, m.Digest, m.Annotations); err != nil {
				return err
			}
		}
		return nil
	}

	walkDescriptor = func(mediaType, digest string, annotations map[string]string) error {
		mt := strings.TrimSpace(mediaType)
		digest = strings.TrimSpace(digest)
		if digest == "" {
			return nil
		}
		key := mt + "|" + digest
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}

		// Index/manifest list.
		if mt == string(types.OCIImageIndex) || mt == string(types.DockerManifestList) {
			parsed, err := parseSHA256Digest(digest)
			if err != nil {
				return nil
			}
			path := blobPath(ociLayoutDir, parsed)
			var child ociIndex
			if err := readJSON(path, &child); err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				// Best-effort: if the blob isn't an index, ignore it.
				return nil
			}
			return walkIndex(child)
		}

		// Image/attestation manifest.
		parsed, err := parseSHA256Digest(digest)
		if err != nil {
			return nil
		}
		var man ociManifest
		if err := readJSON(blobPath(ociLayoutDir, parsed), &man); err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return nil
		}

		subject := ""
		if annotations != nil && annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			subject = strings.TrimSpace(annotations["vnd.docker.reference.digest"])
		}
		if subject == "" && man.Subject != nil {
			subject = strings.TrimSpace(man.Subject.Digest)
		}
		subjectParsed, err := parseSHA256Digest(subject)
		if err != nil {
			subjectParsed = parsedDigest{}
		}

		for _, layer := range man.Layers {
			if strings.TrimSpace(layer.MediaType) != "application/vnd.in-toto+json" {
				continue
			}
			layerDigest := strings.TrimSpace(layer.Digest)
			if layerDigest == "" || subjectParsed.Hex == "" {
				continue
			}
			predicateType := ""
			if layer.Annotations != nil {
				predicateType = strings.TrimSpace(layer.Annotations["in-toto.io/predicate-type"])
			}

			layerParsed, err := parseSHA256Digest(layerDigest)
			if err != nil {
				continue
			}
			body, readErr := os.ReadFile(blobPath(ociLayoutDir, layerParsed))
			if readErr != nil {
				if os.IsNotExist(readErr) {
					continue
				}
				return fmt.Errorf("read attestation blob %s: %w", layerDigest, readErr)
			}

			var statement inTotoStatement
			if err := json.Unmarshal(body, &statement); err != nil {
				continue
			}
			if len(statement.Subject) > 0 && !subjectDigestMatches(statement, subjectParsed) {
				return fmt.Errorf("attestation %s subject digest does not match referenced subject %s", layerDigest, subject)
			}
			if predicateType == "" {
				predicateType = strings.TrimSpace(statement.PredicateType)
			}
			filename := fmt.Sprintf(
				"%s_%s.json",
				sanitizeFilenamePart(subjectParsed.Hex),
				sanitizeFilenamePart(predicateType),
			)
			dst, err := uniquePath(destDir, filename)
			if err != nil {
				return err
			}
			if err := copyToFile(dst, bytes.NewReader(body)); err != nil {
				return fmt.Errorf("write attestation blob %s: %w", layerDigest, err)
			}
			out = append(out, AttestationFile{
				Path:          dst,
				PredicateType: predicateType,
				SubjectDigest: subject,
				LayerDigest:   layerDigest,
			})
		}

		return nil
	}

	if err := walkIndex(root); err != nil {
		return nil, err
	}

	// Fallback: if index.json doesn't reference any usable attestation manifests,
	// scan all blobs and try to treat them as indexes.
	if len(out) == 0 {
		dir := filepath.Join(ociLayoutDir, "blobs", "sha256")
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, ent := range entries {
				if ent.IsDir() {
					continue
				}
				hex := strings.TrimSpace(ent.Name())
				if hex == "" {
					continue
				}
				if err := walkDescriptor(string(types.OCIImageIndex), "sha256:"+hex, nil); err != nil {
					// Best-effort.
					continue
				}
			}
		}
	}

	return out, nil
}
