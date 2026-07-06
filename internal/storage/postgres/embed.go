// Package postgres embeds the SQL migrations applied by Run. The migrations
// directory contains versioned up/down pairs (e.g. 0001_audit_events.up.sql)
// consumed by the migration runner in postgres.go.
package postgres

import "embed"

// migrationFS holds the embedded *.sql migration files. The directive must
// live in a .go file in the same package as the embed.
//
//go:embed migrations/*.sql
var migrationFS embed.FS