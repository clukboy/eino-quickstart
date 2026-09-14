// Package restapi is the go-zero REST transport for the Eino harness.
//
// It mirrors the dependency set of internal/transport/httpapi (net/http) and
// internal/transport/hertzapi (Hertz), so a composition root can pick any of the
// three without touching the application layer.
//
// Layout is goctl's canonical one:
//
//	restapi.api                 the API contract, source of truth for goctl
//	restapi.go                  this file: server, middleware chain, hand-written routes
//	stream.go                   the two SSE handlers (see below)
//	logx.go                     routes go-zero's own logs into the project logger
//	internal/config             generated go-zero config (rest.RestConf)
//	internal/handler            generated handlers (thin: parse -> call logic -> write)
//	internal/logic              generated logic, filled with the business code
//	internal/middleware         role checks referenced from the .api DSL
//	internal/svc                generated dependency container, extended by hand
//	internal/types              generated request/response types
//	internal/httpx              error envelope + SSE writer (leaf package)
//
// Three routes are registered here instead of by the generator:
//
//   - POST /api/v1/chat and POST /api/v1/approvals/:id/resume are SSE streams
//     whose clients rely on named events (`event: message` and friends). goctl's
//     SSE generator emits unnamed `data:` frames only, so these keep a
//     hand-written handler (stream.go) that writes the original frames byte for
//     byte.
//   - GET /metrics returns Prometheus text, which the (resp, err) logic
//     signature cannot express.
//
// Status codes the (resp, err) signature cannot express — the upload
// placeholder's 202 and the knowledge-base grant/revoke 204 — travel on a typed
// error carrying just a status, via httpx.Error.Empty(). See internal/httpx.
package restapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/agent"
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
	"github.com/zeromicro/go-zero/rest"
)

// Server is the go-zero transport. Business dependencies live on svc.Deps;
// transport knobs (host, port, timeouts, body limits) live on Config.
type Server struct {
	svc.Deps
	Config config.Config
}

// Options is the public composition-root input.
//
// It exists because Go's internal-package rule stops cmd/restapi from naming
// svc.Deps or config.Config: those live under restapi/internal/ and are only
// importable from inside this subtree. Options therefore declares the
// dependencies inline, and New loads the transport config itself from
// ConfigFile.
type Options struct {
	// ConfigFile is the go-zero transport config (the file goctl would have
	// pointed -f at, normally .../restapi/etc/restapi.yaml).
	ConfigFile string

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

// New loads the transport config and validates the dependency set, failing fast
// on a missing dependency rather than panicking on the first request.
func New(opts Options) (*Server, error) {
	var cfg config.Config
	if err := conf.Load(opts.ConfigFile, &cfg); err != nil {
		return nil, fmt.Errorf("restapi: load %s: %w", opts.ConfigFile, err)
	}

	deps := svc.Deps{
		Agent:           opts.Agent,
		Sessions:        opts.Sessions,
		Approvals:       opts.Approvals,
		Runs:            opts.Runs,
		Turns:           opts.Turns,
		Auth:            opts.Auth,
		Logger:          opts.Logger,
		Metrics:         opts.Metrics,
		KnowledgeClient: opts.KnowledgeClient,
	}

	if deps.Agent == nil {
		return nil, errors.New("restapi: agent harness is required")
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

	server.Use(withLogger(observability.HTTPTrace, s.Logger))
	server.Use(rest.ToMiddleware(observability.RequestID))
	server.Use(withLogger(observability.Recover, s.Logger))
	server.Use(withLogger(observability.AccessLog, s.Logger))
	if s.Metrics != nil {
		server.Use(rest.ToMiddleware(s.Metrics.Middleware))
	}
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

	if s.Metrics != nil {
		server.AddRoute(rest.Route{
			Method:  http.MethodGet,
			Path:    "/metrics",
			Handler: serverCtx.RoleAdmin(s.Metrics.Handler().ServeHTTP),
		})
	}

	return server, nil
}

// Run starts the server and blocks until ctx is cancelled. On cancellation it
// performs go-zero's graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	server, err := s.Engine()
	if err != nil {
		return err
	}
	defer server.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		server.Start()
	}()

	select {
	case <-ctx.Done():
		server.Stop()
		<-done
		return nil
	case <-done:
		return nil
	}
}

// withLogger binds the project logger into an observability middleware of the
// shape func(logger, next) http.Handler, yielding a rest.Middleware.
//
// HTTPTrace, Recover and AccessLog take the logger as their first argument
// (they each log or trace per request); RequestID and Metrics.Middleware do
// not, and go straight through rest.ToMiddleware.
func withLogger(
	middleware func(*slog.Logger, http.Handler) http.Handler,
	logger *slog.Logger,
) rest.Middleware {
	return rest.ToMiddleware(func(next http.Handler) http.Handler {
		return middleware(logger, next)
	})
}

// BridgeLogx routes go-zero's own logging (rest access logs, internal warnings)
// into the project's structured logger, so a single handler configuration and a
// single log file cover both. Call it before Engine.
func BridgeLogx(logger *slog.Logger) {
	if logger == nil {
		return
	}
	installLogxWriter(logger)
}
