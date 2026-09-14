package middleware

import (
	"net/http"
	"strings"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
)

// Authenticate resolves the Bearer token into an identity on the request
// context, using restapi's error envelope for rejections.
//
// internal/platform/auth.Authenticator.Authenticate does the same job, but it
// answers a rejected token with a bare {"error":"unauthorized"} that carries no
// stable code. This transport's contract (see internal/httpx) always ships
// code/error/request_id, so the token check is re-done here against the
// exported IdentityFor/WithIdentity pair — exactly what
// internal/transport/hertzapi/middleware does.
//
// A request with no Authorization header passes through untouched: /health and
// /ready are public, and the route-level role middlewares reject the rest.
func Authenticate(authenticator *auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				next.ServeHTTP(w, r)
				return
			}

			secret, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || strings.TrimSpace(secret) == "" {
				httpx.Fail(r.Context(), w, httpx.InvalidCredentials("unauthorized"))
				return
			}

			identity, ok := authenticator.IdentityFor(strings.TrimSpace(secret))
			if !ok {
				httpx.Fail(r.Context(), w, httpx.InvalidCredentials("unauthorized"))
				return
			}

			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
		})
	}
}
