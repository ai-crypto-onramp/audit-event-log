package s3

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestFakePutGet(t *testing.T) {
	f := NewFake()
	body := []byte("payload")
	key, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", RetentionDays: 2555}, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if key != "e1" {
		t.Errorf("key: %q", key)
	}
	got, err := f.Get(t.Context(), "bkt", "e1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body: %q", got)
	}
}

func TestFakeRetentionBlocksOverwrite(t *testing.T) {
	f := NewFake()
	body := []byte("first")
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", RetentionDays: 2555}, bytes.NewReader(body)); err != nil {
		t.Fatalf("put first: %v", err)
	}
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", RetentionDays: 2555}, bytes.NewReader([]byte("second"))); err == nil {
		t.Fatal("expected overwrite blocked by retention")
	} else {
		var rae *ErrRetentionActive
		if !errors.As(err, &rae) {
			t.Fatalf("expected ErrRetentionActive, got %v", err)
		}
	}
}

func TestFakeDeleteBlockedByRetention(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", RetentionDays: 2555}, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := f.Delete(t.Context(), "bkt", "e1"); err == nil {
		t.Fatal("expected delete blocked")
	}
}

func TestFakeDeleteBlockedByLegalHold(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", LegalHold: true}, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := f.Delete(t.Context(), "bkt", "e1"); err == nil {
		t.Fatal("expected delete blocked")
	}
}

func TestFakeDeleteAfterRetentionExpires(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", RetentionDays: 1}, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Mark the object as created 2 days ago so retention expired.
	f.objects["bkt/e1"].createdAt = time.Now().UTC().Add(-48 * time.Hour)
	if err := f.Delete(t.Context(), "bkt", "e1"); err != nil {
		t.Fatalf("delete after retention expired: %v", err)
	}
}

func TestFakeNotFound(t *testing.T) {
	f := NewFake()
	if _, err := f.Get(t.Context(), "bkt", "nope"); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := f.Head(t.Context(), "bkt", "nope"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestFakePresignGet(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1"}, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	url, err := f.PresignGet(t.Context(), "bkt", "e1", 5*time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if url == "" {
		t.Error("empty presigned url")
	}
}

func TestFakeHead(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1", StorageClass: "GLACIER", RetentionDays: 100, LegalHold: true}, bytes.NewReader([]byte("xyz"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	o, err := f.Head(t.Context(), "bkt", "e1")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if o.Size != 3 || o.StorageClass != "GLACIER" || o.RetentionDays != 100 || !o.LegalHold {
		t.Errorf("head: %+v", o)
	}
}

func TestFakeApplyTransition(t *testing.T) {
	f := NewFake()
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e1"}, bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Make e1 old enough for the Glacier transition (90 days).
	f.objects["bkt/e1"].createdAt = time.Now().UTC().Add(-100 * 24 * time.Hour)
	if _, err := f.Put(t.Context(), "bkt", PutOptions{Key: "e2"}, bytes.NewReader([]byte("y"))); err != nil {
		t.Fatalf("put: %v", err)
	}
	n := f.ApplyTransition(90, "STANDARD", "GLACIER")
	if n != 1 {
		t.Errorf("transitioned: %d", n)
	}
	o, _ := f.Head(t.Context(), "bkt", "e1")
	if o.StorageClass != "GLACIER" {
		t.Errorf("e1 class: %q", o.StorageClass)
	}
	// Now apply Deep Archive to objects older than 365d.
	f.objects["bkt/e1"].createdAt = time.Now().UTC().Add(-400 * 24 * time.Hour)
	n = f.ApplyTransition(365, "GLACIER", "DEEP_ARCHIVE")
	if n != 1 {
		t.Errorf("deep archive: %d", n)
	}
	o, _ = f.Head(t.Context(), "bkt", "e1")
	if o.StorageClass != "DEEP_ARCHIVE" {
		t.Errorf("e1 class: %q", o.StorageClass)
	}
}