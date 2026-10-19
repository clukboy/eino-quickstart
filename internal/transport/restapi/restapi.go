package restapi

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/application/usersync"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/platform/persistence/approval"
	consoleStore "eino-quickstart/internal/platform/persistence/console"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/platform/secretbox"
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
	UserSync     *usersync.Service

	// UploadMaxRequestBodyBytes / UploadMaxDocumentBytes / UploadMaxFiles
	// are the API business limits for multipart uploads. They are passed from
	// the platform config because this package only loads go-zero RestConf.
	UploadMaxRequestBodyBytes int64
	UploadMaxDocumentBytes    int64
	UploadMaxFiles            int

	// PasswordHashCost 是 bcrypt 成本，0 表示默认值。它属于**平台配置**
	// （config.Auth.Accounts.BcryptCost），而本层拿到的 cfg 只有 RestConf，
	// 所以由组合根读出来传进来 —— 与 Anonymous/Accounts 同一口径。
	PasswordHashCost int

	Logger    *slog.Logger
	EntClient *ent.Client
	Metrics   *observability.Metrics
	Console   *consoleStore.Store

	// SecretBox 可选：为 nil 表示这次部署没配主密钥，此时 console 写入模型
	// 供应商密钥会被明确拒绝，而不是明文落库。
	SecretBox *secretbox.Box
}

func New(opts Options) (*Server, error) {
	var cfg config.Config
	if err := conf.Load(opts.ConfigFile, &cfg); err != nil {
		return nil, fmt.Errorf("restapi: load %s: %w", opts.ConfigFile, err)
	}

	deps := svc.Deps{
		Agent:                     opts.Agent,
		Knowledge:                 opts.Knowledge,
		Sessions:                  opts.Sessions,
		Approvals:                 opts.Approvals,
		Runs:                      opts.Runs,
		Turns:                     opts.Turns,
		Auth:                      opts.Auth,
		Anonymous:                 opts.Anonymous,
		Accounts:                  opts.Accounts,
		AccountStore:              opts.AccountStore,
		UserSync:                  opts.UserSync,
		PasswordHashCost:          opts.PasswordHashCost,
		UploadMaxRequestBodyBytes: opts.UploadMaxRequestBodyBytes,
		UploadMaxDocumentBytes:    opts.UploadMaxDocumentBytes,
		UploadMaxFiles:            opts.UploadMaxFiles,
		Logger:                    opts.Logger,
		EntClient:                 opts.EntClient,
		Metrics:                   opts.Metrics,
		Console:                   opts.Console,
		SecretBox:                 opts.SecretBox,
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

func (s *Server) Engine() (*rest.Server, error) {
	serverCtx := svc.NewServiceContext(s.Config, s.Deps)

	httpx.Register()

	server, err := rest.NewServer(s.Config.RestConf)
	if err != nil {
		return nil, err
	}

	server.Use(rest.ToMiddleware(middleware.TraceID))
	server.Use(rest.ToMiddleware(middleware.Authenticate(s.Auth)))
	server.Use(rest.ToMiddleware(middleware.PasswordGuard))

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
