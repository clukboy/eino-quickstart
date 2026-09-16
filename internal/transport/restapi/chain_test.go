package restapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/transport/restapi/internal/config"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/middleware"
	"eino-quickstart/internal/transport/restapi/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

// These tests pin the go-zero-native middleware chain, not the business
// endpoints. They exist because the chain is exactly the kind of wiring that
// degrades silently: turn a MiddlewaresConf flag off, drop the Telemetry block,
// or register TraceID outside the native chain, and everything still compiles
// and still answers 200 — only the trace id quietly goes empty.
//
// The shipped config file is loaded rather than hand-built, because
// MiddlewaresConf gets its `default=true` values from conf.Load: a Go struct
// literal would leave every flag false and test a chain that never ships.

const (
	restConfigPath  = "etc/restapi.yaml"
	testAdminSecret = "test-admin-secret"
)

var (
	setupOnce  sync.Once
	setupErr   error
	metricsURL string
)

// ensureSetUp runs go-zero's ServiceConf.SetUp exactly once for the test
// binary. It is process-global by design: logx.SetUp, trace.StartAgent and
// prometheus.StartAgent are all sync.Once-guarded, so whichever test runs first
// decides the process-wide configuration.
//
// The prometheus port is redirected to a free one so the agent does not fight
// the real 9091, and the log level is raised so the native LogHandler does not
// spray access logs into the test output. Everything else comes from the
// shipped file.
func ensureSetUp(t *testing.T) {
	t.Helper()

	setupOnce.Do(func() {
		var cfg config.Config
		if err := conf.Load(restConfigPath, &cfg); err != nil {
			setupErr = fmt.Errorf("load %s: %w", restConfigPath, err)
			return
		}

		port, err := freePort()
		if err != nil {
			setupErr = err
			return
		}
		cfg.Prometheus.Host = "127.0.0.1"
		cfg.Prometheus.Port = port
		cfg.Log.Level = "severe"

		metricsURL = fmt.Sprintf("http://127.0.0.1:%d%s", port, cfg.Prometheus.Path)
		setupErr = cfg.SetUp()
	})

	if setupErr != nil {
		t.Fatalf("service setup: %v", setupErr)
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func loadShippedConfig(t *testing.T) config.Config {
	t.Helper()

	var cfg config.Config
	if err := conf.Load(restConfigPath, &cfg); err != nil {
		t.Fatalf("load %s: %v", restConfigPath, err)
	}

	return cfg
}

// newTestEngine builds the engine with stub dependencies and serves it through
// rest.Serverless, so requests can be driven by httptest without binding a
// port. Stub stores are enough: every request below is answered by the
// middleware chain or by /health, neither of which touches them.
func newTestEngine(t *testing.T) *rest.Serverless {
	t.Helper()
	ensureSetUp(t)

	authenticator, err := auth.New([]auth.APIKey{
		{
			Secret:   testAdminSecret,
			Identity: auth.Identity{Subject: "admin", Role: auth.RoleAdmin},
		},
	})
	if err != nil {
		t.Fatalf("build authenticator: %v", err)
	}

	server := &Server{
		Deps: svc.Deps{
			Agent:     &agent.Harness{},
			Sessions:  &session.Store{},
			Approvals: &approval.Store{},
			Runs:      &run.Store{},
			Turns:     &turn.Store{},
			Auth:      authenticator,
			Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Config: loadShippedConfig(t),
	}

	engine, err := server.Engine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	serverless, err := rest.NewServerless(engine)
	if err != nil {
		t.Fatalf("serverless: %v", err)
	}

	return serverless
}

func do(t *testing.T, engine *rest.Serverless, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	engine.Serve(recorder, request)

	return recorder
}

// TestShippedConfigKeepsEveryNativeMiddlewareOn guards the config file. All of
// MiddlewaresConf defaults to true, so the only way to lose a native middleware
// is to add an explicit false — which is what the previous revision did, to
// avoid double-processing against internal/platform/observability.
func TestShippedConfigKeepsEveryNativeMiddlewareOn(t *testing.T) {
	middlewares := loadShippedConfig(t).Middlewares

	flags := map[string]bool{
		"Trace":      middlewares.Trace,
		"Log":        middlewares.Log,
		"Prometheus": middlewares.Prometheus,
		"MaxConns":   middlewares.MaxConns,
		"Breaker":    middlewares.Breaker,
		"Shedding":   middlewares.Shedding,
		"Timeout":    middlewares.Timeout,
		"Recover":    middlewares.Recover,
		"Metrics":    middlewares.Metrics,
		"MaxBytes":   middlewares.MaxBytes,
		"Gunzip":     middlewares.Gunzip,
	}

	for name, enabled := range flags {
		if !enabled {
			t.Errorf("Middlewares.%s is false, want true (restapi runs go-zero's own chain)", name)
		}
	}
}

// TestShippedConfigKeepsAgentsConfigurable covers the two blocks that are easy
// to delete by accident and whose absence is silent: without Telemetry the
// tracer provider is a no-op so every trace id is empty, and without
// Prometheus.Host the scrape agent never listens.
func TestShippedConfigKeepsAgentsConfigurable(t *testing.T) {
	cfg := loadShippedConfig(t)

	if cfg.Telemetry.Name == "" {
		t.Error("Telemetry.Name is empty: no TracerProvider is installed and trace ids stay empty")
	}
	if cfg.Prometheus.Host == "" {
		t.Error("Prometheus.Host is empty: prometheus.StartAgent returns early and /metrics never opens")
	}
	if cfg.Prometheus.Path == "" {
		t.Error("Prometheus.Path is empty")
	}
}

// TestHealthIsPublicAndTraced is the positive path: no token, no store access,
// straight through the native chain and back.
func TestHealthIsPublicAndTraced(t *testing.T) {
	engine := newTestEngine(t)

	recorder := do(t, engine, http.MethodGet, "/health", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get(middleware.TraceIDHeader); got == "" {
		t.Error("X-Trace-ID is empty: the TraceID middleware did not see a span")
	}
	if got := recorder.Header().Get("traceparent"); got == "" {
		t.Error("traceparent is missing: go-zero's native TraceHandler did not run")
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

// TestTraceIDHeaderAgreesWithErrorEnvelope pins the one contract the trace id
// carries: the X-Trace-ID header and the envelope's request_id are the same
// value. An unknown token is the cheapest way to reach the envelope — it is
// rejected by our own Authenticate middleware, which renders through httpx.
func TestTraceIDHeaderAgreesWithErrorEnvelope(t *testing.T) {
	engine := newTestEngine(t)

	recorder := do(t, engine, http.MethodPost, "/api/v1/sessions", "not-a-real-token")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}

	headerID := recorder.Header().Get(middleware.TraceIDHeader)
	if headerID == "" {
		t.Fatal("X-Trace-ID is empty on the error path")
	}

	var envelope httpx.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	if envelope.Code != httpx.CodeInvalidCredentials {
		t.Errorf("code = %q, want %q", envelope.Code, httpx.CodeInvalidCredentials)
	}
	if envelope.RequestID != headerID {
		t.Errorf("envelope request_id = %q, header = %q; want the same value",
			envelope.RequestID, headerID)
	}
}

// TestMetricsIsNoLongerARoute asserts the hand-written route is gone. Metrics
// now leave through go-zero's agent on its own port; 8090 must not serve them.
func TestMetricsIsNoLongerARoute(t *testing.T) {
	engine := newTestEngine(t)

	recorder := do(t, engine, http.MethodGet, "/metrics", testAdminSecret)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (metrics moved to the agent port); body=%s",
			recorder.Code, recorder.Body.String())
	}
}

// TestPrometheusAgentServesGoZeroMetrics closes the loop end to end: drive one
// request through the engine so go-zero's PrometheusHandler records it, then
// scrape the agent's own port and find the metric family there. This is what
// proves SetUp actually started the agent and that the middleware is feeding
// the registry the agent serves.
func TestPrometheusAgentServesGoZeroMetrics(t *testing.T) {
	ensureSetUp(t)

	engine := newTestEngine(t)
	if recorder := do(t, engine, http.MethodGet, "/health", ""); recorder.Code != http.StatusOK {
		t.Fatalf("warm-up request failed: %d", recorder.Code)
	}

	response, err := http.Get(metricsURL)
	if err != nil {
		t.Fatalf("scrape %s: %v", metricsURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}

	const family = "http_server_requests_code_total"
	if !strings.Contains(string(body), family) {
		t.Errorf("%s not found in the agent's output; the native PrometheusHandler is not recording", family)
	}
}
