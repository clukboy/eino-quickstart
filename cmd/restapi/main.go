// Command restapi runs the Eino agent harness on the go-zero REST transport.
//
// It is the composition root. It builds the application dependencies — tool
// registry, executor, harness, stores, auth, observability — the same way
// cmd/server does, then hands them to internal/transport/restapi.
//
// Two config files, one layer each:
//
//	EINO_CONFIG       business config (configs/config.yaml), read by the
//	                  project's own loader: agent, model, storage, auth keys,
//	                  observability, execution.
//	EINO_REST_CONFIG  transport config (etc/restapi.yaml), read by go-zero's
//	                  conf.Load: host, port, body limit, timeout, logging.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"eino-quickstart/internal/application/agent"
	appmiddleware "eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/execution"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/checkpoint"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/platform/privacy"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/skill"
	"eino-quickstart/internal/tool"
	"eino-quickstart/internal/tool/builtin"
	"eino-quickstart/internal/tool/registry"
	"eino-quickstart/internal/transport/restapi"
)

const (
	defaultBusinessConfig  = "./configs/config.yaml"
	defaultTransportConfig = "./internal/transport/restapi/etc/restapi.yaml"
)

func main() {
	if err := runServer(); err != nil {
		log.Fatal(err)
	}
}

// runServer is named rather than `run` because the composition root also
// imports internal/platform/persistence/run.
func runServer() error {
	businessConfigPath := envOr("EINO_CONFIG", defaultBusinessConfig)
	transportConfigPath := envOr("EINO_REST_CONFIG", defaultTransportConfig)

	cfg, err := config.Load(businessConfigPath)
	if err != nil {
		return fmt.Errorf("load business config: %w", err)
	}
	if err := os.MkdirAll(cfg.Workspace.Root, 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registryInstance := registry.New()

	fileSystemTools, err := builtin.NewFileSystem(filepath.Clean(cfg.Workspace.Root), 256*1024)
	if err != nil {
		return err
	}
	for _, fileSystemTool := range fileSystemTools {
		if err := registryInstance.Register(fileSystemTool); err != nil {
			return err
		}
	}

	runner, err := newRunner(cfg)
	if err != nil {
		return err
	}

	if cfg.Execution.Mode != "disabled" {
		shell, err := builtin.NewShell(
			cfg.Workspace.Root,
			time.Duration(cfg.Workspace.ShellTimeoutSeconds)*time.Second,
			runner,
		)
		if err != nil {
			return err
		}
		if err := registryInstance.Register(shell); err != nil {
			return err
		}
	}

	skillTools, err := skill.NewLoader(cfg.Skills.Root, cfg.Skills.MaxReadBytes)
	if err != nil {
		return err
	}
	for _, skillTool := range skillTools {
		if err := registryInstance.Register(skillTool); err != nil {
			return err
		}
	}

	entClient, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	defer func() {
		if err := entClient.Close(); err != nil {
			log.Printf("database close failed: %v", err)
		}
	}()

	// The harness refuses to start unless "search_knowledge" is registered.
	// Retrieval is not wired into this transport yet — the retriever-backed
	// pipeline still lives behind the in-progress RAG refactor — so the
	// bindings-backed tool stands in and answers with the authorized knowledge
	// bases it can see.
	bindings, err := tool.NewEntKnowledgeBaseBindings(entClient)
	if err != nil {
		return err
	}
	knowledgeSearchTool, err := tool.NewKnowledgeSearchWithBindings("", bindings)
	if err != nil {
		return err
	}
	if err := registryInstance.Register(knowledgeSearchTool); err != nil {
		return err
	}

	argumentPolicy, err := privacy.NewArgumentPolicy(
		cfg.Security.MaxApprovalArgumentBytes,
		cfg.Security.SensitiveArgumentKeys,
	)
	if err != nil {
		return err
	}

	approvals := approval.NewStore(
		entClient,
		argumentPolicy,
		time.Duration(cfg.Security.ApprovalTTLSeconds)*time.Second,
	)
	policy := appmiddleware.NewPolicy(
		cfg.Security.AllowedTools,
		cfg.Security.RequireApprovalForShell,
		cfg.Security.RequireApprovalForWrite,
		approvals,
	)
	checkpoints := checkpoint.NewStore(entClient)

	harness, err := agent.NewHarness(ctx, cfg, registryInstance, policy, checkpoints)
	if err != nil {
		return err
	}

	logger, err := observability.NewLogger(
		cfg.Observability.LogLevel,
		cfg.Observability.ServiceName,
		cfg.Observability.Environment,
		cfg.Observability.LogFilePath,
		cfg.Observability.LogMaxSizeMB,
		cfg.Observability.LogMaxBackups,
		cfg.Observability.LogMaxAgeDays,
	)
	if err != nil {
		return err
	}

	var metrics *observability.Metrics
	if cfg.Observability.MetricsEnabled {
		metrics = observability.NewMetrics()
	}

	apiKeys := make([]auth.APIKey, 0, len(cfg.Auth.APIKeys))
	for _, configuredKey := range cfg.Auth.APIKeys {
		apiKeys = append(apiKeys, auth.APIKey{
			Secret: os.Getenv(configuredKey.KeyEnv),
			Identity: auth.Identity{
				Subject: configuredKey.Subject,
				Role:    auth.Role(configuredKey.Role),
			},
		})
	}
	authenticator, err := auth.New(apiKeys)
	if err != nil {
		return err
	}

	// Route go-zero's own logging into the project logger before the server
	// starts, so nothing escapes to a second sink.
	restapi.BridgeLogx(logger)

	server, err := restapi.New(restapi.Options{
		ConfigFile:      transportConfigPath,
		Agent:           harness,
		Sessions:        session.NewStore(entClient),
		Approvals:       approvals,
		Runs:            run.NewStore(entClient),
		Turns:           turn.NewStore(entClient),
		Auth:            authenticator,
		Logger:          logger,
		Metrics:         metrics,
		KnowledgeClient: entClient,
	})
	if err != nil {
		return err
	}

	logger.Info(
		"eino harness restapi started",
		slog.String("business_config", businessConfigPath),
		slog.String("transport_config", transportConfigPath),
		slog.String("workspace", cfg.Workspace.Root),
	)

	return server.Run(ctx)
}

// newRunner builds the tool executor. Mirrors cmd/server so the two transports
// accept the same configs/config.yaml.
func newRunner(cfg *config.Config) (execution.Runner, error) {
	switch cfg.Execution.Mode {
	case "docker":
		return execution.NewDockerRunner(execution.DockerConfig{
			Binary:       cfg.Execution.DockerBinary,
			Image:        cfg.Execution.Image,
			User:         cfg.Execution.User,
			MemoryLimit:  cfg.Execution.MemoryLimit,
			CPULimit:     cfg.Execution.CPULimit,
			PIDsLimit:    cfg.Execution.PIDsLimit,
			TmpFSSize:    cfg.Execution.TmpFSSize,
			AllowNetwork: cfg.Execution.AllowNetwork,
			MaxOutput:    cfg.Workspace.MaxOutputBytes,
		})
	case "local":
		return execution.NewLocalRunner(cfg.Workspace.MaxOutputBytes)
	case "disabled":
		return execution.NewDisabledRunner(), nil
	default:
		return nil, fmt.Errorf("unsupported execution mode: %s", cfg.Execution.Mode)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
