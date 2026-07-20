package kmsadapter

import "testing"

func TestKeyID(t *testing.T) {
	c := New(nil, "alias/x")
	if c.KeyID() != "alias/x" {
		t.Errorf("key id: %q", c.KeyID())
	}
}

func TestSignWrongDigestLength(t *testing.T) {
	c := New(nil, "alias/x")
	if _, _, err := c.Sign([]byte("short")); err == nil {
		t.Fatal("expected error for non-32-byte digest")
	}
	if _, _, err := c.Sign(nil); err == nil {
		t.Fatal("expected error for nil digest")
	}
}