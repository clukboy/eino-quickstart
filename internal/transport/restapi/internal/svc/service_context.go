// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"log/slog"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/transport/restapi/internal/config"
	"eino-quickstart/internal/transport/restapi/internal/middleware"

	"github.com/zeromicro/go-zero/rest"
)

// Deps carries the business dependencies of the transport.
//
// They are injected by the composition root (cmd/restapi) rather than read from
// the go-zero config file, because the application layer already owns how they
// are built (Ent client, privacy policy, tool registry, harness). The go-zero
// config file stays responsible for transport knobs only: host, port, timeouts,
// body limits and logging.
type Deps struct {
	Agent           *agent.Harness
	Sessions        *session.Store
	Approvals       *approval.Store
	Runs            *run.Store
	Turns           *turn.Store
	Auth            *auth.Authenticator
	Logger          *slog.Logger
	Metrics         *observability.Metrics
	KnowledgeClient *ent.Client
}

// ServiceContext is what goctl passes to every handler and logic. goctl owns the
// Config/RoleAdmin/RoleAgent/RoleApprover fields; the business dependencies
// above were added by hand.
type ServiceContext struct {
	Config config.Config

	Agent     *agent.Harness
	Sessions  *session.Store
	Approvals *approval.Store
	Runs      *run.Store
	Turns     *turn.Store
	Logger    *slog.Logger
	Metrics   *observability.Metrics
	EntClient *ent.Client

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
		Metrics:      deps.Metrics,
		EntClient:    deps.KnowledgeClient,
		RoleAdmin:    middleware.NewRoleAdminMiddleware().Handle,
		RoleAgent:    middleware.NewRoleAgentMiddleware().Handle,
		RoleApprover: middleware.NewRoleApproverMiddleware().Handle,
	}
}
