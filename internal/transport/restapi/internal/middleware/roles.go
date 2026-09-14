// Package middleware holds the go-zero middleware for the restapi transport.
//
// The global chain (trace, request id, recover, access log, metrics, authenticate)
// is installed in restapi.go straight from internal/platform/observability and
// internal/platform/auth, because those are already net/http native — only the
// signature needs adapting. What lives here is the route-scoped role checks that
// the .api DSL references via `middleware: RoleAgent` and friends.
package middleware

import (
	"net/http"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
)

// RequireRoles allows the request only if the authenticated identity holds one
// of roles. Admin is always allowed.
//
// It assumes the global Authenticate middleware already resolved the identity
// onto the request context. go-zero runs server.Use(...) middleware outside the
// route handlers, so by the time this runs the identity is always present when
// a token was supplied.
func RequireRoles(roles ...auth.Role) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			identity, ok := auth.IdentityFromContext(r.Context())
			if !ok {
				httpx.Fail(r.Context(), w, httpx.Unauthorized("unauthorized"))
				return
			}
			if !hasRole(identity.Role, roles) {
				httpx.Fail(r.Context(), w, httpx.Forbidden("forbidden"))
				return
			}
			next(w, r)
		}
	}
}

func hasRole(role auth.Role, allowed []auth.Role) bool {
	if role == auth.RoleAdmin {
		return true
	}
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	return false
}
