package restapi

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/transport/restapi/internal/config"
	"eino-quickstart/internal/transport/restapi/internal/handler"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/middleware"
	"eino-quickstart/internal/transport/restapi/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
)

type Server struct {
	svc.Deps
	Config config.Config

	mu      sync.Mutex
	stopped chan struct{}
}

var _ service.Service = (*Server)(nil)

type Options struct {
	ConfigFile string

	Agent     *agent.Harness
	Knowledge *knowledge.Service
	Sessions  *session.Store
	Approvals *approval.Store
	Runs      *run.Store
	Turns     *turn.Store
	Auth      *auth.Authenticator

	// Anonymous 可选：为 nil 表示不开放匿名身份，此时 /auth/anonymous 会明确
	// 回 503。签发者与校验者（Auth）必须是同一个密钥的两个视图，所以两者都在
	// 组合根里构造 —— 传输层只负责把令牌签出来，不持有密钥来源。
	Anonymous *auth.AnonymousIssuer

	// Accounts 与 AccountStore 可选，且必须同时给出（见 New 里的校验）。
	// 两者都为 nil 表示不开放账号登录，此时 /auth/login 与 /users* 会明确回
	// 503，而不是「登录成功但查不到账号」这种半开状态。
	Accounts     *auth.AccountIssuer
	AccountStore *account.Store

	// PasswordHashCost 是 bcrypt 成本，0 表示默认值。它属于**平台配置**
	// （config.Auth.Accounts.BcryptCost），而本层拿到的 cfg 只有 RestConf，
	// 所以由组合根读出来传进来 —— 与 Anonymous/Accounts 同一口径。
	PasswordHashCost int

	Logger    *slog.Logger
	EntClient *ent.Client
}

func New(opts Options) (*Server, error) {
	var cfg config.Config
	if err := conf.Load(opts.ConfigFile, &cfg); err != nil {
		return nil, fmt.Errorf("restapi: load %s: %w", opts.ConfigFile, err)
	}

	deps := svc.Deps{
		Agent:            opts.Agent,
		Knowledge:        opts.Knowledge,
		Sessions:         opts.Sessions,
		Approvals:        opts.Approvals,
		Runs:             opts.Runs,
		Turns:            opts.Turns,
		Auth:             opts.Auth,
		Anonymous:        opts.Anonymous,
		Accounts:         opts.Accounts,
		AccountStore:     opts.AccountStore,
		PasswordHashCost: opts.PasswordHashCost,
		Logger:           opts.Logger,
		EntClient:        opts.EntClient,
	}

	if deps.Agent == nil {
		return nil, errors.New("restapi: agent harness is required")
	}
	if deps.Knowledge == nil {
		return nil, errors.New("restapi: knowledge service is required")
	}
	if deps.Sessions == nil {
		return nil, errors.New("restapi: session store is required")
	}
	if deps.Approvals == nil {
		return nil, errors.New("restapi: approval store is required")
	}
	if deps.Runs == nil {
		return nil, errors.New("restapi: run store is required")
	}
	if deps.Turns == nil {
		return nil, errors.New("restapi: turn store is required")
	}
	if deps.Auth == nil {
		return nil, errors.New("restapi: authenticator is required")
	}
	// 账号登录的两半必须同时给出：只有 store 时登录接口签不出令牌（前端会
	// 卡在「密码对了却拿不到令牌」），只有 issuer 时查不到人（每个账号都
	// 不存在）。两者单独出现都是配置错误，在组合处就拒绝，别等第一次登录。
	if (deps.Accounts == nil) != (deps.AccountStore == nil) {
		return nil, errors.New(
			"restapi: account issuer and account store must be provided together",
		)
	}
	if deps.Logger == nil {
		return nil, errors.New("restapi: logger is required")
	}

	return &Server{Deps: deps, Config: cfg}, nil
}

// Engine builds the go-zero server: global middleware, then every route
// generated from restapi.api. Exported so tests can drive the engine through
// httptest without binding a port.
func (s *Server) Engine() (*rest.Server, error) {
	serverCtx := svc.NewServiceContext(s.Config, s.Deps)

	// Install the unified error envelope before any request can fail.
	httpx.Register()

	server, err := rest.NewServer(s.Config.RestConf)
	if err != nil {
		return nil, err
	}

	// The chain is go-zero's. rest assembles TraceHandler, LogHandler,
	// PrometheusHandler, RecoverHandler and the resilience handlers per route
	// from MiddlewaresConf (every field defaults to true; see etc/restapi.yaml),
	// so there is nothing to register for those here.
	//
	// server.Use middlewares are appended *after* that native chain, which makes
	// them run inside it: by the time these execute, TraceHandler has already
	// started the span and put it on the request context. Both depend on that.
	//
	// Three middlewares are ours:
	//
	//	TraceID      echoes the go-zero trace id back as X-Trace-ID. go-zero
	//	             injects W3C traceparent only, no bare trace id header.
	//	Authenticate the Bearer API key check. go-zero's handler.Authorize is
	//	             HMAC request signing, an entirely different contract.
	//	PasswordGuard blocks everything but /api/v1/auth/ while the identity
	//	             still carries must_change_password. It must run *after*
	//	             Authenticate — it reads the identity Authenticate put on
	//	             the context, and before Authenticate there is none, so it
	//	             would silently never fire.
	//
	// TraceID is registered first so the header is also set on the 401s that
	// Authenticate and the role checks produce.
	server.Use(rest.ToMiddleware(middleware.TraceID))
	server.Use(rest.ToMiddleware(middleware.Authenticate(s.Auth)))
	server.Use(rest.ToMiddleware(middleware.PasswordGuard))

	// Every route, including the two SSE ones. The stream group in
	// docs/stream/stream.api carries `sse: true`, so its routes are registered
	// here with rest.WithSSE() — nothing is added by hand any more.
	handler.RegisterHandlers(server, serverCtx)

	return server, nil
}

func (s *Server) Start() {
	s.mu.Lock()
	s.stopped = make(chan struct{})
	stopped := s.stopped
	s.mu.Unlock()

	defer close(stopped)

	if err := s.Config.SetUp(); err != nil {
		s.Logger.Error("restapi: service setup failed",
			slog.String("error", err.Error()))
		return
	}
	observability.BridgeLogx(s.Logger)

	server, err := s.Engine()
	if err != nil {
		s.Logger.Error("restapi: build engine failed",
			slog.String("error", err.Error()))
		return
	}
	server.Start()
}
func (s *Server) Stop() {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()

	if stopped == nil {
		// Start never ran, so there is no listener to wait for.
		return
	}
	<-stopped
}
