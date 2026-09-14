// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package middleware

import (
	"net/http"

	"eino-quickstart/internal/platform/auth"
)

// RoleApproverMiddleware guards the routes in the `approver` group of
// restapi.api.
type RoleApproverMiddleware struct {
}

func NewRoleApproverMiddleware() *RoleApproverMiddleware {
	return &RoleApproverMiddleware{}
}

func (m *RoleApproverMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return RequireRoles(auth.RoleApprover)(next)
}
