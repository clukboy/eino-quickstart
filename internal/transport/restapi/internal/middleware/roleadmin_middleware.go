// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package middleware

import (
	"net/http"

	"eino-quickstart/internal/platform/auth"
)

// RoleAdminMiddleware guards the routes in the `admin` group of restapi.api.
type RoleAdminMiddleware struct {
}

func NewRoleAdminMiddleware() *RoleAdminMiddleware {
	return &RoleAdminMiddleware{}
}

func (m *RoleAdminMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return RequireRoles(auth.RoleAdmin)(next)
}
