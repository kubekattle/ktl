package secrets

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/types"
)

type ociIndex struct {
	Manifests []struct {
		MediaType    string `json:"mediaType"`
		Digest       string `json:"digest"`
		ArtifactType string `json:"artifactType"`
	} `json:"manifests"`
}

type ociManifest struct {
	MediaType string `json:"mediaType"`
	Config    struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

func ScanOCIForSecrets(ociLayoutDir string, byteLimit int64) ([]Finding, error) {
	ociLayoutDir = strings.TrimSpace(ociLayoutDir)
	if ociLayoutDir == "" {
		return nil, errors.New("oci layout dir is required")
	}
	if byteLimit <= 0 {
		byteLimit = 10 << 20 // 10 MiB total scanned per blob
	}
	var idx ociIndex
	if err := readJSON(filepath.Join(ociLayoutDir, "index.json"), &idx); err != nil {
		return nil, err
	}
	var findings []Finding
	for _, desc := range idx.Manifests {
		mt := strings.TrimSpace(desc.MediaType)
		if mt == "" {
			continue
		}
		if mt != string(types.OCIManifestSchema1) && mt != string(types.DockerManifestSchema2) {
			continue
		}
		manPath, err := blobPath(ociLayoutDir, strings.TrimSpace(desc.Digest))
		if err != nil {
			continue
		}
		var man ociManifest
		if err := readJSON(manPath, &man); err != nil {
			continue
		}
		for _, layer := range man.Layers {
			d := strings.TrimSpace(layer.Digest)
			if d == "" {
				continue
			}
			layerPath, err := blobPath(ociLayoutDir, d)
			if err != nil {
				continue
			}
			layerFindings, _ := scanLayerTar(layerPath, byteLimit)
			for i := range layerFindings {
				layerFindings[i].Source = SourceOCI
			}
			findings = append(findings, layerFindings...)
		}
	}
	return findings, nil
}

func readJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func blobPath(layoutDir string, digest string) (string, error) {
	digest = strings.TrimSpace(digest)
	parts := strings.Split(digest, ":")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid digest %q", digest)
	}
	return filepath.Join(layoutDir, "blobs", strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])), nil
}

func scanLayerTar(path string, byteLimit int64) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = io.LimitReader(f, byteLimit)
	// most layers are gzipped tars
	head := make([]byte, 2)
	if _, err := io.ReadFull(r, head); err == nil {
		r = io.MultiReader(bytes.NewReader(head), r)
	}
	if bytes.Equal(head, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(r)
		if err == nil {
			defer gz.Close()
			r = gz
		}
	}
	tr := tar.NewReader(r)
	findings := []Finding{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return findings, nil
		}
		if h == nil || h.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(h.Name, "./")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		// quick path-based heuristics
		lower := strings.ToLower(name)
		if strings.Contains(lower, ".npmrc") || strings.Contains(lower, ".pypirc") || strings.Contains(lower, "pip.conf") || strings.Contains(lower, ".netrc") || strings.Contains(lower, "config.json") {
			findings = append(findings, Finding{
				Severity: SeverityWarn,
				Source:   SourceOCI,
				Rule:     "SUSPECT_CRED_FILE",
				Message:  "possible credential/config file in image layer",
				Location: name,
			})
		}
		// scan file content (bounded)
		limit := int64(256 << 10)
		if h.Size > 0 && h.Size < limit {
			limit = h.Size
		}
		buf, _ := io.ReadAll(io.LimitReader(tr, limit))
		text := string(buf)
		if privateKeyRE.MatchString(text) {
			findings = append(findings, Finding{
				Severity: SeverityBlock,
				Source:   SourceOCI,
				Rule:     "PRIVATE_KEY",
				Message:  "private key material detected in image layer",
				Location: name,
				Match:    "-----BEGIN PRIVATE KEY-----",
			})
		}
		if jwtRE.MatchString(text) {
			findings = append(findings, Finding{
				Severity: SeverityWarn,
				Source:   SourceOCI,
				Rule:     "JWT",
				Message:  "JWT-like token detected in image layer",
				Location: name,
				Match:    Redact(jwtRE.FindString(text)),
			})
		}
		if ghTokenRE.MatchString(text) {
			findings = append(findings, Finding{
				Severity: SeverityWarn,
				Source:   SourceOCI,
				Rule:     "GITHUB_TOKEN",
				Message:  "GitHub token-like string detected in image layer",
				Location: name,
				Match:    Redact(ghTokenRE.FindString(text)),
			})
		}
		if awsAccessKeyRE.MatchString(text) {
			findings = append(findings, Finding{
				Severity: SeverityWarn,
				Source:   SourceOCI,
				Rule:     "AWS_ACCESS_KEY_ID",
				Message:  "AWS access key id-like string detected in image layer",
				Location: name,
				Match:    Redact(awsAccessKeyRE.FindString(text)),
			})
		}
	}
	return findings, nil
}
