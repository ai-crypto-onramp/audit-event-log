package kms

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestFakeSignAndVerify(t *testing.T) {
	s := NewFake("alias/test")
	digest := DigestSHA256([]byte("hello"))
	sig, keyID, err := s.Sign(digest)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if keyID != "alias/test" {
		t.Errorf("key id: %q", keyID)
	}
	ok, err := s.Verify(digest, sig)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !ok {
		t.Fatal("verify should succeed for valid signature")
	}
	// Wrong digest
	if ok, _ := s.Verify(DigestSHA256([]byte("other")), sig); ok {
		t.Fatal("verify should fail for wrong digest")
	}
	// Tampered signature
	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[0] ^= 0x01
	if ok, _ := s.Verify(digest, tampered); ok {
		t.Fatal("verify should fail for tampered signature")
	}
}

func TestFakeKeyID(t *testing.T) {
	s := NewFake("alias/abc")
	if s.KeyID() != "alias/abc" {
		t.Errorf("KeyID: %q", s.KeyID())
	}
}

func TestDigestSHA256(t *testing.T) {
	d := DigestSHA256([]byte("x"))
	if len(d) != 32 {
		t.Fatalf("len: %d", len(d))
	}
}

func TestPublicKeyPEM(t *testing.T) {
	s := NewFake("k")
	pem, err := s.PublicKeyPEM()
	if err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	if len(pem) == 0 {
		t.Fatal("empty pem")
	}
}

func TestSignErrorOnShortDigest(t *testing.T) {
	s := NewFake("k")
	if _, _, err := s.Sign(nil); err == nil {
		t.Fatal("expected error signing nil digest")
	}
}

func TestVerifyNilSignature(t *testing.T) {
	s := NewFake("k")
	digest := DigestSHA256([]byte("x"))
	if ok, err := s.Verify(digest, nil); err != nil || ok {
		t.Fatalf("verify nil sig: ok=%v err=%v", ok, err)
	}
}

func TestLoadPrivateKeyPEMPKCS1(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})

	f := NewFake("alias/pkcs1")
	if err := f.LoadPrivateKeyPEM(pemBytes); err != nil {
		t.Fatalf("load: %v", err)
	}
	digest := DigestSHA256([]byte("msg"))
	sig, _, err := f.Sign(digest)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	ok, err := f.Verify(digest, sig)
	if err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}
}

func TestLoadPrivateKeyPEMPKCS8(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	f := NewFake("alias/pkcs8")
	if err := f.LoadPrivateKeyPEM(pemBytes); err != nil {
		t.Fatalf("load: %v", err)
	}
	digest := DigestSHA256([]byte("msg"))
	if _, _, err := f.Sign(digest); err != nil {
		t.Fatalf("sign: %v", err)
	}
}

func TestLoadPrivateKeyPEMNoBlock(t *testing.T) {
	f := NewFake("k")
	if err := f.LoadPrivateKeyPEM([]byte("not pem")); err == nil {
		t.Fatal("expected error for no PEM block")
	}
}

func TestLoadPrivateKeyPEMGarbage(t *testing.T) {
	f := NewFake("k")
	body := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("garbage")})
	if err := f.LoadPrivateKeyPEM(body); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestPublicKeyPEMRoundTrip(t *testing.T) {
	f := NewFake("k")
	pubPEM, err := f.PublicKeyPEM()
	if err != nil {
		t.Fatalf("pubkey: %v", err)
	}
	block, _ := pem.Decode(pubPEM)
	if block == nil {
		t.Fatal("no PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := pub.(*rsa.PublicKey); !ok {
		t.Fatalf("not rsa public key: %T", pub)
	}
}

func TestEnsureKeyConcurrency(t *testing.T) {
	f := NewFake("k")
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			if err := f.ensureKey(); err != nil {
				errs <- err
				return
			}
			digest := DigestSHA256([]byte("x"))
			if _, _, err := f.Sign(digest); err != nil {
				errs <- err
			} else {
				errs <- nil
			}
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent: %v", err)
		}
	}
}

func TestSignPSSAndVerifyWithCryptoRSA(t *testing.T) {
	f := NewFake("k")
	digest := DigestSHA256([]byte("verify-external"))
	sig, _, err := f.Sign(digest)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	pubPEM, _ := f.PublicKeyPEM()
	block, _ := pem.Decode(pubPEM)
	pubAny, _ := x509.ParsePKIXPublicKey(block.Bytes)
	pub := pubAny.(*rsa.PublicKey)
	if err := rsa.VerifyPSS(pub, crypto.SHA256, digest, sig, nil); err != nil {
		t.Fatalf("external verify: %v", err)
	}
}

func TestDigestSHA256MatchesSHA256(t *testing.T) {
	want := sha256.Sum256([]byte("abc"))
	got := DigestSHA256([]byte("abc"))
	if string(got) != string(want[:]) {
		t.Fatal("digest mismatch")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	a := NewFake("a")
	b := NewFake("b")
	digest := DigestSHA256([]byte("x"))
	sig, _, _ := a.Sign(digest)
	if ok, _ := b.Verify(digest, sig); ok {
		t.Fatal("verify with different key should fail")
	}
}

func TestParsePEMErrorBranch(t *testing.T) {
	f := NewFake("k")
	err := f.LoadPrivateKeyPEM([]byte("----BEGIN BAD----\nZ29vZA==\n----END BAD----\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no PEM block") && !strings.Contains(err.Error(), "parse") {
		t.Fatalf("unexpected err: %v", err)
	}
}