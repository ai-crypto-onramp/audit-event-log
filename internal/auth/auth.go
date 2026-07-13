// Package auth implements the authentication / authorization middleware for
// the REST API. Roles are declared as a comma-separated list in the
// X-Audit-Roles header (set by the upstream API gateway after token
// validation); this service trusts the gateway's verdict and only enforces
// role-based access control.
//
// Two roles are recognized:
//   - audit-reader: required for read endpoints (GET /v1/events*).
//   - audit-admin: required for admin endpoints (POST /v1/admin/*,
//     POST /v1/exports). audit-admin implies audit-reader.
package auth

import (
	"net/http"
	"strings"
)

// Role is a principal role.
type Role string

const (
	RoleReader Role = "audit-reader"
	RoleAdmin  Role = "audit-admin"
	RoleSystem Role = "audit-system" // internal service accounts, e.g. Kafka ingest
)

// RolesHeader is the header carrying the principal's roles.
const RolesHeader = "X-Audit-Roles"

// Require returns middleware that allows the request only if the caller
// holds any of the required roles. Roles are read from the RolesHeader.
// The header value is a comma-separated list, e.g. "audit-reader,foo".
// audit-admin implies audit-reader.
func Require(required ...Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if HasAny(r, required...) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}

// HasAny reports whether the request carries any of the required roles.
// audit-admin implies audit-reader.
func HasAny(r *http.Request, required ...Role) bool {
	granted := Roles(r)
	for _, want := range required {
		if granted.Has(want) {
			return true
		}
		if want == RoleReader && granted.Has(RoleAdmin) {
			return true
		}
	}
	return false
}

// Roles parses the RolesHeader into a set.
func Roles(r *http.Request) RoleSet {
	parts := strings.Split(r.Header.Get(RolesHeader), ",")
	out := make(RoleSet, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out[Role(p)] = true
		}
	}
	return out
}

// RoleSet is a set of roles.
type RoleSet map[Role]bool

// Has reports whether the set contains role.
func (s RoleSet) Has(role Role) bool { return s[role] }