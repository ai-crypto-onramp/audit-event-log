// Package payload implements the append-only S3 payload store for audit event
// payloads. It provisions a bucket with Object Lock (compliance mode) for the
// configured retention period and applies the Standard -> Glacier ->
// Deep Archive lifecycle on bucket creation.
//
// Two implementations are provided:
//
//   - S3Store talks to AWS S3 via the AWS SDK for Go v2.
//   - MemoryStore is an in-memory equivalent used by tests and local
//     development. It enforces the same Object Lock semantics: an object under
//     retention cannot be overwritten or deleted, mirroring the rejection an
//     S3 client sees when the retention window has not elapsed.
//
// The interface is intentionally narrow so that Stage 3's ingest path can
// depend on Store without caring which backend is wired up.
package payload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrObjectLocked is returned when an overwrite or delete is attempted on an
// object that is still inside its Object Lock retention window. S3 returns the
// equivalent "Object is under retention" error for the same operation.
var ErrObjectLocked = errors.New("payload: object is under Object Lock retention")

// PutOptions controls how an object is written. StorageClass and Retention
// mirror the S3 PutObject + PutObjectRetention parameters.
type PutOptions struct {
	StorageClass string
	Retention    time.Duration
}

// Store is the append-only payload store. Put writes a new object; Get
// streams it back. Delete is intentionally not present on the public surface
// to make WORM-violating operations unrepresentable at the type level.
type Store interface {
	Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// BucketProvisioner creates the bucket with Object Lock enabled and applies
// the lifecycle policy. Real implementations (S3Store) talk to AWS; the
// MemoryStore is provisioned in-process and never needs to be called for
// local dev, but implementing the same surface lets tests assert the lifecycle
// rules are codified and applied on creation.
type BucketProvisioner interface {
	Provision(ctx context.Context) error
	Lifecycle() Lifecycle
}

// Lifecycle is the bucket lifecycle policy: Standard -> Glacier after
// GlacierTransitionDays, then Glacier -> Deep Archive after
// DeepArchiveTransitionDays. It is exposed so tests can assert the rules are
// codified independently of the S3 API being live.
type Lifecycle struct {
	GlacierTransitionDays    int
	DeepArchiveTransitionDays int
}

// Config holds the bucket provisioning parameters.
type Config struct {
	Bucket                   string
	StorageClass             string
	Retention                time.Duration
	GlacierTransitionDays    int
	DeepArchiveTransitionDays int
}

// Validate returns an error if the config is missing required fields or has
// nonsensical transition days.
func (c Config) Validate() error {
	if c.Bucket == "" {
		return errors.New("payload: bucket name is required")
	}
	if c.Retention <= 0 {
		return errors.New("payload: retention must be positive")
	}
	if c.GlacierTransitionDays <= 0 {
		return errors.New("payload: glacier transition days must be positive")
	}
	if c.DeepArchiveTransitionDays <= c.GlacierTransitionDays {
		return errors.New("payload: deep archive transition must be after glacier transition")
	}
	return nil
}

// String renders the lifecycle for logs and test diffs.
func (l Lifecycle) String() string {
	return fmt.Sprintf("Standard->Glacier(%dd)->DeepArchive(%dd)",
		l.GlacierTransitionDays, l.DeepArchiveTransitionDays)
}