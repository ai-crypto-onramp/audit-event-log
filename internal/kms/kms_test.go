package kms

import (
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