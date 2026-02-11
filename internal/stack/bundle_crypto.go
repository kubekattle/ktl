package stack

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BundleKey struct {
	APIVersion string `json:"apiVersion"`
	Type       string `json:"type"` // ed25519
	PublicKey  string `json:"publicKey"`
	// PrivateKey is base64 of ed25519.PrivateKey (64 bytes).
	PrivateKey string `json:"privateKey,omitempty"`
	// Seed is base64 of 32-byte seed (optional alternative).
	Seed string `json:"seed,omitempty"`
}

type BundleSignature struct {
	APIVersion     string `json:"apiVersion"`
	CreatedAt      string `json:"createdAt"`
	Algorithm      string `json:"algorithm"` // ed25519
	PublicKey      string `json:"publicKey"`
	ManifestSHA256 string `json:"manifestSha256"`
	Signature      string `json:"signature"`
}

func GenerateEd25519Key() (*BundleKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &BundleKey{
		APIVersion: "ktl.dev/bundle-key/v1",
		Type:       "ed25519",
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
	}, nil
}

func LoadBundleKey(path string) (*BundleKey, ed25519.PublicKey, ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	var k BundleKey
	if err := json.Unmarshal(raw, &k); err == nil && strings.TrimSpace(k.Type) != "" {
		pub, priv, err := keyMaterialFromBundleKey(&k)
		return &k, pub, priv, err
	}
	// Fallback: raw base64 private key or seed.
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil, nil, nil, fmt.Errorf("empty key file")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse key file: expected JSON or base64: %w", err)
	}
	switch len(b) {
	case ed25519.PrivateKeySize:
		priv := ed25519.PrivateKey(b)
		pub := priv.Public().(ed25519.PublicKey)
		return &BundleKey{APIVersion: "ktl.dev/bundle-key/v1", Type: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(pub), PrivateKey: s}, pub, priv, nil
	case ed25519.SeedSize:
		priv := ed25519.NewKeyFromSeed(b)
		pub := priv.Public().(ed25519.PublicKey)
		return &BundleKey{APIVersion: "ktl.dev/bundle-key/v1", Type: "ed25519", PublicKey: base64.StdEncoding.EncodeToString(pub), Seed: s}, pub, priv, nil
	default:
		return nil, nil, nil, fmt.Errorf("unexpected key material length %d (want 32 seed or 64 private key)", len(b))
	}
}

func keyMaterialFromBundleKey(k *BundleKey) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	if k == nil {
		return nil, nil, fmt.Errorf("key is nil")
	}
	if strings.TrimSpace(k.Type) != "" && strings.TrimSpace(k.Type) != "ed25519" {
		return nil, nil, fmt.Errorf("unsupported key type %q", k.Type)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k.PublicKey))
	if err != nil {
		return nil, nil, fmt.Errorf("decode publicKey: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("publicKey has length %d (want %d)", len(pubBytes), ed25519.PublicKeySize)
	}
	pub := ed25519.PublicKey(pubBytes)

	if strings.TrimSpace(k.PrivateKey) != "" {
		privBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k.PrivateKey))
		if err != nil {
			return nil, nil, fmt.Errorf("decode privateKey: %w", err)
		}
		if len(privBytes) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("privateKey has length %d (want %d)", len(privBytes), ed25519.PrivateKeySize)
		}
		return pub, ed25519.PrivateKey(privBytes), nil
	}
	if strings.TrimSpace(k.Seed) != "" {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k.Seed))
		if err != nil {
			return nil, nil, fmt.Errorf("decode seed: %w", err)
		}
		if len(seed) != ed25519.SeedSize {
			return nil, nil, fmt.Errorf("seed has length %d (want %d)", len(seed), ed25519.SeedSize)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		return pub, priv, nil
	}
	return pub, nil, nil
}

func sha256File(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func writeJSONAtomicFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func signManifest(priv ed25519.PrivateKey, manifestJSON []byte) (*BundleSignature, error) {
	if len(priv) == 0 {
		return nil, fmt.Errorf("private key is required")
	}
	sum := sha256.Sum256(manifestJSON)
	sig := ed25519.Sign(priv, sum[:])
	pub := priv.Public().(ed25519.PublicKey)
	return &BundleSignature{
		APIVersion:     "ktl.dev/bundle-signature/v1",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		Algorithm:      "ed25519",
		PublicKey:      base64.StdEncoding.EncodeToString(pub),
		ManifestSHA256: hex.EncodeToString(sum[:]),
		Signature:      base64.StdEncoding.EncodeToString(sig),
	}, nil
}
