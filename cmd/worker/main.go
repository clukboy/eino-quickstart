// Command worker runs the async task consumer as its own process.
//
// HTTP 与消费端拆分后，cmd/restapi 只负责接收请求并投递任务；本进程只负责
// 消费。两个进程之间没有函数调用，连接方式是：
//
//	共享的 Redis 队列（asynq broker，configs/config.yaml 的 asynq 段）
//	+ internal/platform/queue/tasks 里的任务契约（类型常量与 payload 结构）。
//
// 本进程装配的是「文档索引」这条链路的全部重活：读正文 → 按内容拆出产品块 →
// 切块 → embedding → 写向量库。其中「读入 → 拆分 → 切块」这一段走
// internal/rag 的 Pipeline（与 cmd/ragserver 共用同一份实现），落库与分块
// 状态机留在 internal/application/knowledge。这些依赖（PostgreSQL、Milvus、
// embedding 服务）都只在这里构造，HTTP 进程一个都不碰 —— 这正是当初把 worker
// 拆出去的目的。
//
// 新增一种任务：tasks 包里加常量与 payload，HTTP 侧经应用层加投递入口，
// 本文件的 registerHandlers 里注册 handler，两侧部署顺序要能容忍旧 payload。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/observability"
	asynqqueue "eino-quickstart/internal/platform/queue/asynq"
	"eino-quickstart/internal/platform/queue/tasks"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/store/milvus"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
)

const defaultBusinessConfig = "./configs/config.yaml"

// traceFlushTimeout 是退出前等 OTLP 导出的上限。只在进程收尾时用，短一点没关系：
// 超时就丢掉未发送的 span，不能让一个连不上的 collector 拖住整个进程的退出。
const traceFlushTimeout = 5 * time.Second

func main() {
	if err := runWorker(); err != nil {
		log.Fatal(err)
	}
}

func runWorker() error {
	businessConfigPath := envOr("EINO_CONFIG", defaultBusinessConfig)

	cfg, err := config.Load(businessConfigPath)
	if err != nil {
		return fmt.Errorf("load business config: %w", err)
	}

	// worker 进程不需要 workspace，但 config.Load 之后业务代码可能依赖它存在。
	if err := os.MkdirAll(cfg.Workspace.Root, 0o755); err != nil {
		return err
	}

	if !cfg.Asynq.Enabled {
		return errors.New("asynq.enabled=false: worker 没有可消费的队列，拒绝空转启动")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
	logger = logger.With(slog.String("process", "worker"))

	// asynq 与 go-zero 的内部日志走 logx，桥到项目 slog，和 restapi 进程落在
	// 同一个观测面。
	logx.DisableStat()
	observability.BridgeLogx(logger)

	// 链路追踪。worker 不走 go-zero 的 rest/rpc，所以没有 ServiceConf.SetUp()
	// 帮它装 provider，必须自己装：没有 provider 时 otel 的 tracer 是空实现，
	// 从任务 payload 里恢复出来的 traceparent 无处落地，HTTP 那条 trace 到
	// worker 就断了 —— 而且断得无声无息，日志里连一个空的 trace 字段都不会有。
	//
	// 服务名刻意与 HTTP 进程不同（observability.workerServiceName）：异步那一段
	// 要能看成一次跨服务调用，而不是「服务自己调自己」。
	otlpEndpoint := cfg.Observability.OTLPEndpoint
	shutdownTracing, err := observability.SetupTracing(ctx, observability.TraceConfig{
		ServiceName: cfg.Observability.WorkerTraceName(),
		Environment: cfg.Observability.Environment,
		Endpoint:    otlpEndpoint,
		Insecure:    cfg.Observability.OTLPInsecure,
		SampleRatio: cfg.Observability.TraceSampleRatio,
	})
	if err != nil {
		return fmt.Errorf("init tracer provider: %w", err)
	}
	defer func() {
		// 冲刷要用独立的超时上下文：这里的 ctx 是信号上下文，进程退出时已经被
		// 取消，拿它去 Shutdown 会把还在 Batcher 里没发出去的 span 直接丢掉 ——
		// 而「优雅退出时最后一批 span 丢失」恰恰是最难注意到的那种丢数据。
		flushCtx, cancel := context.WithTimeout(context.Background(), traceFlushTimeout)
		defer cancel()
		if err := shutdownTracing(flushCtx); err != nil {
			logger.Error("trace provider shutdown failed", slog.String("error", err.Error()))
		}
	}()
	logger.Info(
		"tracing enabled",
		slog.String("service_name", cfg.Observability.WorkerTraceName()),
		slog.String("otlp_endpoint", otlpEndpoint),
		slog.Bool("exporting", otlpEndpoint != ""),
		slog.Float64("sample_ratio", cfg.Observability.TraceSampleRatio),
	)

	entClient, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := entClient.Close(); err != nil {
			logger.Error("database close failed", slog.String("error", err.Error()))
		}
	}()

	// 正文以文件为唯一真相：索引时按 documents.source 把文件读回来。
	contentStore, err := rag.NewContentStore(
		cfg.Knowledge.Root,
		int64(cfg.Knowledge.MaxDocumentBytes),
	)
	if err != nil {
		return fmt.Errorf("init knowledge content store: %w", err)
	}

	embedder, err := newEmbedder(ctx, cfg)
	if err != nil {
		return err
	}

	vectorStore, err := milvus.NewMilvusStore(ctx, &milvus.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions,
		MetricType: cfg.Milvus.MetricType,
	})
	if err != nil {
		return fmt.Errorf("connect vector store: %w", err)
	}
	defer func() {
		if err := vectorStore.Close(ctx); err != nil {
			logger.Error("vector store close failed", slog.String("error", err.Error()))
		}
	}()
	// 启动时就确保集合存在、维度正确、已经 load：这些都要跟 Milvus 往返，
	// 放在这里失败是启动失败；放到任务里就是每个任务都白跑一遍。
	if err := vectorStore.EnsureCollection(ctx); err != nil {
		return fmt.Errorf("ensure vector collection: %w", err)
	}

	// 索引链的前半段（读入 -> 按产品拆分 -> 切块）统一交给 rag.Pipeline，
	// 与 cmd/ragserver 共用同一份实现，切块规则只有一处定义。
	//
	// 这里**不注入 Store**：Pipeline 的落库尾段不认识 document_chunks 的
	// pending / indexed 状态，也没有 content_hash 的幂等口径，持久化与状态机
	// 仍由 Indexer 自己写。DocRoot 必须与 ContentStore 的 root 是同一个目录，
	// 否则 Pipeline 读不到 Indexer 交给它的那条路径。
	pipeline, err := rag.NewPipeline(ctx, rag.Config{
		Embedder:      embedder,
		DocRoot:       contentStore.Root(),
		MaxFileBytes:  int64(cfg.Knowledge.MaxDocumentBytes),
		ChunkMaxChars: cfg.Knowledge.ChunkSizeCharacters,
	}, entClient)
	if err != nil {
		return fmt.Errorf("init rag pipeline: %w", err)
	}

	indexer, err := knowledge.NewIndexer(knowledge.IndexerConfig{
		Client:   entClient,
		Content:  contentStore,
		Pipeline: pipeline,
		Embedder: embedder,
		Vectors:  vectorStore,
		// 一次送多少段文本去 embedding，和 indexer.batchSize 是同一个旋钮。
		BatchSize: cfg.Indexer.BatchSize,
		Logger:    logger,
	})
	if err != nil {
		return fmt.Errorf("init indexer: %w", err)
	}

	asynqConf := newAsynqConf(cfg)
	queueClient := asynqqueue.NewAsynqClient(asynqConf)
	defer func() {
		if queueClient.Client == nil {
			return
		}
		if err := queueClient.Close(); err != nil {
			logger.Error("queue client close failed", slog.String("error", err.Error()))
		}
	}()

	registerHandlers(queueClient, indexer)

	// worker 是本进程唯一的 service，直接走 ServiceGroup + 信号上下文的组合：
	// ServiceGroup.Start 阻塞，信号触发 group.Stop -> asynq Shutdown 优雅排空。
	// 已在跑的索引任务可能正卡在 embedding 的网络往返上，所以
	// asynq.shutdownTimeoutSeconds 要按任务的实际耗时配，别用默认的 20s ——
	// 超时的任务会被退回队列重跑，白烧一次 embedding。
	group := service.NewServiceGroup()
	group.Add(queueClient)

	go func() {
		<-ctx.Done()
		group.Stop()
	}()

	logger.Info(
		"eino worker started",
		slog.String("business_config", businessConfigPath),
		slog.String("queue_addr", cfg.Asynq.Redis.Addr),
		slog.Int("concurrency", cfg.Asynq.Concurrency),
		slog.Any("queues", asynqConf.QueuesOrDefault()),
		slog.String("knowledge_root", contentStore.Root()),
		slog.String("milvus_collection", cfg.Milvus.Collection),
	)
	group.Start()
	logger.Info("eino worker stopped")
	return nil
}

// registerHandlers 把任务类型绑定到处理函数。必须在 Start 之前完成：
// ServeMux 启动后不再安全地写注册表。
func registerHandlers(c *asynqqueue.AsynqClient, indexer *knowledge.Indexer) {
	c.Register(tasks.TypeKnowledgeIndex, indexer.HandleTask)
}

// newEmbedder 构造 embedding 客户端。
//
// 密钥只从环境变量读，不进配置文件：embedding 的 API key 和数据库口令同级，
// 落进 yaml 就会跟着配置一起进版本库。
func newEmbedder(ctx context.Context, cfg *config.Config) (*rag.Embedder, error) {
	apiKey := os.Getenv(cfg.Embedding.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf(
			"environment variable %s is required for embedding",
			cfg.Embedding.APIKeyEnv,
		)
	}
	embedder, err := rag.NewEmbedder(ctx, &openai.EmbeddingConfig{
		APIKey:     apiKey,
		BaseURL:    cfg.Embedding.BaseURL,
		Model:      cfg.Embedding.Model,
		Dimensions: &cfg.Embedding.Dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("init embedder: %w", err)
	}
	return embedder, nil
}

// newAsynqConf 把业务配置里的 asynq 段翻译成适配器配置。
func newAsynqConf(cfg *config.Config) *asynqqueue.AsynqConf {
	queues := make(map[string]int, len(cfg.Asynq.Queues))
	for _, q := range cfg.Asynq.Queues {
		queues[q.Name] = q.Weight
	}
	return &asynqqueue.AsynqConf{
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

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
