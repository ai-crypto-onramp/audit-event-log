// Package postgres implements the searchable PostgreSQL index for audit events:
// the schema migration, the idempotent bootstrap runner, and schema
// introspection helpers used by the test suite to verify that the expected
// columns and indexes exist on a fresh database.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Migration represents a single versioned SQL migration pair loaded from the
// embedded migrations directory. New migrations are appended; existing
// entries must never be edited once shipped (append-only schema evolution).
type Migration struct {
	Version int
	Up      string
	Down    string
}

// LoadMigrations reads the embedded migration files and returns them sorted
// by version. Each version must have an up script; down scripts are optional
// but recommended.
func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	byVersion := make(map[int]*Migration)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		var version int
		var kind string
		if _, err := fmt.Sscanf(name, "%d_%s", &version, &kind); err != nil {
			return nil, fmt.Errorf("parse migration name %s: %w", name, err)
		}
		m, ok := byVersion[version]
		if !ok {
			m = &Migration{Version: version}
			byVersion[version] = m
		}
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			m.Up = string(body)
		case strings.HasSuffix(name, ".down.sql"):
			m.Down = string(body)
		}
	}
	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.Up == "" {
			return nil, fmt.Errorf("migration %d missing up script", m.Version)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// Migrations returns the loaded, ordered migration set. It is a convenience
// wrapper around LoadMigrations for callers that only need the slice.
func Migrations() ([]Migration, error) {
	return LoadMigrations()
}

// Run applies all pending up-migrations to db. It is idempotent: an internal
// schema_migrations table tracks applied versions, so running on every boot
// is safe. Each migration is applied in its own transaction. A nil ctx is
// treated as context.Background().
func Run(ctx context.Context, db DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return errors.New("postgres.Run: nil db")
	}
	if _, err := db.Exec(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	migrations, err := LoadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migrations {
		applied, err := migrationApplied(ctx, db, m.Version)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.Version, err)
		}
		if applied {
			continue
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for migration %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(ctx, m.Up); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %d up: %w", m.Version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.Version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}
	return nil
}

// migrationApplied reports whether version is recorded in schema_migrations.
func migrationApplied(ctx context.Context, db DB, version int) (bool, error) {
	var exists int
	err := db.QueryRow(ctx, `SELECT 1 FROM schema_migrations WHERE version = $1`, version).Scan(&exists)
	switch err {
	case nil:
		return true, nil
	case pgx.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}

// DB is the subset of *pgx.Conn / *pgxpool.Pool used by this package. It is
// satisfied by pgxpool.Pool and pgx.Conn so callers can pass whichever they
// have, and tests can substitute a fake.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Column describes a column row from information_schema.columns.
type Column struct {
	Name     string
	DataType string
	IsNULL   string
}

// Columns returns the columns of audit_events in ordinal order.
func Columns(ctx context.Context, db DB) ([]Column, error) {
	rows, err := db.Query(ctx, `
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_name = 'audit_events'
		ORDER BY ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.DataType, &c.IsNULL); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Index describes an index row from pg_indexes.
type Index struct {
	Name   string
	Table  string
	Def    string
}

// Indexes returns all indexes on audit_events.
func Indexes(ctx context.Context, db DB) ([]Index, error) {
	rows, err := db.Query(ctx, `
		SELECT indexname, tablename, indexdef
		FROM pg_indexes
		WHERE tablename = 'audit_events'
		ORDER BY indexname`)
	if err != nil {
		return nil, fmt.Errorf("query indexes: %w", err)
	}
	defer rows.Close()
	var out []Index
	for rows.Next() {
		var idx Index
		if err := rows.Scan(&idx.Name, &idx.Table, &idx.Def); err != nil {
			return nil, err
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

// ExpectColumns is the canonical list of audit_events columns the README data
// model requires, in declaration order. Schema tests assert that every name
// appears (set semantics) and that the row count matches.
var ExpectColumns = []string{
	"id", "ts", "source_service", "actor_id", "action", "target_type",
	"target_id", "payload_hash", "payload_ref", "prev_hash", "this_hash",
	"anchored", "legal_hold", "redacted",
}

// ExpectIndexes is the canonical set of index names the migration creates.
var ExpectIndexes = []string{
	"audit_events_pkey",
	"idx_audit_events_ts_id",
	"idx_audit_events_source_ts",
	"idx_audit_events_actor_ts",
	"idx_audit_events_action_ts",
	"idx_audit_events_target_ts",
}

// VerifySchema fetches columns and indexes and reports the first missing
// expected entry, or nil if everything is present. The returned human-readable
// diffs are joined into a single error for convenience in test output.
func VerifySchema(ctx context.Context, db DB) error {
	cols, err := Columns(ctx, db)
	if err != nil {
		return err
	}
	have := make(map[string]Column, len(cols))
	for _, c := range cols {
		have[c.Name] = c
	}
	var missing []string
	for _, name := range ExpectColumns {
		if _, ok := have[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing columns: %s", strings.Join(missing, ", "))
	}

	idxs, err := Indexes(ctx, db)
	if err != nil {
		return err
	}
	haveIdx := make(map[string]Index, len(idxs))
	for _, i := range idxs {
		haveIdx[i.Name] = i
	}
	var missingIdx []string
	for _, name := range ExpectIndexes {
		if _, ok := haveIdx[name]; !ok {
			missingIdx = append(missingIdx, name)
		}
	}
	if len(missingIdx) > 0 {
		return fmt.Errorf("missing indexes: %s", strings.Join(missingIdx, ", "))
	}
	return nil
}