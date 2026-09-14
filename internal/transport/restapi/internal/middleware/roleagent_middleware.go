// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package middleware

import (
	"net/http"

	"eino-quickstart/internal/platform/auth"
)

// RoleAgentMiddleware guards the routes in the `agent` group of restapi.api.
// It delegates to RequireRoles; the only job of this type is to match the
// `serverCtx.RoleAgent` shape goctl's generated routes.go expects.
type RoleAgentMiddleware struct {
}

func NewRoleAgentMiddleware() *RoleAgentMiddleware {
	return &RoleAgentMiddleware{}
}

func (m *RoleAgentMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return RequireRoles(auth.RoleAgent)(next)
}
