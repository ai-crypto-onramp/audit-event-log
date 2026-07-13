// Package s3 defines the object-storage surface for audit payloads and
// export artifacts. The in-memory Fake in this package is used by unit
// tests; the real S3 adapter lives in the s3adapter subpackage.
package s3

import (
	"context"
	"io"
	"time"
)

// Object describes a stored object's metadata.
type Object struct {
	Key          string
	Bucket       string
	Size         int64
	StorageClass string
	RetentionDays int
	LegalHold    bool
	CreatedAt    time.Time
}

// PutOptions controls how an object is written.
type PutOptions struct {
	// Key is the S3 object key.
	Key string
	// ContentType is the MIME type (default application/octet-stream).
	ContentType string
	// StorageClass is the S3 storage class (e.g. "STANDARD", "GLACIER").
	// Empty defaults to "STANDARD".
	StorageClass string
	// RetentionDays applies an Object Lock retention period. 0 disables
	// retention enforcement for this object.
	RetentionDays int
	// LegalHold, if true, places the object under legal hold.
	LegalHold bool
}

// Client is the object-storage surface used by ingest and export.
type Client interface {
	// Put writes an object with the given options. The object is
	// immutable for max(RetentionDays, LegalHold).
	Put(ctx context.Context, bucket string, opts PutOptions, body io.Reader) (string, error)
	// Get downloads an object's bytes. Returns an error matching
	// os.IsNotExist on a missing key.
	Get(ctx context.Context, bucket, key string) ([]byte, error)
	// PresignGet returns a time-limited download URL for an object.
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
	// Head returns object metadata.
	Head(ctx context.Context, bucket, key string) (*Object, error)
	// Delete attempts to delete an object. Returns ErrRetentionActive if
	// the object is under retention or legal hold.
	Delete(ctx context.Context, bucket, key string) error
}

// ErrRetentionActive is returned by Delete when the object is under
// retention or legal hold.
type ErrRetentionActive struct {
	Key string
}

func (e *ErrRetentionActive) Error() string { return "s3store: object under retention: " + e.Key }

// ErrNotFound is returned by Get/Head for a missing object.
type ErrNotFound struct{ Key string }

func (e *ErrNotFound) Error() string { return "s3store: not found: " + e.Key }

// Fake is an in-memory Client for tests. It enforces retention / legal-hold
// immutability the same way real S3 Object Lock would: Delete on a retained
// object returns ErrRetentionActive, and overwriting a retained object
// (Put with the same key while retention is active) returns ErrRetentionActive.
type Fake struct {
	objects map[string]*fakeObject
}

type fakeObject struct {
	bucket     string
	key        string
	body       []byte
	storageClass string
	retentionDays int
	legalHold  bool
	createdAt  time.Time
}

// NewFake returns a fresh in-memory Client.
func NewFake() *Fake { return &Fake{objects: map[string]*fakeObject{}} }

// Put writes an object, enforcing retention on overwrite.
func (f *Fake) Put(_ context.Context, bucket string, opts PutOptions, body io.Reader) (string, error) {
	b, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	key := opts.Key
	if _, ok := f.objects[bucket+"/"+key]; ok {
		o := f.objects[bucket+"/"+key]
		// Retention enforcement: cannot overwrite while retention active.
		if isRetained(o) {
			return "", &ErrRetentionActive{Key: key}
		}
	}
	if opts.StorageClass == "" {
		opts.StorageClass = "STANDARD"
	}
	f.objects[bucket+"/"+key] = &fakeObject{
		bucket:       bucket,
		key:          key,
		body:         b,
		storageClass: opts.StorageClass,
		retentionDays: opts.RetentionDays,
		legalHold:    opts.LegalHold,
		createdAt:    time.Now().UTC(),
	}
	return key, nil
}

// Get downloads an object's bytes.
func (f *Fake) Get(_ context.Context, bucket, key string) ([]byte, error) {
	o, ok := f.objects[bucket+"/"+key]
	if !ok {
		return nil, &ErrNotFound{Key: key}
	}
	out := make([]byte, len(o.body))
	copy(out, o.body)
	return out, nil
}

// PresignGet returns a synthetic presigned URL for tests.
func (f *Fake) PresignGet(_ context.Context, bucket, key string, ttl time.Duration) (string, error) {
	if _, ok := f.objects[bucket+"/"+key]; !ok {
		return "", &ErrNotFound{Key: key}
	}
	return "https://" + bucket + ".s3.example.com/" + key + "?ttl=" + ttl.String(), nil
}

// Head returns object metadata.
func (f *Fake) Head(_ context.Context, bucket, key string) (*Object, error) {
	o, ok := f.objects[bucket+"/"+key]
	if !ok {
		return nil, &ErrNotFound{Key: key}
	}
	return &Object{
		Key:           o.key,
		Bucket:        o.bucket,
		Size:          int64(len(o.body)),
		StorageClass:  o.storageClass,
		RetentionDays: o.retentionDays,
		LegalHold:     o.legalHold,
		CreatedAt:     o.createdAt,
	}, nil
}

// Delete attempts to delete an object.
func (f *Fake) Delete(_ context.Context, bucket, key string) error {
	o, ok := f.objects[bucket+"/"+key]
	if !ok {
		return &ErrNotFound{Key: key}
	}
	if isRetained(o) {
		return &ErrRetentionActive{Key: key}
	}
	delete(f.objects, bucket+"/"+key)
	return nil
}

func isRetained(o *fakeObject) bool {
	if o.legalHold {
		return true
	}
	if o.retentionDays <= 0 {
		return false
	}
	expiry := o.createdAt.Add(time.Duration(o.retentionDays) * 24 * time.Hour)
	return time.Now().UTC().Before(expiry)
}

// ApplyTransition simulates the S3 lifecycle policy by transitioning all
// objects older than ageDays from fromClass to toClass. Returns the number
// of objects transitioned. Used by Stage 9 acceptance tests.
func (f *Fake) ApplyTransition(ageDays int, fromClass, toClass string) int {
	n := 0
	cutoff := time.Now().UTC().Add(-time.Duration(ageDays) * 24 * time.Hour)
	for _, o := range f.objects {
		if o.storageClass != fromClass {
			continue
		}
		if o.createdAt.After(cutoff) {
			continue
		}
		o.storageClass = toClass
		n++
	}
	return n
}