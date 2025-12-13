// signing_test.go exercises the signing helpers to guarantee envelopes verify as expected.
package signing

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.bin")
	if err := os.WriteFile(file, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	env, err := SignFile(file, priv, pub)
	if err != nil {
		t.Fatalf("sign file: %v", err)
	}
	if env.Digest == "" || env.Signature == "" {
		t.Fatalf("missing digest or signature")
	}
	if err := VerifyFile(file, env, pub); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}

func TestKeyPersistence(t *testing.T) {
	dir := t.TempDir()
	privPath := filepath.Join(dir, "priv.pem")
	pubPath := filepath.Join(dir, "pub.pem")
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := SavePrivateKey(privPath, priv); err != nil {
		t.Fatalf("save private: %v", err)
	}
	if err := SavePublicKey(pubPath, pub); err != nil {
		t.Fatalf("save public: %v", err)
	}
	loadedPriv, loadedPub, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("load private: %v", err)
	}
	if !ed25519.PrivateKey(priv).Equal(loadedPriv) {
		t.Fatalf("private key mismatch")
	}
	pubOnly, err := LoadPublicKey(pubPath)
	if err != nil {
		t.Fatalf("load public: %v", err)
	}
	if !ed25519.PublicKey(pub).Equal(pubOnly) {
		t.Fatalf("public key mismatch")
	}
	if !ed25519.PublicKey(pub).Equal(loadedPub) {
		t.Fatalf("derived public mismatch")
	}
}
