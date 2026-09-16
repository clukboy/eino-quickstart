// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"log/slog"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/transport/restapi/internal/config"
	"eino-quickstart/internal/transport/restapi/internal/middleware"

	"github.com/zeromicro/go-zero/rest"
)

// Deps 是传输层的全部外部依赖。
//
// 注意这里没有队列：传输层只认识 Agent 与 Knowledge 两个应用层入口，投递
// 索引任务、任务进哪个队列、payload 长什么样，全是 knowledge 包内部的事。
// 早先这里挂着一个 queue.Producer，让 handler 直接拼队列消息 —— 那等于把
// 队列的内部概念（任务类型、载荷字段）暴露成了对外 HTTP 契约。
type Deps struct {
	Agent     *agent.Harness
	Knowledge *knowledge.Service
	Sessions  *session.Store
	Approvals *approval.Store
	Runs      *run.Store
	Turns     *turn.Store
	Auth      *auth.Authenticator
	Logger    *slog.Logger
	EntClient *ent.Client
}

// ServiceContext is what goctl passes to every handler and logic. goctl owns the
// Config/RoleAdmin/RoleAgent/RoleApprover fields; the business dependencies
// above were added by hand.
type ServiceContext struct {
	Config       config.Config
	Agent        *agent.Harness
	Knowledge    *knowledge.Service
	Sessions     *session.Store
	Approvals    *approval.Store
	Runs         *run.Store
	Turns        *turn.Store
	Logger       *slog.Logger
	EntClient    *ent.Client
	RoleAdmin    rest.Middleware
	RoleAgent    rest.Middleware
	RoleApprover rest.Middleware
}

// NewServiceContext wires the dependency set into a ServiceContext.
func NewServiceContext(c config.Config, deps Deps) *ServiceContext {
	return &ServiceContext{
		Config:       c,
		Agent:        deps.Agent,
		Knowledge:    deps.Knowledge,
		Sessions:     deps.Sessions,
		Approvals:    deps.Approvals,
		Runs:         deps.Runs,
		Turns:        deps.Turns,
		Logger:       deps.Logger,
		EntClient:    deps.EntClient,
		RoleAdmin:    middleware.NewRoleAdminMiddleware().Handle,
		RoleAgent:    middleware.NewRoleAgentMiddleware().Handle,
		RoleApprover: middleware.NewRoleApproverMiddleware().Handle,
	}
}
