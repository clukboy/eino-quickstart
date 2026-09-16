package restapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/observability"
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
	Logger    *slog.Logger
	EntClient *ent.Client
}

func New(opts Options) (*Server, error) {
	var cfg config.Config
	if err := conf.Load(opts.ConfigFile, &cfg); err != nil {
		return nil, fmt.Errorf("restapi: load %s: %w", opts.ConfigFile, err)
	}

	deps := svc.Deps{
		Agent:     opts.Agent,
		Knowledge: opts.Knowledge,
		Sessions:  opts.Sessions,
		Approvals: opts.Approvals,
		Runs:      opts.Runs,
		Turns:     opts.Turns,
		Auth:      opts.Auth,
		Logger:    opts.Logger,
		EntClient: opts.EntClient,
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
	if deps.Logger == nil {
		return nil, errors.New("restapi: logger is required")
	}

	return &Server{Deps: deps, Config: cfg}, nil
}

// Engine builds the go-zero server: global middleware, the routes generated from
// restapi.api, then the three hand-written routes. Exported so tests can drive
// the engine through httptest without binding a port.
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
	// Only two middlewares are ours:
	//
	//	TraceID      echoes the go-zero trace id back as X-Trace-ID. go-zero
	//	             injects W3C traceparent only, no bare trace id header.
	//	Authenticate the Bearer API key check. go-zero's handler.Authorize is
	//	             HMAC request signing, an entirely different contract.
	//
	// TraceID is registered first so the header is also set on the 401s that
	// Authenticate and the role checks produce.
	server.Use(rest.ToMiddleware(middleware.TraceID))
	server.Use(rest.ToMiddleware(middleware.Authenticate(s.Auth)))

	// Routes generated from restapi.api.
	handler.RegisterHandlers(server, serverCtx)

	// Hand-written routes. See the package comment for why these are not in the
	// .api file.
	stream := newStreamHandlers(serverCtx)
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/v1/chat",
		Handler: serverCtx.RoleAgent(stream.Chat),
	}, rest.WithSSE())
	server.AddRoute(rest.Route{
		Method:  http.MethodPost,
		Path:    "/api/v1/approvals/:id/resume",
		Handler: serverCtx.RoleAgent(stream.ResumeApproval),
	}, rest.WithSSE())

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
