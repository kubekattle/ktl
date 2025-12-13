// signing.go wraps signing and verification helpers for ktl .k8s artifacts and attachments.
package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// EnvelopeVersion identifies the signature schema used for ktl archives.
	EnvelopeVersion = "ktl.sig.v1"
	defaultAlgo     = "ed25519"
)

// Envelope stores the detached signature metadata for an app archive.
type Envelope struct {
	Version   string    `json:"version"`
	Algorithm string    `json:"algorithm"`
	Digest    string    `json:"digest"`
	Signature string    `json:"signature"`
	PublicKey string    `json:"publicKey,omitempty"`
	KeyID     string    `json:"keyId,omitempty"`
	SignedAt  time.Time `json:"signedAt"`
}

// GenerateKeyPair returns a fresh Ed25519 keypair.
func GenerateKeyPair() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return priv, pub, nil
}

// SavePrivateKey writes the private key to disk in PKCS8/PEM form.
func SavePrivateKey(path string, key ed25519.PrivateKey) error {
	if err := ensureParentDir(path); err != nil {
		return err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

// SavePublicKey writes the public key to disk in PKIX/PEM form.
func SavePublicKey(path string, key ed25519.PublicKey) error {
	if err := ensureParentDir(path); err != nil {
		return err
	}
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o644)
}

// LoadPrivateKey reads an Ed25519 private key from disk.
func LoadPrivateKey(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, nil, fmt.Errorf("file %s does not contain a private key", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("private key %s is not Ed25519", path)
	}
	return priv, ed25519.PrivateKey(priv).Public().(ed25519.PublicKey), nil
}

// LoadPublicKey reads an Ed25519 public key from disk.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("file %s does not contain a public key", path)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key %s is not Ed25519", path)
	}
	return pub, nil
}

// SignFile calculates the digest for path and returns a detached signature envelope.
func SignFile(path string, key ed25519.PrivateKey, pub ed25519.PublicKey) (Envelope, error) {
	if len(key) == 0 {
		return Envelope{}, fmt.Errorf("signing key is empty")
	}
	digest, err := fileDigest(path)
	if err != nil {
		return Envelope{}, err
	}
	message := messageForDigest(digest)
	sig := ed25519.Sign(key, message)
	if pub == nil {
		pub = key.Public().(ed25519.PublicKey)
	}
	env := Envelope{
		Version:   EnvelopeVersion,
		Algorithm: defaultAlgo,
		Digest:    digest,
		Signature: base64.StdEncoding.EncodeToString(sig),
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		KeyID:     keyID(pub),
		SignedAt:  time.Now().UTC(),
	}
	return env, nil
}

// VerifyFile validates the detached envelope signature for path.
func VerifyFile(path string, env Envelope, pub ed25519.PublicKey) error {
	if strings.TrimSpace(env.Version) == "" {
		return fmt.Errorf("signature envelope missing version")
	}
	if env.Version != EnvelopeVersion {
		return fmt.Errorf("unsupported signature version %s", env.Version)
	}
	if env.Algorithm != defaultAlgo {
		return fmt.Errorf("unsupported signature algorithm %s", env.Algorithm)
	}
	digest, err := fileDigest(path)
	if err != nil {
		return err
	}
	if digest != env.Digest {
		return fmt.Errorf("digest mismatch: expected %s, got %s", env.Digest, digest)
	}
	signature, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if pub == nil {
		if env.PublicKey == "" {
			return fmt.Errorf("public key not provided")
		}
		pub, err = decodePublicKeyBase64(env.PublicKey)
		if err != nil {
			return err
		}
	}
	message := messageForDigest(digest)
	if !ed25519.Verify(pub, message, signature) {
		return fmt.Errorf("signature verification failed")
	}
	return nil
}

// SaveEnvelope writes the signature envelope JSON to disk.
func SaveEnvelope(path string, env Envelope) error {
	if err := ensureParentDir(path); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}

// LoadEnvelope reads a signature envelope JSON file.
func LoadEnvelope(path string) (Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// FileDigest returns the sha256 digest string for the given path.
func FileDigest(path string) (string, error) {
	return fileDigest(path)
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" || dir == string(filepath.Separator) {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func messageForDigest(digest string) []byte {
	return []byte(fmt.Sprintf("%s:%s", EnvelopeVersion, digest))
}

func keyID(pub ed25519.PublicKey) string {
	if len(pub) == 0 {
		return ""
	}
	sum := sha256.Sum256(pub)
	return fmt.Sprintf("ed25519:%x", sum[:6])
}

func decodePublicKeyBase64(value string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	return ed25519.PublicKey(raw), nil
}
