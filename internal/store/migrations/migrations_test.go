package migrations

import (
	"context"
	"sync"
	"testing"
)

func TestAllReturnsInit(t *testing.T) {
	migs, err := All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(migs) != 1 {
		t.Fatalf("expected 1 migration, got %d", len(migs))
	}
	if migs[0].Version != "0001_init" {
		t.Errorf("version: %q", migs[0].Version)
	}
	if migs[0].Up == "" || migs[0].Down == "" {
		t.Error("up/down missing")
	}
}

// fakeDB is an in-memory runner that records applied versions and counts
// Exec calls.
type fakeDB struct {
	mu       sync.Mutex
	applied  map[string]bool
	execs   []string
}

func newFakeDB() *fakeDB {
	return &fakeDB{applied: map[string]bool{}}
}

func (d *fakeDB) exec(_ context.Context, q string, args ...any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.execs = append(d.execs, q)
	return nil
}

// After running Up, the fakeDB won't have set applied=true via the runner
// (the runner inserts into schema_migrations via exec, not via the
// in-memory map). We override query so the runner sees recorded versions.
// We do this by tracking applied versions from exec() in a wrapper.

func TestRunnerUpAppliesMigrations(t *testing.T) {
	db := newFakeDB()
	// Wrap exec to record applied versions when INSERT into schema_migrations
	// is executed. We can't parse args generically, so we record by query
	// prefix.
	applied := make(map[string]bool)
	var mu sync.Mutex
	exec := func(ctx context.Context, q string, args ...any) error {
		if err := db.exec(ctx, q, args...); err != nil {
			return err
		}
		if len(args) >= 1 {
			if v, ok := args[0].(string); ok {
				mu.Lock()
				applied[v] = true
				mu.Unlock()
			}
		}
		return nil
	}
	query := func(ctx context.Context, v string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		return applied[v], nil
	}
	r := NewRunner(exec, query)
	if err := r.Up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if !applied["0001_init"] {
		t.Error("0001_init not marked applied")
	}

	// Re-running Up should not re-apply the migration (the CREATE TABLE
	// for schema_migrations is always re-executed, so we count only the
	// migration Up statements, identified by their leading comment).
	countMigUps := func() int {
		n := 0
		for _, q := range db.execs {
			if len(q) > 0 && q[0] == '-' {
				n++
			}
		}
		return n
	}
	before := countMigUps()
	_ = r.Up(context.Background())
	if got := countMigUps(); got != before {
		t.Errorf("re-running Up re-applied migrations: %d -> %d", before, got)
	}
}

func TestRunnerDownReverts(t *testing.T) {
	applied := make(map[string]bool)
	var mu sync.Mutex
	exec := func(ctx context.Context, q string, args ...any) error {
		if len(args) >= 1 {
			if v, ok := args[0].(string); ok {
				mu.Lock()
				if q == "DELETE FROM schema_migrations WHERE version=$1" {
					delete(applied, v)
				} else if q == "INSERT INTO schema_migrations(version, applied_at) VALUES ($1, $2) ON CONFLICT (version) DO NOTHING" {
					applied[v] = true
				}
				mu.Unlock()
			}
		}
		return nil
	}
	query := func(ctx context.Context, v string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		return applied[v], nil
	}
	r := NewRunner(exec, query)
	if err := r.Up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := r.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
	if applied["0001_init"] {
		t.Error("0001_init should be reverted")
	}
}