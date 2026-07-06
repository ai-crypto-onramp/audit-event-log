package payload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// skipIfNoS3 skips tests requiring a live S3-compatible endpoint. The
// acceptance criterion "S3 bucket accepts a payload and rejects an overwrite
// during the Object Lock retention window" is exercised in CI where
// S3_ENDPOINT (or AWS_S3_ENDPOINT) is set; locally these tests skip.
func skipIfNoS3(t *testing.T) (Config, S3Options) {
	t.Helper()
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("AWS_S3_ENDPOINT")
	}
	if endpoint == "" {
		t.Skip("S3_ENDPOINT/AWS_S3_ENDPOINT not set; skipping live S3 integration test")
	}
	bucket := os.Getenv("PAYLOAD_BUCKET")
	if bucket == "" {
		bucket = "audit-payloads-test"
	}
	cfg := Config{
		Bucket:                    bucket,
		StorageClass:              "STANDARD",
		Retention:                 1 * time.Hour,
		GlacierTransitionDays:     90,
		DeepArchiveTransitionDays: 365,
	}
	opts := S3Options{
		Region:       os.Getenv("AWS_REGION"),
		EndpointURL:   endpoint,
		AccessKey:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretKey:     os.Getenv("S3_SECRET_ACCESS_KEY"),
		UsePathStyle:  os.Getenv("S3_USE_PATH_STYLE") == "true",
	}
	return cfg, opts
}

func TestS3StoreProvisionLifecycle(t *testing.T) {
	cfg, opts := skipIfNoS3(t)
	store, err := NewS3Store(cfg, opts)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	lc := store.Lifecycle()
	if lc.GlacierTransitionDays != 90 || lc.DeepArchiveTransitionDays != 365 {
		t.Fatalf("lifecycle = %+v want {90 365}", lc)
	}
	if got := lc.String(); !strings.Contains(got, "90") || !strings.Contains(got, "365") {
		t.Fatalf("lifecycle.String() = %q", got)
	}
}

func TestS3StorePutRejectsOverwriteDuringRetention(t *testing.T) {
	cfg, opts := skipIfNoS3(t)
	store, err := NewS3Store(cfg, opts)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	key := "events/s3-lock-test-" + time.Now().Format("20060102150405") + ".json"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("first")), PutOptions{}); err != nil {
		t.Fatalf("first put: %v", err)
	}
	err = store.Put(ctx, key, bytes.NewReader([]byte("second")), PutOptions{})
	if err == nil {
		t.Fatalf("expected overwrite to be rejected by Object Lock, got nil")
	}
	if errors.Is(err, ErrObjectLocked) {
		return
	}
	// S3 returns its own "under retention" error rather than our sentinel;
	// accept it as long as the message indicates the object is locked.
	if strings.Contains(strings.ToLower(err.Error()), "retention") ||
		strings.Contains(strings.ToLower(err.Error()), "locked") {
		return
	}
	t.Fatalf("overwrite err = %v, want a retention/lock rejection", err)
}

func TestS3StorePutGetRoundtrip(t *testing.T) {
	cfg, opts := skipIfNoS3(t)
	store, err := NewS3Store(cfg, opts)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	key := "events/s3-roundtrip-" + time.Now().Format("20060102150405") + ".json"
	body := []byte("hello audit")
	if err := store.Put(ctx, key, bytes.NewReader(body), PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestS3StoreNilContexts(t *testing.T) {
	cfg, opts := skipIfNoS3(t)
	store, err := NewS3Store(cfg, opts)
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	if err := store.Provision(nil); err == nil {
		t.Fatalf("expected nil ctx error on Provision")
	}
	if err := store.Put(nil, "k", bytes.NewReader([]byte("x")), PutOptions{}); err == nil {
		t.Fatalf("expected nil ctx error on Put")
	}
	if _, err := store.Get(nil, "k"); err == nil {
		t.Fatalf("expected nil ctx error on Get")
	}
}