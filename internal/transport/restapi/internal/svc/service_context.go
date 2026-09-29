// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package svc

import (
	"log/slog"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
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

	// Anonymous 是匿名身份的签发者，为 nil 表示这次部署不开放匿名访问
	// （此时 /auth/anonymous 返回 503，而不是签发一个没人能校验的身份）。
	Anonymous *auth.AnonymousIssuer

	// Accounts 与 AccountStore 是账号登录这一对：签发者负责令牌，store 负责
	// 账号与密码。它们必须一起为 nil 或一起存在 —— 只给签发者而没有 store
	// 会让登录接口「能签令牌但查不到人」，只给 store 则每个令牌都签不出来。
	// 因此组合根（cmd/restapi）按同一个开关构造它们，见 Options.Accounts。
	Accounts     *auth.AccountIssuer
	AccountStore *account.Store

	// PasswordHashCost 是哈希「管理员设定的初始密码」时用的 bcrypt 成本，
	// 0 表示用 bcrypt.DefaultCost。
	//
	// 它来自**平台配置**（config.Auth.Accounts.BcryptCost），而传输层拿到的
	// 只有 go-zero 的 RestConf，所以只能在组合根读出来、当成普通依赖传进来。
	// 放在这里而不是硬编码：成本每加 1 就把单次哈希耗时翻倍，而 /auth/login
	// 没有限流（见 docs/known-gaps.md），慢机器上需要能调低。
	PasswordHashCost int

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
	Anonymous    *auth.AnonymousIssuer
	Accounts     *auth.AccountIssuer
	AccountStore *account.Store

	// PasswordHashCost 的语义同 Deps.PasswordHashCost。
	PasswordHashCost int

	Logger       *slog.Logger
	EntClient    *ent.Client
	RoleAdmin    rest.Middleware
	RoleAgent    rest.Middleware
	RoleApprover rest.Middleware
}

// NewServiceContext wires the dependency set into a ServiceContext.
func NewServiceContext(c config.Config, deps Deps) *ServiceContext {
	return &ServiceContext{
		Config:           c,
		Agent:            deps.Agent,
		Knowledge:        deps.Knowledge,
		Sessions:         deps.Sessions,
		Approvals:        deps.Approvals,
		Runs:             deps.Runs,
		Turns:            deps.Turns,
		Anonymous:        deps.Anonymous,
		Accounts:         deps.Accounts,
		AccountStore:     deps.AccountStore,
		PasswordHashCost: deps.PasswordHashCost,
		Logger:           deps.Logger,
		EntClient:        deps.EntClient,
		RoleAdmin:        middleware.NewRoleAdminMiddleware().Handle,
		RoleAgent:        middleware.NewRoleAgentMiddleware().Handle,
		RoleApprover:     middleware.NewRoleApproverMiddleware().Handle,
	}
}
