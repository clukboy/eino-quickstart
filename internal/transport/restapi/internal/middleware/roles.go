package middleware

import (
	"net/http"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
)

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
