package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// DigestNormalizedManifest computes a stable digest for a Helm manifest after applying
// the same normalization rules used by plan diffing (metadata/status stripping and
// deterministic list ordering).
//
// The digest is order-insensitive across documents and is suitable for cheap
// "does the stored release manifest match what we expect?" checks.
func DigestNormalizedManifest(manifest string) (digest string, hasHooks bool, err error) {
	objects, err := parseManifestObjects(manifest)
	if err != nil {
		return "", false, err
	}
	if len(objects) == 0 {
		sum := sha256.Sum256([]byte("ktl.manifest-digest.v1\x00empty\x00"))
		return "sha256:" + hex.EncodeToString(sum[:]), false, nil
	}

	sort.SliceStable(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.manifest-digest.v1")
	for _, obj := range objects {
		if obj.IsHook {
			hasHooks = true
		}
		write(strings.TrimSpace(obj.Key))
		_, _ = h.Write(obj.CanonicalJSON)
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), hasHooks, nil
}

func FormatDigestMismatch(want, got string) string {
	want = strings.TrimSpace(want)
	got = strings.TrimSpace(got)
	if want == got {
		return ""
	}
	if want == "" || got == "" {
		return fmt.Sprintf("digest mismatch (want=%q got=%q)", want, got)
	}
	return fmt.Sprintf("digest mismatch (want=%s got=%s)", want, got)
}
