package buildkit

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/types"
)

type OCILayerInfo struct {
	ImageDigest string
	Digest      string
	Size        int64
	MediaType   string
}

// TopOCILayers returns the largest layers (by compressed size) for images present in an OCI layout directory.
// Best-effort: ignores non-image manifests and any blobs it cannot parse.
func TopOCILayers(ociLayoutDir string, topN int) ([]OCILayerInfo, error) {
	ociLayoutDir = strings.TrimSpace(ociLayoutDir)
	if ociLayoutDir == "" {
		return nil, fmt.Errorf("oci layout dir is empty")
	}
	if topN <= 0 {
		topN = 10
	}
	var root ociIndex
	if err := readJSON(filepath.Join(ociLayoutDir, "index.json"), &root); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	layers := make([]OCILayerInfo, 0)

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

		if mt == string(types.OCIImageIndex) || mt == string(types.DockerManifestList) {
			parsed, err := parseSHA256Digest(digest)
			if err != nil {
				return nil
			}
			path := blobPath(ociLayoutDir, parsed)
			var child ociIndex
			if err := readJSON(path, &child); err != nil {
				return nil
			}
			return walkIndex(child)
		}

		if mt != string(types.OCIManifestSchema1) && mt != string(types.DockerManifestSchema2) {
			return nil
		}

		parsed, err := parseSHA256Digest(digest)
		if err != nil {
			return nil
		}
		var man ociManifest
		if err := readJSON(blobPath(ociLayoutDir, parsed), &man); err != nil {
			return nil
		}
		// Heuristic: ignore attestation artifact manifests.
		if annotations != nil {
			if t := strings.TrimSpace(annotations["vnd.docker.reference.type"]); t == "attestation-manifest" {
				return nil
			}
		}
		for _, layer := range man.Layers {
			if strings.TrimSpace(layer.Digest) == "" || layer.Size <= 0 {
				continue
			}
			layers = append(layers, OCILayerInfo{
				ImageDigest: digest,
				Digest:      layer.Digest,
				Size:        layer.Size,
				MediaType:   layer.MediaType,
			})
		}
		return nil
	}

	if err := walkIndex(root); err != nil {
		return nil, err
	}
	sort.Slice(layers, func(i, j int) bool {
		if layers[i].Size == layers[j].Size {
			return layers[i].Digest < layers[j].Digest
		}
		return layers[i].Size > layers[j].Size
	})
	if len(layers) > topN {
		layers = layers[:topN]
	}
	return layers, nil
}
