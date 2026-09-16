// Package middleware holds the go-zero middleware for the restapi transport.
//
// The global chain is go-zero's own: rest assembles TraceHandler, LogHandler,
// PrometheusHandler, RecoverHandler and the resilience handlers per route from
// MiddlewaresConf (see etc/restapi.yaml), and restapi.go adds only the two
// things go-zero does not provide — TraceID and the Bearer API key check.
//
// What lives here is those two global middlewares plus the route-scoped role
// checks that the .api DSL references via `middleware: RoleAgent` and friends.
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
