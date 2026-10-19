package middleware

import (
	"net/http"
	"strings"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
)

const (
	passwordGuardExemptPrefix = "/api/v1/auth/"

	passwordChangeRequiredMessage = "请先修改初始密码"
)

func PasswordGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if ok && identity.MustChangePassword && !isPasswordGuardExempt(r.URL.Path) {
			httpx.Fail(r.Context(), w, httpx.Forbidden(passwordChangeRequiredMessage))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isPasswordGuardExempt(path string) bool {
	return strings.HasPrefix(path, passwordGuardExemptPrefix)
}
