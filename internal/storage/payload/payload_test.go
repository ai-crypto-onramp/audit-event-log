package payload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func baseCfg() Config {
	return Config{
		Bucket:                    "audit-payloads-test",
		StorageClass:              "STANDARD",
		Retention:                 24 * time.Hour,
		GlacierTransitionDays:     90,
		DeepArchiveTransitionDays: 365,
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		mut  func(Config) Config
		want string
	}{
		{"empty bucket", func(c Config) Config { c.Bucket = ""; return c }, "bucket name is required"},
		{"zero retention", func(c Config) Config { c.Retention = 0; return c }, "retention must be positive"},
		{"zero glacier", func(c Config) Config { c.GlacierTransitionDays = 0; return c }, "glacier transition days must be positive"},
		{"deep before glacier", func(c Config) Config { c.DeepArchiveTransitionDays = 30; return c }, "deep archive transition must be after glacier transition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mut(baseCfg()).Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
	if err := baseCfg().Validate(); err != nil {
		t.Fatalf("base cfg should be valid: %v", err)
	}
}

func TestMemoryStoreLifecycleCodified(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	lc := store.Lifecycle()
	if lc.GlacierTransitionDays != 90 || lc.DeepArchiveTransitionDays != 365 {
		t.Fatalf("lifecycle = %+v want {90 365}", lc)
	}
	if got := lc.String(); !strings.Contains(got, "90") || !strings.Contains(got, "365") {
		t.Fatalf("lifecycle.String() = %q", got)
	}
}

func TestMemoryStoreProvisionIdempotent(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision 1: %v", err)
	}
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision 2: %v", err)
	}
}

func TestMemoryStorePutBeforeProvisionFails(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	err = store.Put(context.Background(), "k", bytes.NewReader([]byte("x")), PutOptions{})
	if err == nil || !strings.Contains(err.Error(), "not provisioned") {
		t.Fatalf("err = %v, want provisioned error", err)
	}
}

func TestMemoryStorePutGetRoundtrip(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	payload := []byte("hello audit")
	if err := store.Put(ctx, "events/abc.json", bytes.NewReader(payload), PutOptions{}); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := store.Get(ctx, "events/abc.json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q want %q", got, payload)
	}
}

func TestMemoryStoreRejectsOverwriteDuringRetention(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	key := "events/locked.json"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("first")), PutOptions{}); err != nil {
		t.Fatalf("first put: %v", err)
	}
	err = store.Put(ctx, key, bytes.NewReader([]byte("second")), PutOptions{})
	if !errors.Is(err, ErrObjectLocked) {
		t.Fatalf("second put err = %v, want ErrObjectLocked", err)
	}
}

func TestMemoryStoreRejectsOverwriteEvenAfterRetention(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	ctx := context.Background()
	if err := store.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	key := "events/worm.json"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("first")), PutOptions{Retention: 1 * time.Hour}); err != nil {
		t.Fatalf("first put: %v", err)
	}
	store.AdvanceTime(2 * time.Hour)
	err = store.Put(ctx, key, bytes.NewReader([]byte("second")), PutOptions{})
	if !errors.Is(err, ErrObjectLocked) {
		t.Fatalf("post-retention overwrite err = %v, want ErrObjectLocked (compliance WORM)", err)
	}
}

func TestMemoryStoreGetMissing(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	if err := store.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	_, err = store.Get(context.Background(), "nope")
	if err == nil {
		t.Fatalf("expected error for missing object")
	}
}

func TestMemoryStoreNilContexts(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	if err := store.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := store.Put(nil, "k", bytes.NewReader([]byte("x")), PutOptions{}); err == nil {
		t.Fatalf("expected nil ctx error on Put")
	}
	if _, err := store.Get(nil, "k"); err == nil {
		t.Fatalf("expected nil ctx error on Get")
	}
	if err := store.Provision(nil); err == nil {
		t.Fatalf("expected nil ctx error on Provision")
	}
}

func TestMemoryStoreEmptyKey(t *testing.T) {
	store, err := NewMemoryStore(baseCfg())
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	if err := store.Provision(context.Background()); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := store.Put(context.Background(), "", bytes.NewReader([]byte("x")), PutOptions{}); err == nil {
		t.Fatalf("expected empty key error")
	}
}

func TestLifecycleString(t *testing.T) {
	lc := Lifecycle{90, 365}
	if !strings.Contains(lc.String(), "90") || !strings.Contains(lc.String(), "365") {
		t.Fatalf("String = %q", lc.String())
	}
}