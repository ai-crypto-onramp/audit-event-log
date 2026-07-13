// Package kms defines the signing surface for periodic chain root
// anchors. The in-memory Fake is used by tests and as the default when
// KMS_KEY_ID is empty; the real KMS adapter lives in the kmsadapter
// subpackage.
package kms

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
)

// Signer signs a digest with a configured KMS key.
type Signer interface {
	// Sign signs digest (a 32-byte SHA-256) and returns the signature
	// bytes and the KMS key id used.
	Sign(digest []byte) (signature []byte, keyID string, err error)
	// Verify verifies that signature was produced by this key over digest.
	Verify(digest, signature []byte) (bool, error)
	// KeyID returns the configured key id.
	KeyID() string
}

// Fake is an in-memory Signer backed by an auto-generated RSA key. It is
// sufficient for unit tests and local dev; production deployments use the
// real KMS adapter.
type Fake struct {
	mu  sync.Mutex
	key *rsa.PrivateKey
	id  string
}

// NewFake returns a Fake Signer with the given key id. If pemKey is empty an
// in-memory key is generated deterministically (256-bit) for tests; pass a
// real PEM to reuse a stable key.
func NewFake(keyID string) *Fake {
	return &Fake{id: keyID}
}

// KeyID returns the configured key id.
func (f *Fake) KeyID() string { return f.id }

// Sign signs the digest with the in-memory RSA key.
func (f *Fake) Sign(digest []byte) ([]byte, string, error) {
	if err := f.ensureKey(); err != nil {
		return nil, "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sig, err := rsa.SignPSS(rand.Reader, f.key, crypto.SHA256, digest, nil)
	if err != nil {
		return nil, "", fmt.Errorf("kms: sign: %w", err)
	}
	return sig, f.id, nil
}

// Verify checks the signature.
func (f *Fake) Verify(digest, signature []byte) (bool, error) {
	if err := f.ensureKey(); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := rsa.VerifyPSS(&f.key.PublicKey, crypto.SHA256, digest, signature, nil); err != nil {
		return false, nil
	}
	return true, nil
}

// ensureKey lazily generates a 2048-bit RSA key on first use. The first call
// is slow (~100ms); subsequent calls are no-ops.
func (f *Fake) ensureKey() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.key != nil {
		return nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("kms: generate key: %w", err)
	}
	f.key = key
	return nil
}

// LoadPrivateKeyPEM initializes the Fake from a PEM-encoded RSA private key.
// Used to keep a stable key across restarts in tests if desired.
func (f *Fake) LoadPrivateKeyPEM(pemKey []byte) error {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return errors.New("kms: no PEM block")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		if k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes); err2 == nil {
			rk, ok := k.(*rsa.PrivateKey)
			if !ok {
				return errors.New("kms: PKCS8 key is not RSA")
			}
			key = rk
		} else {
			return fmt.Errorf("kms: parse key: %w", err)
		}
	}
	f.mu.Lock()
	f.key = key
	f.mu.Unlock()
	return nil
}

// PublicKeyPEM returns the Fake's public key as PEM, useful for sharing with
// a verifier in tests.
func (f *Fake) PublicKeyPEM() ([]byte, error) {
	if err := f.ensureKey(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	der, err := x509.MarshalPKIXPublicKey(&f.key.PublicKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// _ = crypto.SHA256 keeps the import without lint noise.
var _ = crypto.SHA256

// DigestSHA256 is a convenience helper that hashes data and returns the
// 32-byte digest for signing.
func DigestSHA256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}