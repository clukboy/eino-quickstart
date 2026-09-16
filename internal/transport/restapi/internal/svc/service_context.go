// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"log/slog"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/platform/queue"
	"eino-quickstart/internal/transport/restapi/internal/config"
	"eino-quickstart/internal/transport/restapi/internal/middleware"

	"github.com/zeromicro/go-zero/rest"
)

type Deps struct {
	Agent     *agent.Harness
	Sessions  *session.Store
	Approvals *approval.Store
	Runs      *run.Store
	Turns     *turn.Store
	Auth      *auth.Authenticator
	Logger    *slog.Logger
	EntClient *ent.Client
	Queue     queue.Producer
}

// ServiceContext is what goctl passes to every handler and logic. goctl owns the
// Config/RoleAdmin/RoleAgent/RoleApprover fields; the business dependencies
// above were added by hand.
type ServiceContext struct {
	Config       config.Config
	Agent        *agent.Harness
	Sessions     *session.Store
	Approvals    *approval.Store
	Runs         *run.Store
	Turns        *turn.Store
	Logger       *slog.Logger
	EntClient    *ent.Client
	Queue        queue.Producer
	RoleAdmin    rest.Middleware
	RoleAgent    rest.Middleware
	RoleApprover rest.Middleware
}

// NewServiceContext wires the dependency set into a ServiceContext.
func NewServiceContext(c config.Config, deps Deps) *ServiceContext {
	return &ServiceContext{
		Config:       c,
		Agent:        deps.Agent,
		Sessions:     deps.Sessions,
		Approvals:    deps.Approvals,
		Runs:         deps.Runs,
		Turns:        deps.Turns,
		Logger:       deps.Logger,
		EntClient:    deps.EntClient,
		Queue:        deps.Queue,
		RoleAdmin:    middleware.NewRoleAdminMiddleware().Handle,
		RoleAgent:    middleware.NewRoleAgentMiddleware().Handle,
		RoleApprover: middleware.NewRoleApproverMiddleware().Handle,
	}
}
