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
	"eino-quickstart/internal/application/knowledge"
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
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag/grouping"
	"eino-quickstart/internal/rag/store/milvus"
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

	asynqQueue := asynq.NewAsynqClient(newAsynqConf(cfg))
	// 本进程只当 Producer：asynqQueue.Server 保持未启动状态，消费端在
	// cmd/worker 进程里。两者通过共享 Redis 连接，任务契约见
	// internal/platform/queue/tasks。asynq.enabled=false 时 Enqueue 会返回
	// 明确的错误，调用方必须把它当作请求失败浮出来。

	// 知识库用例是 HTTP 侧唯一的索引入口：它把正文写进 documents.content、
	// 建行、投递任务。transport 只调它，不认识队列。
	//
	// 召回也在这里装配：这个进程既要能写（上传、重建索引）也要能读
	// （POST /dataset/:id/search）。装配是分级的 —— 向量库或 embedding 没配
	// 只会关掉对应通道，关键词与精确通道照常工作，见 newRetrievalSearcher。
	// 召回结果的归并粒度按数据集类型配（knowledge.recallGrouping）。在这里解析
	// 一次并注入：粒度名写错要在启动时就报出来，而不是等某个数据集被搜到时静默
	// 回落成默认粒度 —— 后者只会表现为「条数不太对」，没人会联想到配置。
	recallGrouping, err := grouping.NewPolicy(cfg.Knowledge.RecallGrouping)
	if err != nil {
		return fmt.Errorf("解析 knowledge.recallGrouping: %w", err)
	}

	knowledgeService, err := knowledge.NewService(knowledge.ServiceDeps{
		Client: entClient,
		Queue: asynq.NewIndexQueue(asynqQueue, asynq.IndexQueueConfig{
			MaxRetries: cfg.Asynq.MaxRetries,
		}),
		Vectors:  newVectorCleaner(ctx, cfg, logger),
		Keywords: newKeywordCleaner(ctx, cfg, logger),
		Searcher: newRetrievalSearcher(ctx, cfg, entClient, logger),
		Grouping: recallGrouping,
		Limits: knowledge.Limits{
			DefaultTopK:        cfg.Knowledge.DefaultTopK,
			MaxTopK:            cfg.Knowledge.MaxTopK,
			MaxQueryCharacters: cfg.Knowledge.MaxQueryCharacters,
			// 单条结果正文的上限在 document 粒度下就是整篇文档的长度上限：
			// 一篇长型录几万字，不设上限时一次 top_k=20 就能带出几百 KB。
			MaxContentBytes: cfg.Knowledge.MaxResultBytes,
		},
		MaxDocumentBytes: cfg.Knowledge.MaxDocumentBytes,
		Logger:           logger,
	})
	if err != nil {
		return fmt.Errorf("init knowledge service: %w", err)
	}

	server, err := restapi.New(restapi.Options{
		ConfigFile: transportConfigPath,
		Agent:      harness,
		Knowledge:  knowledgeService,
		Sessions:   session.NewStore(entClient),
		Approvals:  approvals,
		Runs:       run.NewStore(entClient),
		Turns:      turn.NewStore(entClient),
		Auth:       authenticator,
		Logger:     logger,
		EntClient:  entClient,
	})
	if err != nil {
		return err
	}
	logx.DisableStat()

	// HTTP-only 进程：asynq 消费端已拆到 cmd/worker，这里不再把 worker 加进
	// ServiceGroup。进程里唯一的 service 就是 HTTP server，Stop 语义不变：
	// 监听由 go-zero 的 proc 关闭链收起。
	group := service.NewServiceGroup()

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

	// 阻塞到 HTTP server 停下：监听由 go-zero 的 proc 关闭链收起，main 随之
	// 退出。队列里的任务由 cmd/worker 进程继续消化，HTTP 重启不影响它们。
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

// newAsynqConf 把业务配置里的 asynq 段翻译成适配器配置。
//
// 连接参数、队列权重、重试上限、关闭超时都收在 asynq 一段里 —— 早先这些散在
// queue 与 asynq 两处、只有一处生效，是很容易配错的结构。
func newAsynqConf(cfg *config.Config) *asynq.AsynqConf {
	queues := make(map[string]int, len(cfg.Asynq.Queues))
	for _, q := range cfg.Asynq.Queues {
		queues[q.Name] = q.Weight
	}
	return &asynq.AsynqConf{
		Addr:                   cfg.Asynq.Redis.Addr,
		Username:               cfg.Asynq.Redis.Username,
		Pass:                   cfg.Asynq.Redis.Password,
		DB:                     cfg.Asynq.Redis.DB,
		Concurrency:            cfg.Asynq.Concurrency,
		Enable:                 cfg.Asynq.Enabled,
		Queues:                 queues,
		MaxRetries:             cfg.Asynq.MaxRetries,
		RetryDelaySeconds:      cfg.Asynq.RetryDelaySeconds,
		MaxRetryDelaySeconds:   cfg.Asynq.MaxRetryDelaySeconds,
		ShutdownTimeoutSeconds: cfg.Asynq.ShutdownTimeoutSeconds,
	}
}

// newVectorCleaner 给 HTTP 进程准备一个向量清理句柄。
//
// 删除文档时要顺手清掉 Milvus 里的向量，所以 HTTP 侧也需要向量库句柄。但这条
// 清理路径是**尽力而为**的（孤儿向量取不回来，靠后续全量重建收拾），所以连不上
// Milvus 不该挡住 HTTP 启动：失败只记警告，返回 nil，删除退化成「只摘索引」。
func newVectorCleaner(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) knowledge.VectorIndex {
	store, err := milvus.NewMilvusStore(ctx, &milvus.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions,
		MetricType: cfg.Milvus.MetricType,
	})
	if err != nil {
		logger.Warn(
			"vector store unavailable, document delete will skip vector cleanup",
			slog.String("error", err.Error()),
		)
		return nil
	}
	return store
}

// newKeywordCleaner 给 HTTP 进程准备一个检索索引（ES）清理句柄。
//
// 和 newVectorCleaner 完全同构：删除文档时要顺手清掉 ES 里的分块。这条清理
// 同样是尽力而为的（孤儿文档取不回来，靠后续全量重建收拾），所以连不上集群
// 只记警告并返回 nil，删除退化成「只摘索引」，不影响删文档本身。
//
// 注意这里不建索引：建索引与分词器校验属于索引链路的启动步骤（cmd/worker），
// 放在 HTTP 进程里会让两个进程对索引形态各有说法。
func newKeywordCleaner(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) knowledge.KeywordIndex {
	client, err := es.New(&cfg.ES)
	if err != nil {
		logger.Warn(
			"search index unavailable, document delete will skip keyword index cleanup",
			slog.String("error", err.Error()),
		)
		return nil
	}
	if client == nil {
		// 没配 ES：不清理是正确的（索引里本来就没有这篇文档的东西）。
		return nil
	}
	if err := client.Health(ctx); err != nil {
		logger.Warn(
			"search index unavailable, document delete will skip keyword index cleanup",
			slog.String("error", err.Error()),
		)
		return nil
	}
	return client.Writer()
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
