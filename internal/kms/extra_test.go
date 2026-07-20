package kms

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestLoadPrivateKeyPEMPKCS8NotRSA(t *testing.T) {
	// Generate an EC key and encode it as PKCS8 PEM. The loader should
	// parse it as PKCS8 but reject it because it is not RSA.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	f := NewFake("k")
	if err := f.LoadPrivateKeyPEM(pemBytes); err == nil {
		t.Fatal("expected error for non-RSA PKCS8 key")
	}
}