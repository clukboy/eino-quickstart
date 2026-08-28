package main

import (
	"context"
	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/knowledge/embedding"
	retriever "eino-quickstart/internal/knowledge/retrieval"
	"eino-quickstart/internal/knowledge/vectorstore"
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
	server "eino-quickstart/internal/transport/httpapi"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	configPath := os.Getenv("EINO_CONFIG")
	if configPath == "" {
		configPath = "./configs/config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.Workspace.Root, 0755); err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	reg := registry.New()
	fsTools, err := builtin.NewFileSystem(filepath.Clean(cfg.Workspace.Root), 256*1024)
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range fsTools {
		if err := reg.Register(t); err != nil {
			log.Fatal(err)
		}
	}

	var runner execution.Runner

	switch cfg.Execution.Mode {
	case "docker":
		runner, err = execution.NewDockerRunner(
			execution.DockerConfig{
				Binary:       cfg.Execution.DockerBinary,
				Image:        cfg.Execution.Image,
				User:         cfg.Execution.User,
				MemoryLimit:  cfg.Execution.MemoryLimit,
				CPULimit:     cfg.Execution.CPULimit,
				PIDsLimit:    cfg.Execution.PIDsLimit,
				TmpFSSize:    cfg.Execution.TmpFSSize,
				AllowNetwork: cfg.Execution.AllowNetwork,
				MaxOutput:    cfg.Workspace.MaxOutputBytes,
			},
		)
	case "local":
		runner, err = execution.NewLocalRunner(
			cfg.Workspace.MaxOutputBytes,
		)
	case "disabled":
		runner = execution.NewDisabledRunner()
	default:
		err = fmt.Errorf(
			"unsupported execution mode: %s",
			cfg.Execution.Mode,
		)
	}

	if err != nil {
		log.Fatal(err)
	}

	if cfg.Execution.Mode != "disabled" {
		shell, err := builtin.NewShell(
			cfg.Workspace.Root,
			time.Duration(
				cfg.Workspace.ShellTimeoutSeconds,
			)*time.Second,
			runner,
		)
		if err != nil {
			log.Fatal(err)
		}

		if err := reg.Register(shell); err != nil {
			log.Fatal(err)
		}
	}

	skillTools, err := skill.NewLoader(
		cfg.Skills.Root,
		cfg.Skills.MaxReadBytes,
	)
	if err != nil {
		log.Fatal(err)
	}

	for _, skillTool := range skillTools {
		if err := reg.Register(skillTool); err != nil {
			log.Fatal(err)
		}
	}
	entClient, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		log.Fatal(err)
	}

	argumentPolicy, err := privacy.NewArgumentPolicy(cfg.Security.MaxApprovalArgumentBytes, cfg.Security.SensitiveArgumentKeys)
	if err != nil {
		log.Fatal(err)
	}
	approvals := approval.NewStore(entClient, argumentPolicy, time.Duration(cfg.Security.ApprovalTTLSeconds)*time.Second)
	policy := middleware.NewPolicy(
		cfg.Security.AllowedTools,
		cfg.Security.RequireApprovalForShell,
		cfg.Security.RequireApprovalForWrite,
		approvals,
	)
	checkpoints := checkpoint.NewStore(entClient)

	embedder, err := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL:    cfg.Embedding.BaseURL,
		APIKey:     os.Getenv(cfg.Embedding.APIKeyEnv),
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
		BatchSize:  cfg.Embedding.BatchSize,
	})
	if err != nil {
		log.Fatal(err)
	}

	vecStore, err := vectorstore.NewMilvusStore(ctx, vectorstore.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions,
		MetricType: cfg.Milvus.MetricType,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := vecStore.EnsureCollection(ctx); err != nil {
		log.Fatal(err)
	}

	hybridRetriever := &retriever.HybridRetriever{
		Client:             entClient,
		Embedder:           embedder,
		VectorStore:        vecStore,
		KeywordSearcher:    retriever.NewPostgresKeywordSearcher(entClient),
		DefaultTopK:        cfg.Knowledge.DefaultTopK,
		MaxTopK:            cfg.Knowledge.MaxTopK,
		VectorCandidates:   cfg.Milvus.TopKCandidate,
		KeywordCandidates:  cfg.Retrieval.KeywordCandidateLimit,
		MaxQueryCharacters: cfg.Knowledge.MaxQueryCharacters,
		MaxResultBytes:     cfg.Knowledge.MaxResultBytes,
		VectorWeight:       cfg.Retrieval.VectorWeight,
		KeywordWeight:      cfg.Retrieval.KeywordWeight,
		RRFSmoothing:       cfg.Retrieval.RRFSmoothing,
	}

	knowledgeSearchTool, err := tool.NewKnowledgeSearch(hybridRetriever, "")
	if err != nil {
		log.Fatal(err)
	}
	if err := reg.Register(knowledgeSearchTool); err != nil {
		log.Fatal(err)
	}

	ag, err := agent.NewHarness(ctx, cfg, reg, policy, checkpoints)
	if err != nil {
		log.Fatal(err)
	}
	sessions := session.NewStore(entClient)

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
		log.Fatal(err)
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
		log.Fatal(err)
	}

	var metrics *observability.Metrics
	if cfg.Observability.MetricsEnabled {
		metrics = observability.NewMetrics()
	}
	runs := run.NewStore(entClient)
	turns := turn.NewStore(entClient)
	srv := &server.Server{
		Agent:          ag,
		Sessions:       sessions,
		Turns:          turns,
		Approvals:      approvals,
		Runs:           runs,
		Authenticator:  authenticator,
		Logger:         logger,
		Metrics:        metrics,
		MaxRequestBody: int64(cfg.Runtime.MaxRequestBodyBytes),
	}
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadTimeout:       time.Duration(cfg.Runtime.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.Runtime.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Runtime.IdleTimeoutSeconds) * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info(
		"eino harness server started",
		slog.String("address", addr),
		slog.String("workspace", cfg.Workspace.Root),
	)

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(
		shutdownSignals,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer signal.Stop(shutdownSignals)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}

	case receivedSignal := <-shutdownSignals:
		logger.Info(
			"shutdown signal received",
			"signal", receivedSignal.String(),
		)

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error(
				"http shutdown failed",
				"error", err,
			)
		}
		err := vecStore.Close(ctx)
		if err != nil {
			logger.Error(
				"vector store close failed",
				"error", err,
			)
		}
		if err := entClient.Close(); err != nil {
			logger.Error(
				"database close failed",
				"error", err,
			)
		}
	}
}
