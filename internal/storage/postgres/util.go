package postgres

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// randSuffix returns 8 hex chars suitable for unique test database names.
func randSuffix() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// replaceDBName swaps the path component of a Postgres DSN for the given db
// name. It handles both URL form (postgres://u:p@h:port/db?...) and keyword
// form (host=h dbname=x ...). For the URL form it replaces the first path
// segment; for keyword form it replaces/inserts dbname=.
func replaceDBName(dsn, name string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		// Split off query.
		path := dsn
		q := ""
		if i := strings.Index(dsn, "?"); i >= 0 {
			path = dsn[:i]
			q = dsn[i:]
		}
		// Find start of path after host.
		idx := strings.Index(path, "://")
		rest := path[idx+3:]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return path + "/" + name + q
		}
		return path[:idx+3] + rest[:slash+1] + name + q
	}
	parts := strings.Fields(dsn)
	found := false
	for i, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			parts[i] = "dbname=" + name
			found = true
			break
		}
	}
	if !found {
		parts = append(parts, "dbname="+name)
	}
	return strings.Join(parts, " ")
}