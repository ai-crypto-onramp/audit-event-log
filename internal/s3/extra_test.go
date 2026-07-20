package s3

import (
	"bytes"
	"errors"
	"testing"
)

func TestErrNotFoundError(t *testing.T) {
	e := &ErrNotFound{Key: "k"}
	if got := e.Error(); got != "s3store: not found: k" {
		t.Errorf("error: %q", got)
	}
}

func TestErrRetentionActiveError(t *testing.T) {
	e := &ErrRetentionActive{Key: "k"}
	if got := e.Error(); got != "s3store: object under retention: k" {
		t.Errorf("error: %q", got)
	}
}

func TestFakePutReadError(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "k"}, errReader{}); err == nil {
		t.Fatal("expected read error")
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("read boom") }

func TestFakePresignGetNotFound(t *testing.T) {
	f := NewFake()
	if _, err := f.PresignGet(t.Context(), "bkt", "nope", 0); err == nil {
		t.Fatal("expected not found")
	}
}

func TestFakeDeleteNotFound(t *testing.T) {
	f := NewFake()
	if err := f.Delete(t.Context(), "bkt", "nope"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestFakeDeleteAfterRetentionZero(t *testing.T) {
	// RetentionDays == 0 means no retention; delete should succeed.
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", RetentionDays: 0}, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := f.Delete(t.Context(), "bkt", "e1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}