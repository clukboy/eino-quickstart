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
	"eino-quickstart/internal/platform/queue/asynq"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/skill"
	"eino-quickstart/internal/tool"
	"eino-quickstart/internal/tool/builtin"
	"eino-quickstart/internal/tool/registry"
	"eino-quickstart/internal/transport/restapi"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
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
	bindings, err := tool.NewEntDatasetBindings(entClient)
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

	asynqWorker := asynq.NewAsynqClient(&asynq.AsynqConf{
		Addr:         cfg.QueueConfig.Addr,
		Username:     cfg.QueueConfig.Username,
		Pass:         cfg.QueueConfig.Pass,
		DB:           cfg.QueueConfig.DB,
		Concurrency:  cfg.QueueConfig.Concurrency,
		SyncInterval: cfg.QueueConfig.SyncInterval,
		Enable:       cfg.QueueConfig.Enable,
	})

	server, err := restapi.New(restapi.Options{
		ConfigFile: transportConfigPath,
		Agent:      harness,
		Sessions:   session.NewStore(entClient),
		Approvals:  approvals,
		Runs:       run.NewStore(entClient),
		Turns:      turn.NewStore(entClient),
		Auth:       authenticator,
		Logger:     logger,
		EntClient:  entClient,
		Queue:      asynqWorker,
	})
	if err != nil {
		return err
	}
	logx.DisableStat()

	// HTTP 与索引 worker 交给同一个 ServiceGroup。Add 是前插、Stop 逆序执行，
	// 所以下面这个顺序会让 worker 先停：先掐掉消费，再收起 HTTP 监听，避免
	// 端口已经关了而任务还在被领走。
	group := service.NewServiceGroup()
	if cfg.QueueConfig.Enable {
		group.Add(asynqWorker)
	}

	group.Add(server)

	// 信号触发优雅关闭。ServiceGroup.Stop 幂等，go-zero 的 proc 关闭链也会走
	// 同一条路，两者相遇无副作用。
	go func() {
		<-ctx.Done()
		group.Stop()
	}()

	logger.Info(
		"eino harness restapi started",
		slog.String("business_config", businessConfigPath),
		slog.String("transport_config", transportConfigPath),
		slog.String("workspace", cfg.Workspace.Root),
	)

	// 阻塞到全部服务停下：HTTP 监听由 go-zero 的 proc 关闭链收起，asynq worker
	// 由 ServiceGroup.Stop 排空。两者都返回后这里才继续，main 随之退出。
	group.Start()

	logger.Info("eino harness restapi stopped")
	return nil
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
