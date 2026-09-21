package main

import (
	"context"
	"log/slog"
	"strconv"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	"eino-quickstart/internal/rag/store/milvus"
	"eino-quickstart/internal/rag/store/postgres"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// newRetrievalSearcher 给 HTTP 进程装配召回能力。
//
// 装配是**分级**的：缺一个依赖只降一级，不会整体不可用。这个取舍是刻意的 ——
// 召回有两条不依赖 embedding 服务的通道（精确 / 关键词），它们是「按型号找资料」
// 这类查询的主力。让向量库或 embedding 的可用性决定「能不能按型号搜到东西」，
// 把本来能答的问题一起拒了。
//
//	Milvus 连不上   只关掉向量通道（仍可按型号与关键词召回）
//	ES 连不上       只关掉精确通道，关键词通道回落 PostgreSQL 子串匹配
//	两者都不可用    只剩关键词通道（子串匹配），仍能答型号查询
//
// 返回 nil 表示这个进程完全没有召回能力，此时用例层返回 ErrSearchUnavailable，
// 传输层映射成 503 —— 明确说「这里没有装配检索」，而不是让调用方把空结果读成
// 「没有相关内容」。
func newRetrievalSearcher(ctx context.Context, cfg *config.Config, entClient *ent.Client, logger *slog.Logger) knowledge.Searcher {
	if entClient == nil {
		return nil
	}

	keywordSearcher := newKeywordSearcher(ctx, cfg, logger)
	vectorStore := newVectorStore(ctx, cfg, logger)

	policy := rag.PolicyFromConfig(cfg.Retrieval)
	if vectorStore == nil && policy.VectorWeight > 0 {
		// 起不来就整条通道关掉，而不是留着它每次请求都报错：那会把每一次响应的
		// degraded 都填上 vector，那条信息就不再指示任何异常了。
		logger.Warn("向量库不可用，本次运行关闭向量通道（仍可按型号与关键词召回）")
		policy.VectorWeight = 0
	}

	// embedder 可能是 nil（没配密钥或客户端构造失败），此时检索器会自己把向量
	// 通道关掉 —— 那条规则只写在 NewHybridRetriever 里，不在组合根再判一次。
	hybrid, err := rag.NewHybridRetriever(rag.HybridConfig{
		Store: &rag.Store{
			PgStore: postgres.NewPostgres(entClient),
			// 注意用接口类型接住：把 nil 的 *MilvusStore 塞进接口字段会得到一个
			// 非 nil 的接口，判空就失效了，而调用它会打到 nil 接收者上。
			MilvusStore: vectorStore,
			Keyword:     keywordSearcher,
		},
		Embedder:  newQueryEmbedder(ctx, cfg, logger),
		Retrieval: policy,
		TopK:      cfg.Knowledge.DefaultTopK,
		Logger:    logger,
	})
	if err != nil {
		logger.Error("装配召回失败，检索接口将不可用", slog.String("error", err.Error()))
		return nil
	}
	return hybridSearcher{retriever: hybrid}
}

// newKeywordSearcher 给 HTTP 进程准备一个检索索引（ES）查询句柄。
//
// 连不上就返回 nil，关键词通道随回落 PostgreSQL 子串匹配 —— 能力弱一档（没有
// 词频、IDF 与长度归一化），但「按型号搜得到」这件事不依赖 ES。
//
// 这里**不建索引**：建索引与分词器校验属于索引链路的启动步骤（cmd/worker），
// 放在 HTTP 进程里会让两个进程对索引形态各有说法。
func newKeywordSearcher(ctx context.Context, cfg *config.Config, logger *slog.Logger) es.Searcher {
	client, err := es.New(&cfg.ES)
	if err != nil {
		logger.Warn("检索索引不可用，关键词通道回落到子串匹配，精确通道关闭",
			slog.String("error", err.Error()))
		return nil
	}
	if client == nil {
		// 没配 es.address：这是合法配置，不是异常，不用告警。
		return nil
	}
	if err := client.Health(ctx); err != nil {
		logger.Warn("检索索引不可用，关键词通道回落到子串匹配，精确通道关闭",
			slog.String("error", err.Error()))
		return nil
	}
	return client.Searcher()
}

// newVectorStore 给 HTTP 进程准备一个向量库句柄。
//
// 返回 milvus.Store 接口而不是具体类型，nil 时就是真 nil（见 newRetrievalSearcher
// 里那段关于 typed-nil 的说明）。
func newVectorStore(ctx context.Context, cfg *config.Config, logger *slog.Logger) milvus.Store {
	store, err := milvus.NewMilvusStore(ctx, &milvus.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions,
		MetricType: cfg.Milvus.MetricType,
	})
	if err != nil {
		logger.Warn("向量库不可用", slog.String("error", err.Error()))
		return nil
	}
	return store
}

// newQueryEmbedder 准备查询向量化用的客户端。
//
// 没配密钥时返回 nil：查询向量算不出来，向量通道就没有意义。这时仍然按关键词
// 与精确通道召回 —— 那是两条不依赖外部模型的通道，也是型号类查询的主力。
func newQueryEmbedder(ctx context.Context, cfg *config.Config, logger *slog.Logger) *rag.Embedder {
	apiKey := envOr(cfg.Embedding.APIKeyEnv, "")
	if apiKey == "" {
		logger.Warn("未配置 embedding 密钥，向量通道关闭（仍可按型号与关键词召回）",
			slog.String("env", cfg.Embedding.APIKeyEnv))
		return nil
	}
	embedder, err := rag.NewEmbedder(ctx, &openai.EmbeddingConfig{
		APIKey:     apiKey,
		BaseURL:    cfg.Embedding.BaseURL,
		Model:      cfg.Embedding.Model,
		Dimensions: &cfg.Embedding.Dimensions,
	}, rag.WithMaxTextsPerRequest(cfg.Embedding.BatchSize))
	if err != nil {
		// 构造都没有客户端返回时是配置问题（模型名、地址），不是网络抖动：
		// 重试没用，所以按「不可用」处理并说清楚。
		logger.Warn("初始化 embedding 客户端失败，向量通道关闭",
			slog.String("error", err.Error()))
		return nil
	}
	return embedder
}

// detailedRetriever 是 hybridSearcher 需要的最小检索能力。
//
// 声明成接口而不是直接依赖 *rag.HybridRetriever：适配器只做「范围翻译 + 结果摊平」，
// 它不需要知道融合算法。收窄之后，「DatasetID 有没有被丢掉」「TopK 有没有传下去」
// 这两条映射才能用一个假实现钉住 —— 依赖具体类型时，为了一行赋值得先搭出向量库、
// ES 与数据库，于是最该被守住的那一行反而没人测。
type detailedRetriever interface {
	RetrieveDetailed(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, rag.Report, error)
}

// hybridSearcher 把 rag 的混合检索器适配成用例层的召回端口。
//
// 适配只做两件事：把范围条件翻译成检索器的过滤条件，把结果摊平成用例层的形状。
// 通道台账（哪条出力、哪条降级）原样带上去 —— 它是「结果为什么这么少」唯一的
// 现场证据，压在适配器里就只剩日志能看了。
type hybridSearcher struct {
	retriever detailedRetriever
}

func (s hybridSearcher) Search(ctx context.Context, query string, scope knowledge.SearchScope) (knowledge.SearchOutcome, error) {
	docs, report, err := s.retriever.RetrieveDetailed(ctx, query,
		retriever.WithTopK(scope.TopK),
		rag.WithFilter(rag.Filter{DatasetID: scope.DatasetID}),
	)
	if err != nil {
		return knowledge.SearchOutcome{}, err
	}

	outcome := knowledge.SearchOutcome{
		Hits:     make([]knowledge.SearchHit, 0, len(docs)),
		Channels: report.Used(),
		Degraded: report.Degraded(),
	}
	for _, doc := range docs {
		outcome.Hits = append(outcome.Hits, knowledge.SearchHit{
			ChunkID:     parseUint64(doc.ID),
			DocumentID:  metaUint64(doc, constant.MetaDocID),
			Source:      rag.MetaString(doc, constant.MetaSource),
			Title:       rag.MetaString(doc, constant.MetaTitle),
			HeadingPath: rag.MetaString(doc, constant.MetaHeadingPath),
			Content:     doc.Content,
			Score:       doc.Score(),
		})
	}
	return outcome, nil
}

// parseUint64 解析检索结果里的 ID。
//
// 解析不了就返回 0：ID 不是客户端能修的输入，为它让整次检索失败不值得 ——
// 一条拿不到 ID 的命中仍然能把正文给出去。
func parseUint64(raw string) uint64 {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// metaUint64 取元数据里的无符号整数。
//
// 要认这么多类型是因为元数据有两条来路：检索侧重建元数据时写进去的是 uint64，
// 而经过 JSON 列往返的可能变成 float64 或 string。少认一种，document_id 就会
// 静默变成 0 —— 而 0 在客户端看起来只是「这个字段没填」，不会有人去查。
func metaUint64(doc *schema.Document, key string) uint64 {
	if doc == nil || doc.MetaData == nil {
		return 0
	}
	switch value := doc.MetaData[key].(type) {
	case uint64:
		return value
	case uint:
		return uint64(value)
	case int64:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case int:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case float64:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case string:
		return parseUint64(value)
	default:
		return 0
	}
}

// 编译期断言：适配器必须满足用例层声明的端口。
var _ knowledge.Searcher = hybridSearcher{}
