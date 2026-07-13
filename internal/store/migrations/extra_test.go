package migrations

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestRunnerUpCreateTableError(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.Contains(q, "schema_migrations") {
			return errors.New("create boom")
		}
		return nil
	}
	r := NewRunner(exec, nil)
	if err := r.Up(context.Background()); err == nil {
		t.Fatal("expected create table error")
	}
}

func TestRunnerUpApplyError(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "--") {
			return errors.New("apply boom")
		}
		return nil
	}
	r := NewRunner(exec, nil)
	if err := r.Up(context.Background()); err == nil {
		t.Fatal("expected apply error")
	}
}

func TestRunnerUpRecordError(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "INSERT INTO schema_migrations") {
			return errors.New("record boom")
		}
		return nil
	}
	r := NewRunner(exec, nil)
	if err := r.Up(context.Background()); err == nil {
		t.Fatal("expected record error")
	}
}

func TestRunnerUpQueryError(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error { return nil }
	query := func(ctx context.Context, v string) (bool, error) {
		return false, errors.New("query boom")
	}
	r := NewRunner(exec, query)
	if err := r.Up(context.Background()); err == nil {
		t.Fatal("expected query error")
	}
}

func TestRunnerDownApplyError(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "DROP TABLE") {
			return errors.New("down boom")
		}
		return nil
	}
	query := func(ctx context.Context, v string) (bool, error) { return true, nil }
	r := NewRunner(exec, query)
	if err := r.Down(context.Background()); err == nil {
		t.Fatal("expected down apply error")
	}
}

func TestRunnerDownDeleteError(t *testing.T) {
	applied := true
	var mu sync.Mutex
	exec := func(ctx context.Context, q string, args ...any) error {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(q, "DROP TABLE") {
			return nil // down SQL succeeds
		}
		if strings.HasPrefix(q, "DELETE FROM schema_migrations") {
			return errors.New("delete boom")
		}
		return nil
	}
	query := func(ctx context.Context, v string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		return applied, nil
	}
	r := NewRunner(exec, query)
	if err := r.Down(context.Background()); err == nil {
		t.Fatal("expected delete error")
	}
}

func TestRunnerDownQueryError(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error { return nil }
	query := func(ctx context.Context, v string) (bool, error) {
		return false, errors.New("query boom")
	}
	r := NewRunner(exec, query)
	if err := r.Down(context.Background()); err == nil {
		t.Fatal("expected query error")
	}
}

func TestRunnerDownSkipsNotApplied(t *testing.T) {
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "DROP TABLE") {
			t.Errorf("should not apply down for unapplied migration: %q", q)
		}
		return nil
	}
	query := func(ctx context.Context, v string) (bool, error) { return false, nil }
	r := NewRunner(exec, query)
	if err := r.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
}

func TestRunnerUpSkipsApplied(t *testing.T) {
	calls := 0
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "--") {
			calls++
		}
		return nil
	}
	query := func(ctx context.Context, v string) (bool, error) { return true, nil }
	r := NewRunner(exec, query)
	if err := r.Up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if calls != 0 {
		t.Errorf("should skip applied migrations, got %d up calls", calls)
	}
}

func TestRunnerUpNilQueryAlwaysApplies(t *testing.T) {
	calls := 0
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "--") {
			calls++
		}
		return nil
	}
	r := NewRunner(exec, nil)
	if err := r.Up(context.Background()); err != nil {
		t.Fatalf("up: %v", err)
	}
	if calls == 0 {
		t.Error("nil query should force apply")
	}
}

func TestRunnerDownNilQueryAlwaysReverts(t *testing.T) {
	calls := 0
	exec := func(ctx context.Context, q string, args ...any) error {
		if strings.HasPrefix(q, "DROP TABLE") {
			calls++
		}
		return nil
	}
	r := NewRunner(exec, nil)
	if err := r.Down(context.Background()); err != nil {
		t.Fatalf("down: %v", err)
	}
	if calls == 0 {
		t.Error("nil query should force revert")
	}
}
