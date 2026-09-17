package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/queue"
	"eino-quickstart/internal/platform/queue/tasks"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	ragparser "eino-quickstart/internal/rag/parser"
	"eino-quickstart/pkg/convert"

	einodoc "github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// defaultChunkBatchSize 是一次 embedding 送多少段文本。embedding 服务对单请求
// 的条数有上限，分批是硬需求而不是优化。
const defaultChunkBatchSize = 32

// Indexer 消费 knowledge:index 任务，把一篇文档的内容索引进向量库与检索索引。
//
// 它是 worker 进程侧的唯一入口，职责边界很清楚：只做「让这篇文档的索引追上
// 它的内容」，不做校验、不做状态展示 —— 那些是 Service 的事。
//
// 整条链路按下面这条线切开：
//
//	rag.Pipeline        读入 -> 按产品拆分 -> 切块（唯一实现，与 ragserver 共用）
//	Indexer（本文件）   落 pending 行 -> embedding -> 写向量与检索索引 -> 收敛状态
//
// 后一半没有交给 Pipeline：它的落库尾段不认识 document_chunks 的
// pending / indexed 状态，也没有 content_hash 的幂等口径，把状态机搬进 rag
// 会让 RAG 工具包反向依赖 ent 与 dataset / ACL 语义。所以 worker 只借 Pipeline
// 的前半段，持久化与状态一直留在这一层。
type Indexer struct {
	client    *ent.Client
	content   *rag.ContentStore
	pipeline  *rag.Pipeline
	embedder  *rag.Embedder
	vectors   VectorIndex
	keywords  KeywordIndex
	batchSize int
	logger    *slog.Logger
}

// IndexerConfig 是构造 Indexer 的全部依赖。
type IndexerConfig struct {
	Client    *ent.Client
	Content   *rag.ContentStore
	Pipeline  *rag.Pipeline
	Embedder  *rag.Embedder
	Vectors   VectorIndex
	Keyword   KeywordIndex
	BatchSize int
	Logger    *slog.Logger
}

// NewIndexer 组装消费端。pipeline 与 embedder 是必需依赖：没有它们索引这件事
// 根本无法发生，与其等到任务进来才空指针 panic，不如启动时就拒绝。
//
// Keyword 是可选的：es.address 没配时它是 nil，此时分块只进向量库，关键词
// 通道回落到 PostgreSQL 子串匹配 —— 少一半检索能力，但索引链路本身仍然完整。
func NewIndexer(cfg IndexerConfig) (*Indexer, error) {
	if cfg.Client == nil {
		return nil, errors.New("knowledge: ent client is required")
	}
	if cfg.Content == nil {
		return nil, errors.New("knowledge: content store is required")
	}
	if cfg.Pipeline == nil {
		return nil, errors.New("knowledge: rag pipeline is required")
	}
	if cfg.Embedder == nil {
		return nil, errors.New("knowledge: embedder is required")
	}
	if cfg.Vectors == nil {
		return nil, errors.New("knowledge: vector index is required")
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultChunkBatchSize
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Indexer{
		client:    cfg.Client,
		content:   cfg.Content,
		pipeline:  cfg.Pipeline,
		embedder:  cfg.Embedder,
		vectors:   cfg.Vectors,
		keywords:  cfg.Keyword,
		batchSize: batchSize,
		logger:    logger,
	}, nil
}

// HandleTask 是 knowledge:index 的 handler，签名直接满足 queue.TaskHandler。
func (i *Indexer) HandleTask(ctx context.Context, payload []byte) error {
	p, err := tasks.DecodeKnowledgeIndex(payload)
	if err != nil {
		return err
	}
	documentID, err := strconv.ParseUint(strings.TrimSpace(p.DocumentID), 10, 64)
	if err != nil {
		// payload 坏了，重放多少次都一样：让它失败进归档，而不是无限重试。
		return fmt.Errorf("knowledge: invalid document id %q in payload: %w", p.DocumentID, err)
	}
	return i.IndexDocument(ctx, documentID, p.Mode)
}

// IndexDocument 让一篇文档的索引追上它的内容。可重放：重复执行只会空转，
// 不会重复写向量。
//
// mode 决定要不要相信内容指纹：CatchUp（零值）在指纹一致时复用已有分块行；
// Rebuild 一律重新切块，用于显式 reindex —— 解析器或切块配置变了的时候正文
// 是没变的，只看指纹会把这种修复挡在门外，索引里的元数据就永远停在旧形态。
//
// 这是 worker 侧的主 span，也是「HTTP 那条链路」与「向量库那条链路」的接点：
// 它的父 span 是队列投递 span（跨进程传过来的 traceparent），子 span 是读入、
// 切块与每个 embedding 批次。埋点只给 err 起名字，任何 `return ..., err` 都会被
// defer 记进 span。
func (i *Indexer) IndexDocument(ctx context.Context, documentID uint64, mode tasks.IndexMode) (err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.index_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.document_id", int64(documentID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	doc, err := i.client.Document.Query().
		Where(document.IDEQ(documentID)).
		WithDataset().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// 文档在投递之后被删了，任务失去意义，静默成功即可。
			observability.LogWithTrace(ctx, i.logger).Info("knowledge: index task skipped, document is gone",
				slog.Uint64("document_id", documentID),
			)
			return nil
		}
		return fmt.Errorf("knowledge: load document %d: %w", documentID, err)
	}
	if doc.Status == document.StatusDeleted {
		span.SetAttributes(attribute.Bool("knowledge.document_deleted", true))
		return nil
	}
	span.SetAttributes(
		attribute.Int64("knowledge.dataset_id", int64(doc.DatasetID)),
		attribute.String("knowledge.source", doc.Source),
		attribute.String("knowledge.document_status", string(doc.Status)),
	)

	content, err := i.content.Read(doc.Source)
	if err != nil {
		if errors.Is(err, rag.ErrContentNotFound) {
			// 正文没了，重试不会让它回来：落终态当作处理完毕，不触发重试。
			return i.abandon(ctx, documentID, "content is unavailable: "+doc.Source)
		}
		return fmt.Errorf("knowledge: read content of document %d: %w", documentID, err)
	}

	// content_hash 是幂等的关键，同时也是「要不要惊动 Pipeline」的开关：
	// 哈希一致就直接跳到「补还欠着的分块」，重试时不会把上一轮已经切好的
	// 产品块推倒重来。
	hash := contentHash(content)
	span.SetAttributes(attribute.String("knowledge.content_hash", hash))
	if !i.alreadyChunked(ctx, doc, hash) {
		if err := i.rechunk(ctx, doc, hash); err != nil {
			return err
		}
	} else {
		// 这条属性是「重试有没有真的续跑」的直接证据：重试的任务应当命中这里，
		// 而不是每轮都重新切一次块。
		span.SetAttributes(attribute.Bool("knowledge.rechunk_skipped", true))
	}
	return i.embedPending(ctx, doc)
}

// alreadyChunked 报告分块是否已经是这份正文的切块结果。
//
// 两个条件都要满足：哈希一致**并且**分块行确实存在。只看哈希是不够的 ——
// 分块行可能被人工清掉或者被上面的清理逻辑删了一半，那时必须重切。
func (i *Indexer) alreadyChunked(ctx context.Context, doc *ent.Document, hash string) bool {
	stored, _ := doc.Metadata[constant.MetaContentHash].(string)
	if stored != hash {
		return false
	}
	exists, err := i.client.DocumentChunk.Query().
		Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
		Exist(ctx)
	if err != nil {
		// 查不出来就当作需要重切：重切是幂等的，比漏切安全。
		observability.LogWithTrace(ctx, i.logger).Warn("knowledge: check existing chunks failed, will re-chunk",
			slog.Uint64("document_id", doc.ID),
			slog.String("error", err.Error()),
		)
		return false
	}
	return exists
}

// ingest 调用 rag.Pipeline 的 ingest 链，拿到「读入 -> 拆分 -> 切块」的结果。
//
// 传绝对路径：Pipeline 的 FileLoader 会自己再校验一次 root 边界、扩展名与大小，
// 所以组合根必须把 Pipeline 的 DocRoot 和 ContentStore 的 root 指向同一个目录，
// 否则这里会以「路径逃出 doc root」失败。
func (i *Indexer) ingest(ctx context.Context, doc *ent.Document) (_ []*schema.Document, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.ingest_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.document_id", int64(doc.ID)),
			attribute.String("knowledge.source", doc.Source),
			// 这个值是 parser 注册表的查表键：产品拆分到底走了 ProductParser
			// 还是静默退化成 TextParser，全看它。不落到 span 上的话，排查
			// 「为什么 chunk 元数据里没有 product_id」只能靠猜。
			attribute.String("knowledge.dataset_type", doc.Edges.Dataset.Type),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	uri := strings.TrimSpace(doc.Source)
	if uri == "" {
		return nil, fmt.Errorf("knowledge: document %d has no source", doc.ID)
	}
	if !filepath.IsAbs(uri) {
		uri = filepath.Join(i.content.Root(), filepath.FromSlash(uri))
	}
	parsed, err := i.pipeline.IngestFile(ctx, einodoc.Source{URI: uri}, compose.WithLoaderOption(
		einodoc.WithParserOptions(parser.WithExtraMeta(map[string]any{
			"type": doc.Edges.Dataset.Type,
		})),
	))
	if err != nil {
		return nil, fmt.Errorf("knowledge: ingest document %d: %w", doc.ID, err)
	}
	span.SetAttributes(attribute.Int("knowledge.parsed_blocks", len(parsed)))
	return parsed, nil
}

// rechunk 让 Pipeline 重切一次，清掉旧行、写回 pending 行，并记下这次的内容指纹。
func (i *Indexer) rechunk(ctx context.Context, doc *ent.Document, hash string) (err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.rechunk_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.document_id", int64(doc.ID)),
			attribute.String("knowledge.content_hash", hash),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	parsed, err := i.ingest(ctx, doc)
	if err != nil {
		// 读不回来通常是正文格式或权限的问题，重试不会变好，但正文可能被修好
		// 之后再 reindex，所以这里返回错误让队列按策略重试，而不是直接判死。
		return err
	}

	chunks := buildChunks(doc, parsed)
	if len(chunks) == 0 {
		return i.abandon(ctx, doc.ID, "ingest produced no chunks")
	}
	span.SetAttributes(
		attribute.Int("knowledge.parsed_blocks", len(parsed)),
		attribute.Int("knowledge.chunks", len(chunks)),
	)

	// 旧分块行在事务里会被删掉，删掉之后就查不到它们的 ID 了，向量得先记下来。
	oldChunkIDs, err := i.client.DocumentChunk.Query().
		Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
		IDs(ctx)
	if err != nil {
		return fmt.Errorf("knowledge: list old chunks of document %d: %w", doc.ID, err)
	}

	err = entx.WithTx(ctx, i.client, func(tx *ent.Tx) error {
		if _, err := tx.DocumentChunk.Delete().
			Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
			Exec(ctx); err != nil {
			return err
		}
		builders := make([]*ent.DocumentChunkCreate, 0, len(chunks))
		for _, chunk := range chunks {
			builders = append(builders, tx.DocumentChunk.Create().
				SetDocumentID(doc.ID).
				SetChunkIndex(chunk.index).
				SetContent(chunk.content).
				SetHeadingPath(chunk.headingPath).
				SetMetadata(chunk.metadata).
				SetVectorStatus(documentchunk.VectorStatusPending))
		}
		if _, err := tx.DocumentChunk.CreateBulk(builders...).Save(ctx); err != nil {
			return err
		}
		_, err := tx.Document.UpdateOneID(doc.ID).
			SetMetadata(withMeta(doc.Metadata, constant.MetaContentHash, hash)).
			SetStatus(document.StatusIndexing).
			Save(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("knowledge: rewrite chunks of document %d: %w", doc.ID, err)
	}

	// 旧向量与旧检索文档尽力而为地清掉；清不掉只会留垃圾，不影响正确性
	// （检索要回到 chunk 行做过滤）。检索索引这边必须显式删：分块行是删掉
	// 重建的，新分块会拿到新的自增 ID，旧的 _id 再也不会被覆盖。
	i.cleanupVectors(ctx, doc.ID, oldChunkIDs)
	i.cleanupKeywords(ctx, doc.ID)
	observability.LogWithTrace(ctx, i.logger).Info("knowledge: document re-chunked",
		slog.Uint64("document_id", doc.ID),
		slog.Int("parsed", len(parsed)),
		slog.Int("chunks", len(chunks)),
	)
	return nil
}

// pendingChunk 是一条待落库的分块。
type pendingChunk struct {
	index       int
	content     string
	headingPath string
	metadata    map[string]any
}

// buildChunks 把 Pipeline 出来的分块整理成待落库的行。
//
// 分块的语义信息（product_id / model / specs_from_doc / variants、以及
// heading_path、chunk_index）是 Pipeline 在 parse + chunk 阶段写进 MetaData 的，
// 这里只补上「这篇文档属于哪个数据集、谁能看」这几个检索侧必须的键。
func buildChunks(doc *ent.Document, parsed []*schema.Document) []pendingChunk {
	out := make([]pendingChunk, 0, len(parsed))
	index := 0
	for _, chunk := range parsed {
		if chunk == nil || strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		metadata := chunk.MetaData
		if metadata == nil {
			metadata = make(map[string]any, 5)
		}
		// 检索侧靠这些键把命中定位回原文、数据集与权限，每块都要带上。
		metadata[constant.MetaDocID] = doc.ID
		metadata["dataset_id"] = doc.DatasetID
		metadata[constant.MetaVisibility] = string(doc.Visibility)
		metadata[constant.MetaOwner] = doc.OwnerSubject
		// Pipeline 的 loader 会把 parser 写下的 _source 填成它拿到的那个 URI
		// （这里是绝对路径），而回指原文的口径是 documents.source（相对
		// knowledge.root）。统一成后者，别让部署路径泄进检索元数据。
		metadata[ragparser.MetaKeySource] = doc.Source

		out = append(out, pendingChunk{
			index:       index,
			content:     sanitizeText(chunk.Content),
			headingPath: sanitizeText(rag.MetaString(chunk, constant.MetaHeadingPath)),
			metadata:    sanitizeMetadata(metadata),
		})
		index++
	}
	return out
}

// sanitizeText 去掉 PostgreSQL 的文本类型存不下的字符。
//
// 0x00（NUL）在 Postgres 的 text / varchar 与 jsonb 字符串里都是非法的，
// 插进去会让**整批**插入失败：
//
//	pq: invalid byte sequence for encoding "UTF8": 0x00 (22021)
//
// 一份带 NUL 的正文（PDF 转换产物、混了二进制片段的 Markdown 里都很常见）会让
// 这篇文档永远索引不上，而报错来自数据库层，很难联想到「文件内容有问题」。
// 它同时卡在整批上：同一个 bulk insert 里其它正常的分块一起失败。
//
// 直接删掉而不是替换成空格：NUL 本来就是不可见的分隔残留，换个空格只会让分词
// 多出一个空词元，没有意义。
func sanitizeText(value string) string {
	if !strings.ContainsRune(value, 0) {
		return value
	}
	return strings.ReplaceAll(value, "\x00", "")
}

// sanitizeMetadata 递归清掉元数据里字符串值中的 NUL。
//
// 必须递归：装文本的是 specs_from_doc 这类嵌套 map 和 variants 这类数组，只处理
// 顶层会漏掉它们，而 jsonb 同样拒绝 NUL。
//
// 就地改而不是复制：调用方传进来的是本层的临时 map（分块自己的那份由 chunker
// 浅拷贝而来），清洗是幂等的，重复执行结果一致。
func sanitizeMetadata(metadata map[string]any) map[string]any {
	for key, value := range metadata {
		metadata[key] = sanitizeValue(value)
	}
	return metadata
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeText(typed)
	case map[string]any:
		return sanitizeMetadata(typed)
	case []any:
		for i, item := range typed {
			typed[i] = sanitizeValue(item)
		}
		return typed
	case []string:
		for i, item := range typed {
			typed[i] = sanitizeText(item)
		}
		return typed
	default:
		return value
	}
}

// embedPending 反复取还 pending 的分块做 embedding，直到没有为止。
//
// 每轮只挑 pending 是这个设计里最重要的一条：重试因此天然续跑，一个坏段落
// 不会让整篇文档白跑，重放一个已经跑完的任务也只是空转一次。
//
// 整篇文档随参数带下来（而不是只带 ID）：写检索索引要用标题、source、
// visibility、owner 这些文档级字段，而它们必须以 documents 表的现值为准 ——
// 分块行上的 metadata 是入库那一刻的快照，文档改名或转私有之后它已经过期了。
func (i *Indexer) embedPending(ctx context.Context, doc *ent.Document) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		chunks, err := i.client.DocumentChunk.Query().
			Where(
				documentchunk.HasDocumentWith(document.IDEQ(doc.ID)),
				documentchunk.VectorStatusEQ(documentchunk.VectorStatusPending),
			).
			Order(ent.Asc(documentchunk.FieldChunkIndex)).
			Limit(i.batchSize).
			All(ctx)
		if err != nil {
			return fmt.Errorf("knowledge: load pending chunks of document %d: %w", doc.ID, err)
		}
		if len(chunks) == 0 {
			break
		}
		if err := i.embedBatch(ctx, doc, chunks); err != nil {
			return err
		}
	}

	return setDocumentStatus(ctx, i.client, doc.ID, document.StatusReady)
}

// embedBatch 处理一个批次：embedding -> 向量库 upsert -> 检索索引写入 -> 分块标 indexed。
//
// 这个顺序不是随意的，它定义了「半途失败会怎样」：标记 indexed 是最后一步，
// 前面任一步失败，这批分块就还是 pending，队列重试会原样再跑一遍。两个存储
// 的写入都是幂等的（向量按 chunk ID upsert、检索文档以 chunk_id 为 _id 覆盖），
// 所以重跑不会产生重复数据，也不会出现「向量写了、索引没写」的长期不一致。
//
// 一个批次只开一个 span，而不是给 embedding、upsert、索引各开一个：一篇长文档
// 按 batchSize=32 会切出几十个批次，每个批次再套三层 span 会把 trace 淹掉。批次
// span 上的错误信息本身就区分了失败在哪一段（"embed %d chunks" / "upsert %d
// vectors" / "index %d chunks"），要定位到具体阶段够用了。
func (i *Indexer) embedBatch(ctx context.Context, doc *ent.Document, chunks []*ent.DocumentChunk) (err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.embed_batch",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.document_id", int64(doc.ID)),
			attribute.Int("knowledge.batch_size", len(chunks)),
			// 批次在文档里的位置：重试时每轮只挑 pending，靠它能看出这轮从哪续跑。
			attribute.Int("knowledge.first_chunk_index", chunks[0].ChunkIndex),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	texts := make([]string, len(chunks))
	chunkIDs := make([]uint64, len(chunks))
	vectorIDs := make([]int64, len(chunks))
	for k, chunk := range chunks {
		texts[k] = chunk.Content
		chunkIDs[k] = chunk.ID
		vectorIDs[k] = int64(chunk.ID)
	}

	vectors, err := i.embedder.EmbedStrings(ctx, texts)
	if err != nil {
		return i.report(ctx, doc.ID, fmt.Errorf("embed %d chunks: %w", len(texts), err))
	}
	if len(vectors) != len(chunks) {
		return i.report(ctx, doc.ID, fmt.Errorf("embed returned %d vectors for %d chunks", len(vectors), len(chunks)))
	}

	if err := i.vectors.Upsert(ctx, vectorIDs, convert.Float64ToFloat32(vectors)); err != nil {
		return i.report(ctx, doc.ID, fmt.Errorf("upsert %d vectors: %w", len(vectorIDs), err))
	}

	if err := i.indexChunks(ctx, doc, chunks); err != nil {
		return i.report(ctx, doc.ID, fmt.Errorf("index %d chunks: %w", len(chunks), err))
	}

	if _, err := i.client.DocumentChunk.Update().
		Where(documentchunk.IDIn(chunkIDs...)).
		SetVectorStatus(documentchunk.VectorStatusIndexed).
		SetIndexedAt(time.Now()).
		Save(ctx); err != nil {
		return fmt.Errorf("knowledge: mark %d chunks indexed: %w", len(chunkIDs), err)
	}
	return nil
}

// indexChunks 把这一批分块写进检索索引（BM25）。
//
// 没配 ES 时是彻底的空操作，连文档级字段都不用取：这里刻意不做任何降级写入，
// 因为「索引没开」和「索引写失败」必须是两种不同的结果 —— 前者不该让任务失败，
// 后者必须重试。
func (i *Indexer) indexChunks(ctx context.Context, doc *ent.Document, chunks []*ent.DocumentChunk) error {
	if i.keywords == nil {
		return nil
	}
	docs := buildChunkDocs(doc, chunks)
	i.warnIfNoSearchableMetadata(ctx, doc, docs)
	return i.keywords.IndexChunks(ctx, docs)
}

// warnIfNoSearchableMetadata 在一批分块全都没有业务元数据时告警。
//
// 这是「ES 里只有基础字段」这个症状唯一直接的证据：索引里的业务字段全部由映射
// 从元数据取值（见 es.Mapping.Extract），元数据为空就意味着 model / series_name /
// specs_from_doc 一个都不会出现 —— 而写入本身是成功的，没有任何报错。
//
// 两个常见来源，告警里都能一眼区分：
//   - 数据集 type 没对上解析器注册名（只有一个 "product"），退化成 TextParser，
//     正文里的产品块 YAML 头没人解析；
//   - 正文里本来就没有产品块 YAML 头（普通 Markdown 文档走这条路是正常的）。
func (i *Indexer) warnIfNoSearchableMetadata(ctx context.Context, doc *ent.Document, docs []es.ChunkDoc) {
	for _, indexed := range docs {
		if len(indexed.Metadata) > 0 {
			return
		}
	}
	if len(docs) == 0 {
		return
	}
	datasetType := ""
	if doc.Edges.Dataset != nil {
		datasetType = doc.Edges.Dataset.Type
	}
	observability.LogWithTrace(ctx, i.logger).Warn(
		"knowledge: 这批分块没有可检索的业务元数据，检索索引里只会有基础字段",
		slog.Uint64("document_id", doc.ID),
		slog.String("source", doc.Source),
		slog.String("dataset_type", datasetType),
		slog.Int("chunks", len(docs)),
		slog.String("hint",
			"数据集 type 要与解析器注册名一致（产品型录是 product）；"+
				"普通 Markdown 没有产品块 YAML 头时出现这条属正常"),
	)
}

// buildChunkDocs 把分块行整理成检索文档。
//
// 文档级字段（标题、source、可见性、归属）一律取自 ent 实体（documents 表的
// 当前值），不从分块的 metadata 里读：那一列是入库时的快照，文档改名、改可见性、
// 转私有之后就不再正确，而检索侧要拿这些值做引用与权限判断。
//
// 元数据反过来只从 metadata 列读，因为它是**内容的一部分** —— 型号、系列、
// 规格明细描述的是这一块内容本身，不会因为文档改名而过期。
//
// 这里不决定「哪些键变成索引里的哪个字段」：那是映射文件的职责（configs/es/*
// 与 internal/platform/storage/es 的 mapping.go），由 es.Client 在写入时按映射
// 取值。这一层只负责把两层元数据合并、剔掉机制键，然后原样交出去。
func buildChunkDocs(doc *ent.Document, chunks []*ent.DocumentChunk) []es.ChunkDoc {
	now := time.Now()
	docs := make([]es.ChunkDoc, 0, len(chunks))
	for _, chunk := range chunks {
		headingPath := ""
		if chunk.HeadingPath != nil {
			headingPath = *chunk.HeadingPath
		}
		indexed := es.ChunkDoc{
			ChunkID:     strconv.FormatUint(chunk.ID, 10),
			DocumentID:  strconv.FormatUint(doc.ID, 10),
			DatasetID:   strconv.FormatUint(doc.DatasetID, 10),
			ChunkIndex:  chunk.ChunkIndex,
			Source:      doc.Source,
			Title:       doc.Title,
			HeadingPath: headingPath,
			Content:     chunk.Content,
			Visibility:  string(doc.Visibility),
			Owner:       doc.OwnerSubject,
			IndexedAt:   now,
			// 分块那份元数据放在后面：它比文档那份更贴近内容，同名键应当由它
			// 覆盖（上传时带的键是文档级的，产品块的 YAML 头是块级的）。
			Metadata: searchableMetadata(doc.Metadata, chunk.Metadata),
		}
		docs = append(docs, indexed)
	}
	return docs
}

// report 记录一次可重试的失败，并在重试机会用完时落终态。
//
// 无论是否落终态都要把错误原样返回：还有余量时队列会重新领取这个任务，而
// 每轮只挑还 pending 的分块，所以重试天然跳过上一轮已成功的部分。
//
// ctx 被取消说明是进程在退出、任务很快会被重新投递，这时绝不能写终态 ——
// 那会把一次正常的优雅关闭变成用户看到的「索引失败」。
func (i *Indexer) report(ctx context.Context, documentID uint64, cause error) error {
	state := queue.RetryStateFrom(ctx)
	observability.LogWithTrace(ctx, i.logger).Error("knowledge: index document failed",
		slog.Uint64("document_id", documentID),
		slog.Int("attempt", state.Attempt),
		slog.Int("max_retries", state.Max),
		slog.Bool("retries_exhausted", state.Exhausted()),
		slog.String("error", cause.Error()),
	)

	if ctx.Err() == nil && state.Exhausted() {
		i.settleFailed(ctx, documentID)
	}
	return cause
}

// abandon 用于「重试也不会变好」的失败：正文文件不在了、切不出任何产品块。
// 落终态之后返回 nil —— 返回 error 只会让队列把注定失败的任务再跑几遍。
func (i *Indexer) abandon(ctx context.Context, documentID uint64, reason string) error {
	observability.LogWithTrace(ctx, i.logger).Warn("knowledge: abandoning index task",
		slog.Uint64("document_id", documentID),
		slog.String("reason", reason),
	)
	if ctx.Err() != nil {
		return nil
	}
	return i.settleFailed(ctx, documentID)
}

// settleFailed 把剩余待索引的分块与文档本身一起标成 failed，需要人工 reindex。
func (i *Indexer) settleFailed(ctx context.Context, documentID uint64) error {
	if _, err := i.client.DocumentChunk.Update().
		Where(
			documentchunk.HasDocumentWith(document.IDEQ(documentID)),
			documentchunk.VectorStatusEQ(documentchunk.VectorStatusPending),
		).
		SetVectorStatus(documentchunk.VectorStatusFailed).
		Save(ctx); err != nil {
		observability.LogWithTrace(ctx, i.logger).Error("knowledge: mark pending chunks failed",
			slog.Uint64("document_id", documentID),
			slog.String("error", err.Error()),
		)
	}
	return setDocumentStatus(ctx, i.client, documentID, document.StatusFailed)
}

// cleanupVectors 尽力而为地清掉不再需要的向量。
//
// 清不掉只记警告：Milvus 不参与关系库事务，孤儿向量取不回来（检索要回到
// chunk 行做过滤），但会一直占着向量库空间，需要靠后续的全量重建收拾。
func (i *Indexer) cleanupVectors(ctx context.Context, documentID uint64, chunkIDs []uint64) {
	if len(chunkIDs) == 0 || i.vectors == nil {
		return
	}
	ids := make([]int64, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		ids = append(ids, int64(id))
	}
	if err := i.vectors.Delete(ctx, ids); err != nil {
		observability.LogWithTrace(ctx, i.logger).Warn("knowledge: stale vector cleanup failed, orphan vectors remain until a full rebuild",
			slog.Uint64("document_id", documentID),
			slog.Int("chunks", len(ids)),
			slog.String("error", err.Error()),
		)
	}
}

// cleanupKeywords 尽力而为地清掉这篇文档在检索索引里的旧分块。
//
// 和 cleanupVectors 同一套取舍：清不掉只记警告。命中的分块回到 PostgreSQL
// 查不到就会被跳过，正确性不受影响，残留要靠后续全量重建收拾。
func (i *Indexer) cleanupKeywords(ctx context.Context, documentID uint64) {
	if i.keywords == nil {
		return
	}
	deleted, err := i.keywords.DeleteByDocument(ctx, documentID)
	if err != nil {
		observability.LogWithTrace(ctx, i.logger).Warn("knowledge: keyword index cleanup failed, stale documents remain until a full rebuild",
			slog.Uint64("document_id", documentID),
			slog.String("error", err.Error()),
		)
		return
	}
	if deleted > 0 {
		observability.LogWithTrace(ctx, i.logger).Info("knowledge: stale keyword documents removed",
			slog.Uint64("document_id", documentID),
			slog.Int64("deleted", deleted),
		)
	}
}

// contentHash 是正文的内容指纹，存在 document.metadata 上用于幂等判断。
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// withMeta 浅拷贝一份元数据并写入一个键，避免就地修改 ent 持有的 map。
func withMeta(metadata map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(metadata)+1)
	for k, v := range metadata {
		out[k] = v
	}
	out[key] = value
	return out
}
