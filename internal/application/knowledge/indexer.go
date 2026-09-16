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
	"eino-quickstart/internal/platform/queue"
	"eino-quickstart/internal/platform/queue/tasks"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	ragparser "eino-quickstart/internal/rag/parser"
	"eino-quickstart/pkg/convert"

	einodoc "github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// defaultChunkBatchSize 是一次 embedding 送多少段文本。embedding 服务对单请求
// 的条数有上限，分批是硬需求而不是优化。
const defaultChunkBatchSize = 32

// Indexer 消费 knowledge:index 任务，把一篇文档的内容索引进向量库。
//
// 它是 worker 进程侧的唯一入口，职责边界很清楚：只做「让这篇文档的索引追上
// 它的内容」，不做校验、不做状态展示 —— 那些是 Service 的事。
//
// 整条链路按下面这条线切开：
//
//	rag.Pipeline        读入 -> 按产品拆分 -> 切块（唯一实现，与 ragserver 共用）
//	Indexer（本文件）   落 pending 行 -> embedding -> 写向量 -> 收敛状态
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
	BatchSize int
	Logger    *slog.Logger
}

// NewIndexer 组装消费端。pipeline 与 embedder 是必需依赖：没有它们索引这件事
// 根本无法发生，与其等到任务进来才空指针 panic，不如启动时就拒绝。
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
	return i.IndexDocument(ctx, documentID)
}

// IndexDocument 让一篇文档的索引追上它的内容。可重放：重复执行只会空转，
// 不会重复写向量。
func (i *Indexer) IndexDocument(ctx context.Context, documentID uint64) error {
	doc, err := i.client.Document.Query().
		Where(document.IDEQ(documentID)).
		WithDataset().
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// 文档在投递之后被删了，任务失去意义，静默成功即可。
			i.logger.Info("knowledge: index task skipped, document is gone",
				slog.Uint64("document_id", documentID),
			)
			return nil
		}
		return fmt.Errorf("knowledge: load document %d: %w", documentID, err)
	}
	if doc.Status == document.StatusDeleted {
		return nil
	}

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
	if !i.alreadyChunked(ctx, doc, hash) {
		if err := i.rechunk(ctx, doc, hash); err != nil {
			return err
		}
	}
	return i.embedPending(ctx, documentID)
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
		i.logger.Warn("knowledge: check existing chunks failed, will re-chunk",
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
func (i *Indexer) ingest(ctx context.Context, doc *ent.Document) ([]*schema.Document, error) {
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
	return parsed, nil
}

// rechunk 让 Pipeline 重切一次，清掉旧行、写回 pending 行，并记下这次的内容指纹。
func (i *Indexer) rechunk(ctx context.Context, doc *ent.Document, hash string) error {
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

	// 旧向量尽力而为地清掉；清不掉只会留垃圾，不影响正确性（检索要回到
	// chunk 行做过滤）。
	i.cleanupVectors(ctx, doc.ID, oldChunkIDs)
	i.logger.Info("knowledge: document re-chunked",
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
			content:     chunk.Content,
			headingPath: rag.MetaString(chunk, constant.MetaHeadingPath),
			metadata:    metadata,
		})
		index++
	}
	return out
}

// embedPending 反复取还 pending 的分块做 embedding，直到没有为止。
//
// 每轮只挑 pending 是这个设计里最重要的一条：重试因此天然续跑，一个坏段落
// 不会让整篇文档白跑，重放一个已经跑完的任务也只是空转一次。
func (i *Indexer) embedPending(ctx context.Context, documentID uint64) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		chunks, err := i.client.DocumentChunk.Query().
			Where(
				documentchunk.HasDocumentWith(document.IDEQ(documentID)),
				documentchunk.VectorStatusEQ(documentchunk.VectorStatusPending),
			).
			Order(ent.Asc(documentchunk.FieldChunkIndex)).
			Limit(i.batchSize).
			All(ctx)
		if err != nil {
			return fmt.Errorf("knowledge: load pending chunks of document %d: %w", documentID, err)
		}
		if len(chunks) == 0 {
			break
		}
		if err := i.embedBatch(ctx, documentID, chunks); err != nil {
			return err
		}
	}

	return setDocumentStatus(ctx, i.client, documentID, document.StatusReady)
}

func (i *Indexer) embedBatch(ctx context.Context, documentID uint64, chunks []*ent.DocumentChunk) error {
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
		return i.report(ctx, documentID, fmt.Errorf("embed %d chunks: %w", len(texts), err))
	}
	if len(vectors) != len(chunks) {
		return i.report(ctx, documentID, fmt.Errorf("embed returned %d vectors for %d chunks", len(vectors), len(chunks)))
	}

	if err := i.vectors.Upsert(ctx, vectorIDs, convert.Float64ToFloat32(vectors)); err != nil {
		return i.report(ctx, documentID, fmt.Errorf("upsert %d vectors: %w", len(vectorIDs), err))
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

// report 记录一次可重试的失败，并在重试机会用完时落终态。
//
// 无论是否落终态都要把错误原样返回：还有余量时队列会重新领取这个任务，而
// 每轮只挑还 pending 的分块，所以重试天然跳过上一轮已成功的部分。
//
// ctx 被取消说明是进程在退出、任务很快会被重新投递，这时绝不能写终态 ——
// 那会把一次正常的优雅关闭变成用户看到的「索引失败」。
func (i *Indexer) report(ctx context.Context, documentID uint64, cause error) error {
	state := queue.RetryStateFrom(ctx)
	i.logger.Error("knowledge: index document failed",
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
	i.logger.Warn("knowledge: abandoning index task",
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
		i.logger.Error("knowledge: mark pending chunks failed",
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
		i.logger.Warn("knowledge: stale vector cleanup failed, orphan vectors remain until a full rebuild",
			slog.Uint64("document_id", documentID),
			slog.Int("chunks", len(ids)),
			slog.String("error", err.Error()),
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
