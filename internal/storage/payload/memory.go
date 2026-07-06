package payload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// MemoryStore is an in-memory append-only payload store that emulates S3
// Object Lock in compliance mode. Once an object is written with a retention
// duration, Put for the same key returns ErrObjectLocked until the retention
// window elapses. There is no Delete method on the public surface; the
// unexported deleteForTest helper exists solely so tests can assert that even
// an internal caller is blocked during retention.
type MemoryStore struct {
	cfg         Config
	lifecycle   Lifecycle
	mu          sync.Mutex
	objects     map[string]*memObject
	provisioned bool
	clock       virtualClock
}

type memObject struct {
	data        []byte
	storageClass string
	retainedUntil time.Time
}

// NewMemoryStore constructs a MemoryStore from the given config. It does not
// provision; call Provision to mark the bucket as created (which records the
// lifecycle policy and makes Put legal).
func NewMemoryStore(cfg Config) (*MemoryStore, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &MemoryStore{
		cfg:       cfg,
		lifecycle: Lifecycle{cfg.GlacierTransitionDays, cfg.DeepArchiveTransitionDays},
		objects:   make(map[string]*memObject),
		clock:     virtualClock{t: time.Now()},
	}, nil
}

// Provision marks the bucket as created and records the lifecycle policy. It
// is idempotent.
func (m *MemoryStore) Provision(ctx context.Context) error {
	if ctx == nil {
		return errors.New("payload: nil context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.provisioned = true
	return nil
}

// Lifecycle returns the lifecycle policy applied at Provision time.
func (m *MemoryStore) Lifecycle() Lifecycle {
	return m.lifecycle
}

// Put writes a new object. If the key already exists and its retention window
// has not elapsed, Put returns ErrObjectLocked. After retention elapses the
// overwrite is still rejected to model compliance-mode WORM (no overwrite ever,
// even after retention — only a delete would be allowed post-retention, and
// Delete is not exposed). For tests that need to simulate time passing, call
// AdvanceTime.
func (m *MemoryStore) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	if ctx == nil {
		return errors.New("payload: nil context")
	}
	if key == "" {
		return errors.New("payload: empty key")
	}
	if err := m.requireProvisioned(); err != nil {
		return err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("payload: read body: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.objects[key]; ok {
		if !m.now().After(existing.retainedUntil) {
			return ErrObjectLocked
		}
		// Compliance mode: never overwrite, even after retention. A future
		// governance-mode variant could allow overwrite post-retention; we are
		// explicitly modeling compliance mode per the README.
		return ErrObjectLocked
	}
	retention := opts.Retention
	if retention <= 0 {
		retention = m.cfg.Retention
	}
	sc := opts.StorageClass
	if sc == "" {
		sc = m.cfg.StorageClass
	}
	m.objects[key] = &memObject{
		data:         data,
		storageClass: sc,
		retainedUntil: m.now().Add(retention),
	}
	return nil
}

// Get streams a stored object back. Returns an error if the key does not exist.
func (m *MemoryStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if ctx == nil {
		return nil, errors.New("payload: nil context")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("payload: object %q not found", key)
	}
	return io.NopCloser(bytes.NewReader(obj.data)), nil
}

// requireProvisioned returns an error if Provision has not been called.
func (m *MemoryStore) requireProvisioned() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.provisioned {
		return errors.New("payload: bucket not provisioned; call Provision first")
	}
	return nil
}

// now is overridable for tests via AdvanceTime.
func (m *MemoryStore) now() time.Time {
	return m.clock.Load()
}

// clock holds the current virtual time. We use atomic-ish semantics through a
// mutex-guarded field; for simplicity we store it under the same mu. To keep
// now() lock-free we use a dedicated small struct.
//
// Implementation note: we keep the clock on a separate mutex to avoid
// re-entrancy with mu in now().
type virtualClock struct {
	mu  sync.Mutex
	t   time.Time
}

func (vc *virtualClock) Load() time.Time {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return vc.t
}

func (vc *virtualClock) Store(t time.Time) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.t = t
}

// AdvanceTime moves the store's virtual clock forward by d. Tests use this to
// simulate the retention window elapsing without sleeping. Production callers
// never need it.
func (m *MemoryStore) AdvanceTime(d time.Duration) {
	m.clock.Store(m.clock.Load().Add(d))
}