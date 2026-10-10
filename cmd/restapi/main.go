package main

import (
	"context"
	"flag"
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
	"eino-quickstart/internal/application/usersync"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/execution"
	"eino-quickstart/internal/platform/im/wechat"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/checkpoint"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/platform/privacy"
	"eino-quickstart/internal/platform/queue/asynq"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/platform/usersource/work"
	"eino-quickstart/internal/rag/grouping"
	"eino-quickstart/internal/rag/store/milvus"
	"eino-quickstart/internal/skill"
	"eino-quickstart/internal/tool"
	"eino-quickstart/internal/tool/builtin"
	"eino-quickstart/internal/tool/registry"
	"eino-quickstart/internal/transport/restapi"

	"github.com/ArtisanCloud/PowerWeChat/v3/src/kernel"
	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
)

func main() {
	checkConfig := flag.Bool("check-config", false, "validate API config without opening external connections")
	flag.Parse()
	if *checkConfig {
		if _, err := config.LoadAPI(config.APIConfigPath()); err != nil {
			log.Fatal(err)
		}
		var transport rest.RestConf
		if err := conf.Load(config.APITransportConfigPath(), &transport); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("API config OK: %s; HTTP config OK: %s\n", config.APIConfigPath(), config.APITransportConfigPath())
		return
	}
	if err := runServer(); err != nil {
		log.Fatal(err)
	}
}

// runServer is named rather than `run` because the composition root also
// imports internal/platform/persistence/run.
func runServer() error {
	businessConfigPath := config.APIConfigPath()
	transportConfigPath := config.APITransportConfigPath()

	cfg, err := config.LoadAPI(businessConfigPath)
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

	// entClient, err := entx.OpenWithoutMigration(ctx, cfg.Storage)
	entClient, err := entx.OpenDevelopment(ctx, cfg.Storage) 
	if err != nil {
		return err
	}
	defer func() {
		if err := entClient.Close(); err != nil {
			log.Printf("database close failed: %v", err)
		}
	}()

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

	metrics := observability.NewMetrics()
	if err := metrics.RegisterDefault(); err != nil {
		return fmt.Errorf("register business metrics: %w", err)
	}

	searcher := newRetrievalSearcher(ctx, cfg, entClient, logger)
	if searcher == nil {
		return fmt.Errorf("knowledge retrieval is unavailable")
	}

	asynqQueue := asynq.NewProducer(newAsynqConf(cfg))


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
		Searcher: searcher,
		Grouping: recallGrouping,
		Limits: knowledge.Limits{
			DefaultTopK:        cfg.Knowledge.DefaultTopK,
			MaxTopK:            cfg.Knowledge.MaxTopK,
			MaxQueryCharacters: cfg.Knowledge.MaxQueryCharacters,
			MaxContentBytes: cfg.Knowledge.MaxResultBytes,
		},
		MaxDocumentBytes:       cfg.Knowledge.MaxDocumentBytes,
		Logger:                 logger,
		Metrics:                metrics,
	})
	if err != nil {
		return fmt.Errorf("init knowledge service: %w", err)
	}

	// The harness refuses to start unless "search_knowledge" is registered.
	// The same retrieval adapter is injected into the HTTP search API and the
	// conversation tool so ACL, channel degradation, and result formatting use
	// one authoritative search pipeline.
	bindings, err := tool.NewEntDatasetBindings(entClient)
	if err != nil {
		return err
	}
	knowledgeSearchTool, err := tool.NewKnowledgeSearchWithBindings("", bindings, knowledgeService)
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
	// 匿名身份（用户端）：令牌由服务端签发，subject 形如 anon:<uuid>，
	// 每个访客一个，会话因此按人隔离（见 internal/platform/auth/anonymous.go）。
	//
	// 它与静态 apiKeys 共用同一个 Authenticator —— 校验路径只多了一个「验签」
	// 分支，中间件和角色模型一行都不用改。未开启时不构造：既签不出也验不了，
	// /api/v1/auth/anonymous 会明确回 503，而不是签一个没人认的身份。
	var anonymousIssuer *auth.AnonymousIssuer
	if cfg.Auth.Anonymous.Enabled {
		anonymousIssuer, err = auth.NewAnonymousIssuer(auth.AnonymousConfig{
			Secret:        cfg.Auth.Anonymous.Secret,
			TTL:           time.Duration(cfg.Auth.Anonymous.TTLHours) * time.Hour,
			Role:          auth.Role(cfg.Auth.Anonymous.Role),
			SubjectPrefix: cfg.Auth.Anonymous.SubjectPrefix,
		})
		if err != nil {
			return fmt.Errorf("init anonymous issuer: %w", err)
		}
	}

	// 账号登录（管理后台建号 + 登录 + 首次强制改密）。与匿名身份并列的第三种
	// 凭证来源，签出的 subject 形如 user:<id>（见 internal/platform/auth/account.go）。
	//
	// AccountStore 与 AccountIssuer 必须同时构造：前者查人、后者签令牌，缺一个
	// 都是半开状态（restapi.New 会拒绝只给一半的配置）。未开启时两者都为 nil，
	// /auth/login 与 /users* 会明确回 503。
	var (
		accountIssuer *auth.AccountIssuer
		accountStore  *account.Store
	)
	if cfg.Auth.Accounts.Enabled {
		accountIssuer, err = auth.NewAccountIssuer(auth.AccountConfig{
			Secret: cfg.Auth.Accounts.Secret,
			TTL:    time.Duration(cfg.Auth.Accounts.TTLHours) * time.Hour,
		})
		if err != nil {
			return fmt.Errorf("init account issuer: %w", err)
		}
		accountStore = account.NewStore(entClient)
	}

	// 初始化微信服务 / 用户同步服务
	wechatClient := wechat.NewClient(cfg.Auth.UserSync.WechatConfig,(*kernel.UniversalOptions)(&cfg.Redis))

	var userSyncService *usersync.Service
	if cfg.Auth.UserSync.Enabled {
		source := work.NewUserSync(wechatClient)
		userSyncService, err = usersync.NewService(accountStore, source, cfg.Auth.Accounts.BcryptCost)
		if err != nil {
			return fmt.Errorf("init user sync service: %w", err)
		}
	}

	authenticator, err := auth.New(
		apiKeys,
		auth.WithAnonymous(anonymousIssuer),
		auth.WithAccounts(accountIssuer),
	)
	if err != nil {
		return err
	}

	server, err := restapi.New(restapi.Options{
		ConfigFile:                transportConfigPath,
		Agent:                     harness,
		Knowledge:                 knowledgeService,
		Sessions:                  session.NewStore(entClient),
		Approvals:                 approvals,
		Runs:                      run.NewStore(entClient),
		Turns:                     turn.NewStore(entClient),
		Auth:                      authenticator,
		Anonymous:                 anonymousIssuer,
		Accounts:                  accountIssuer,
		AccountStore:              accountStore,
		UserSync:                  userSyncService,
		PasswordHashCost:          cfg.Auth.Accounts.BcryptCost,
		UploadMaxRequestBodyBytes: int64(cfg.Knowledge.MaxUploadRequestBytes),
		UploadMaxDocumentBytes:    int64(cfg.Knowledge.MaxDocumentBytes),
		UploadMaxFiles:            cfg.Knowledge.MaxUploadFiles,
		Logger:                    logger,
		EntClient:                 entClient,
		Metrics:                   metrics,
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

// newRunner builds the tool executor from the API-owned business config.
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

// newAsynqConf maps API-owned connection and publish settings only.
// Consumer concurrency, queue weights and shutdown settings belong to worker.
func newAsynqConf(cfg *config.Config) *asynq.AsynqConf {
	return &asynq.AsynqConf{
		Addr:       cfg.Asynq.Redis.Addr,
		Username:   cfg.Asynq.Redis.Username,
		Pass:       cfg.Asynq.Redis.Password,
		DB:         cfg.Asynq.Redis.DB,
		Enable:     cfg.Asynq.Enabled,
		MaxRetries: cfg.Asynq.MaxRetries,
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
		panic(err)
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
