package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func SHA256Hex(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func normalizeForHash(manifest string) string {
	// Keep it simple/deterministic: normalize newlines and trim.
	manifest = strings.ReplaceAll(manifest, "\r\n", "\n")
	return strings.TrimSpace(manifest) + "\n"
}

func ManifestDigestSHA256(manifest string) string {
	return SHA256Hex(normalizeForHash(manifest))
}
