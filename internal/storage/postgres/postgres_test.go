package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// freshDB creates a dedicated database for the test, returns a pool connected
// to it, and drops it on cleanup. The maintenance DSN is taken from DB_URL
// (matching the sibling aml-kyt-screening repo) or AUDIT_TEST_DB_URL and must
// have CREATEDB privilege. If neither is set, the test is skipped — these are
// integration tests that need a real Postgres cluster.
func freshDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DB_URL")
	if dsn == "" {
		dsn = os.Getenv("AUDIT_TEST_DB_URL")
	}
	if dsn == "" {
		t.Skip("DB_URL/AUDIT_TEST_DB_URL not set; skipping Postgres integration test")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	maint, err := pgx.ConnectConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect maintenance: %v", err)
	}
	t.Cleanup(func() { _ = maint.Close(context.Background()) })

	dbName := "audit_test_" + randSuffix()
	if _, err := maint.Exec(context.Background(), "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create db %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		_, _ = maint.Exec(context.Background(), "DROP DATABASE "+dbName)
	})

	cfg2, err := pgxpool.ParseConfig(replaceDBName(dsn, dbName))
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg2)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRunCreatesExpectedColumnsAndIndexes(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	if err := Run(ctx, pool); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := VerifySchema(ctx, pool); err != nil {
		t.Fatalf("VerifySchema: %v", err)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	if err := Run(ctx, pool); err != nil {
		t.Fatalf("Run first: %v", err)
	}
	if err := Run(ctx, pool); err != nil {
		t.Fatalf("Run second: %v", err)
	}
	if err := VerifySchema(ctx, pool); err != nil {
		t.Fatalf("VerifySchema after second run: %v", err)
	}
}

func TestVerifySchemaReportsMissing(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	if err := VerifySchema(ctx, pool); err == nil {
		t.Fatalf("expected VerifySchema to fail on fresh DB with no audit_events table")
	}
}

func TestRunNilDB(t *testing.T) {
	if err := Run(context.Background(), nil); err == nil {
		t.Fatalf("expected error for nil db")
	}
}

func TestRunNilContext(t *testing.T) {
	if err := Run(nil, nopDB{}); err != nil {
		t.Fatalf("Run with nil ctx returned err: %v", err)
	}
}

func TestLoadMigrations(t *testing.T) {
	migrations, err := LoadMigrations()
	if err != nil {
		t.Fatalf("LoadMigrations: %v", err)
	}
	if len(migrations) != 1 {
		t.Fatalf("expected 1 migration pair, got %d", len(migrations))
	}
	m := migrations[0]
	if m.Version != 1 {
		t.Errorf("migration version = %d want 1", m.Version)
	}
	if m.Up == "" {
		t.Errorf("migration %d: missing Up script", m.Version)
	}
	if m.Down == "" {
		t.Errorf("migration %d: missing Down script", m.Version)
	}
	for _, want := range []string{
		"audit_events",
		"idx_audit_events_ts_id",
		"idx_audit_events_source_ts",
		"idx_audit_events_actor_ts",
		"idx_audit_events_action_ts",
		"idx_audit_events_target_ts",
	} {
		if !strings.Contains(m.Up, want) {
			t.Errorf("migration Up missing %q", want)
		}
	}
}

func TestReplaceDBName(t *testing.T) {
	cases := []struct {
		dsn, name, want string
	}{
		{"postgres://u:p@localhost:5432/db?sslmode=disable", "x", "postgres://u:p@localhost:5432/x?sslmode=disable"},
		{"postgres://u:p@localhost:5432/db", "x", "postgres://u:p@localhost:5432/x"},
		{"postgres://u:p@localhost:5432/", "x", "postgres://u:p@localhost:5432/x"},
		{"host=localhost user=u dbname=old", "x", "host=localhost user=u dbname=x"},
		{"host=localhost user=u", "x", "host=localhost user=u dbname=x"},
	}
	for _, c := range cases {
		if got := replaceDBName(c.dsn, c.name); got != c.want {
			t.Errorf("replaceDBName(%q,%q) = %q want %q", c.dsn, c.name, got, c.want)
		}
	}
}

type nopDB struct{}

func (nopDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (nopDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}
func (nopDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return errRow{err: pgx.ErrNoRows}
}
func (nopDB) Begin(context.Context) (pgx.Tx, error) { return nopTx{}, nil }

// nopTx is a fake pgx.Tx for tests that exercise the migration runner without
// a live database. The unimplemented methods panic because the migration runner
// only uses Exec/Commit/Rollback.
type nopTx struct{}

func (nopTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (nopTx) Commit(context.Context) error   { return nil }
func (nopTx) Rollback(context.Context) error { return nil }
func (nopTx) Begin(context.Context) (pgx.Tx, error) {
	panic("nested begin not supported in nopTx")
}
func (nopTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("CopyFrom not supported in nopTx")
}
func (nopTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("SendBatch not supported in nopTx")
}
func (nopTx) LargeObjects() pgx.LargeObjects { panic("LargeObjects not supported in nopTx") }
func (nopTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("Prepare not supported in nopTx")
}
func (nopTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("Query not supported in nopTx")
}
func (nopTx) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("QueryRow not supported in nopTx")
}
func (nopTx) Conn() *pgx.Conn { panic("Conn not supported in nopTx") }

// errRow is a pgx.Row stub that always returns the wrapped error from Scan.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }