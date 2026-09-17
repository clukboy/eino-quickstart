package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"eino-quickstart/ent"
	"eino-quickstart/ent/dataset"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// IndexTaskQueue 是应用层定义的窄端口：它只说「把这篇文档排进索引队列」，
// 队列名、payload 编码、重试次数都由适配器决定。Service 只依赖这个接口，
// 因此 import 不到 asynq；换 MQ 时改的是适配器，不是用例。
//
// 之所以要在用例层定义而不是在传输层：投递是写文档这件事的一部分 ——
// 事务提交后必须投出去，投不出去整个写入就不算成功。把端口放在这里，
// transport 只表达「重建这篇文档的索引」，不感知队列存在。
type IndexTaskQueue interface {
	EnqueueIndex(ctx context.Context, datasetID, documentID uint64) error
}

// VectorIndex 是向量库在应用层这边的视图。
//
// 接口只暴露两个动作：写入（worker 索引用）和删除（删除文档时清理）。检索
// 不走这里 —— 那是 rag.Retriever 的职责。
type VectorIndex interface {
	Upsert(ctx context.Context, chunkIDs []int64, vectors [][]float32) error
	Delete(ctx context.Context, chunkIDs []int64) error
}

// KeywordIndex 是关键词检索索引（Elasticsearch / BM25）在应用层这边的视图。
//
// 和 VectorIndex 一样只暴露写入与清理：检索不走这里（那是 rag.Store 的职责），
// 索引的形态与查询 DSL 也不该从用例层透出去。delete 按 document_id 而不是按
// 分块 ID —— 重新切块时旧分块行先被删掉，那时已经拿不到它们的 ID 了。
//
// nil 表示 es.address 没配：此时分块不写索引，关键词通道回落到 PostgreSQL
// 子串匹配，功能不缺，只是少了词频、IDF 与长度归一化。
type KeywordIndex interface {
	IndexChunks(ctx context.Context, docs []es.ChunkDoc) error
	DeleteByDocument(ctx context.Context, documentID uint64) (int64, error)
}

// ChunkStat 是一篇文档的分块计数。
//
// ent 的 Document 上没有 chunk_count / indexed_chunk_count 字段，这两个数是从
// document_chunks 实时聚合出来的：切块在 worker 内完成，请求返回时根本没有
// 一个可以落库的准确值，与其维护一个会漂的冗余列，不如查的时候算。
type ChunkStat struct {
	Total   int
	Indexed int
}

// Service 是知识库用例的入口。
type Service struct {
	client   *ent.Client
	content  *rag.ContentStore
	queue    IndexTaskQueue
	vectors  VectorIndex
	keywords KeywordIndex
	logger   *slog.Logger
}

// NewService 组装用例层。
//
// vectors 与 keywords 都可以是 nil：删除路径上的清理由它们承担，而清理是
// 尽力而为的 —— 孤儿向量与孤儿文档取不回来（检索要回到 chunk 行做过滤），
// 只是白占空间，靠后续全量重建收拾。所以外部存储没配或连不上都不该挡住
// 「删文档」这件事本身。
func NewService(
	client *ent.Client,
	content *rag.ContentStore,
	queue IndexTaskQueue,
	vectors VectorIndex,
	keywords KeywordIndex,
	logger *slog.Logger,
) (*Service, error) {
	if client == nil {
		return nil, errors.New("knowledge: ent client is required")
	}
	if content == nil {
		return nil, errors.New("knowledge: content store is required")
	}
	if queue == nil {
		return nil, errors.New("knowledge: index queue is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		client:   client,
		content:  content,
		queue:    queue,
		vectors:  vectors,
		keywords: keywords,
		logger:   logger,
	}, nil
}

// CreateInput 是新建文档的输入。
//
// Content 与 Source 二选一：
//   - Content 非空 -> 正文写进托管目录（Source 若填写只能指向托管目录内的位置）
//   - Content 为空 -> 注册 Source 指向的既有文件，Title 可省略（从文件名推断）
type CreateInput struct {
	DatasetID    uint64
	Title        string
	Content      string
	Source       string
	Visibility   string
	OwnerSubject string
	Metadata     map[string]string
}

// Create 落正文、建文档行、投递索引任务。
//
// 注意这里**不切块**：文件里到底有几个产品、每篇多长，要等 worker 内联的
// parse 之后才知道，请求内算不出来。所以文档行建出来时计数是 0、状态是
// indexing，客户端要轮询 GET 等它变 ready。
//
// 埋点只给 err 起名字、结果位留空。这样任何一处 `return ..., err` 都会被下面
// 这个 defer 记进 span，不用在每个错误返回点手写一遍（手写一定会漏，而漏掉的
// 往往就是最需要看见的那条路径）。反过来如果连结果也起名，函数体里原本的
// `base, err := ...` 会因为「:= 左侧没有新变量」而编译不过。
func (s *Service) Create(ctx context.Context, in CreateInput) (_ *ent.Document, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.create_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.dataset_id", int64(in.DatasetID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	base, err := s.dataset(ctx, in.DatasetID)
	if err != nil {
		return nil, err
	}

	visibility, err := parseVisibility(in.Visibility)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(in.Title)
	source := strings.TrimSpace(in.Source)

	switch {
	case strings.TrimSpace(in.Content) != "":
		if title == "" {
			return nil, invalid("title is required when content is provided")
		}
		if source == "" {
			// 文件名从标题派生，托管目录按数据集分片。
			source, err = s.content.Create(in.DatasetID, title, in.Content)
			if err != nil {
				return nil, mapContentError(err)
			}
			break
		}
		// 显式指定了落点：只允许写在托管目录内，注册进来的外部文件不许覆盖。
		if !s.content.Managed(source) {
			return nil, ErrContentNotManaged
		}
		if err := s.content.Write(source, in.Content); err != nil {
			return nil, mapContentError(err)
		}
	default:
		if source == "" {
			return nil, invalid("either content or source is required")
		}
		if !s.content.Exists(source) {
			return nil, ErrContentUnavailable
		}
		if title == "" {
			title = titleFromSource(source)
		}
	}
	if title == "" {
		title = "document"
	}

	owner := strings.TrimSpace(in.OwnerSubject)
	if owner == "" {
		owner = base.OwnerSubject
	}

	created, err := s.client.Document.Create().
		SetDatasetID(in.DatasetID).
		SetSource(source).
		SetTitle(title).
		SetMetadata(mergeMetadata(in.Metadata, source, title)).
		SetVisibility(visibility).
		SetOwnerSubject(owner).
		SetStatus(document.StatusIndexing).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("knowledge: create document: %w", err)
	}

	if err := s.enqueue(ctx, created, in.DatasetID); err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.Int64("knowledge.document_id", int64(created.ID)),
		attribute.String("knowledge.source", created.Source),
		attribute.String("knowledge.title", created.Title),
	)
	return created, nil
}

// UpdateInput 是更新文档的输入。零值字段表示「不改」。
type UpdateInput struct {
	DatasetID  uint64
	DocumentID uint64
	Title      string
	Content    string
	Visibility string
	Metadata   map[string]string
}

// Update 改元信息；给了 content 就改写正文并重新排队索引。
//
// 只有托管目录内的正文允许被覆盖：注册进来的外部文件是调用方的资产，
// 接口不该无声改写。
func (s *Service) Update(ctx context.Context, in UpdateInput) (_ *ent.Document, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.update_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.dataset_id", int64(in.DatasetID)),
			attribute.Int64("knowledge.document_id", int64(in.DocumentID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	doc, err := s.document(ctx, in.DatasetID, in.DocumentID)
	if err != nil {
		return nil, err
	}

	update := s.client.Document.UpdateOneID(doc.ID)
	reindex := false

	if title := strings.TrimSpace(in.Title); title != "" {
		update.SetTitle(title)
	}
	if in.Visibility != "" {
		visibility, err := parseVisibility(in.Visibility)
		if err != nil {
			return nil, err
		}
		update.SetVisibility(visibility)
	}
	if len(in.Metadata) > 0 {
		update.SetMetadata(mergeMetadata(in.Metadata, doc.Source, doc.Title))
	}
	if strings.TrimSpace(in.Content) != "" {
		if !s.content.Managed(doc.Source) {
			return nil, ErrContentNotManaged
		}
		if err := s.content.Write(doc.Source, in.Content); err != nil {
			return nil, mapContentError(err)
		}
		update.SetStatus(document.StatusIndexing)
		reindex = true
	}

	saved, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("knowledge: update document: %w", err)
	}

	if reindex {
		if err := s.enqueue(ctx, saved, in.DatasetID); err != nil {
			return nil, err
		}
	}
	span.SetAttributes(attribute.Bool("knowledge.reindex_queued", reindex))
	return saved, nil
}

// Reindex 重新读正文、重新排队。用途是修复：改了切块参数、换了 embedding
// 模型、或者上一次索引失败之后，都按新配置重建。
func (s *Service) Reindex(ctx context.Context, datasetID, documentID uint64) (_ *ent.Document, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.reindex_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.dataset_id", int64(datasetID)),
			attribute.Int64("knowledge.document_id", int64(documentID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	doc, err := s.document(ctx, datasetID, documentID)
	if err != nil {
		return nil, err
	}
	if !s.content.Exists(doc.Source) {
		return nil, ErrContentUnavailable
	}

	saved, err := s.client.Document.UpdateOneID(doc.ID).
		SetStatus(document.StatusIndexing).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("knowledge: reindex document: %w", err)
	}

	if err := s.enqueue(ctx, saved, datasetID); err != nil {
		return nil, err
	}
	return saved, nil
}

// ReindexResult 是整库重建的受理结果。Chunks 恒为 0：切块在 worker 内完成，
// 投递时还不知道会出多少块。
type ReindexResult struct {
	Documents int
	Failed    int
}

// ReindexDataset 逐个文档重建，互不影响：某个文档的正文丢了只计入 failed，
// 其余照常排队。
func (s *Service) ReindexDataset(ctx context.Context, datasetID uint64) (_ *ReindexResult, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.reindex_dataset",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.dataset_id", int64(datasetID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	if _, err := s.dataset(ctx, datasetID); err != nil {
		return nil, err
	}

	docs, err := s.client.Document.Query().
		Where(
			document.DatasetIDEQ(datasetID),
			document.StatusNEQ(document.StatusDeleted),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list documents for reindex: %w", err)
	}

	result := &ReindexResult{}
	for _, doc := range docs {
		if !s.content.Exists(doc.Source) {
			// 正文读不回来，重排也是白排：直接落成 failed，让调用方看得见。
			if err := setDocumentStatus(ctx, s.client, doc.ID, document.StatusFailed); err != nil {
				return nil, err
			}
			result.Failed++
			continue
		}

		saved, err := s.client.Document.UpdateOneID(doc.ID).
			SetStatus(document.StatusIndexing).
			Save(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("knowledge: reindex dataset: %w", err)
		}
		if err := s.enqueue(ctx, saved, datasetID); err != nil {
			// enqueue 内部已经把文档标成 failed，这里只计数不中断整批。
			observability.LogWithTrace(ctx, s.logger).Warn("reindex dataset: enqueue failed",
				slog.Uint64("dataset_id", datasetID),
				slog.Uint64("document_id", doc.ID),
				slog.String("error", err.Error()),
			)
			result.Failed++
			continue
		}
		result.Documents++
	}
	// 整批的规模要有痕迹：批量重建是「投了一批、失败几个」的形态，只靠单文档
	// 的 span 看不出批次全貌。
	span.SetAttributes(
		attribute.Int("knowledge.documents_queued", result.Documents),
		attribute.Int("knowledge.documents_failed", result.Failed),
	)
	return result, nil
}

// Delete 摘掉分块与文档行，然后尽力而为地清理向量与托管正文。
//
// 关系库的部分在事务里完成；Milvus 不参与这个事务，清理失败只记警告 ——
// 孤儿向量取不回来（检索要回到 chunk 行做过滤），但会一直占着向量库空间，
// 需要靠后续的全量重建收拾。
func (s *Service) Delete(ctx context.Context, datasetID, documentID uint64) (err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.delete_document",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.dataset_id", int64(datasetID)),
			attribute.Int64("knowledge.document_id", int64(documentID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	doc, err := s.document(ctx, datasetID, documentID)
	if err != nil {
		return err
	}

	// 先记下分块 ID，删行之后就找不到它们了。
	chunkIDs, err := s.client.DocumentChunk.Query().
		Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
		IDs(ctx)
	if err != nil {
		return fmt.Errorf("knowledge: list chunks for delete: %w", err)
	}

	err = entx.WithTx(ctx, s.client, func(tx *ent.Tx) error {
		if _, err := tx.DocumentChunk.Delete().
			Where(documentchunk.HasDocumentWith(document.IDEQ(doc.ID))).
			Exec(ctx); err != nil {
			return err
		}
		_, err := tx.Document.Delete().Where(document.IDEQ(doc.ID)).Exec(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("knowledge: delete document: %w", err)
	}

	s.cleanupVectors(ctx, doc.ID, chunkIDs)
	s.cleanupKeywordIndex(ctx, doc.ID)
	if err := s.content.Remove(doc.Source); err != nil {
		observability.LogWithTrace(ctx, s.logger).Warn("knowledge: remove content file failed",
			slog.Uint64("document_id", doc.ID),
			slog.String("source", doc.Source),
			slog.String("error", err.Error()),
		)
	}
	return nil
}

// Get 取一篇文档，顺带校验它确实属于这个数据集。
func (s *Service) Get(ctx context.Context, datasetID, documentID uint64) (*ent.Document, error) {
	return s.document(ctx, datasetID, documentID)
}

// List 列出一个数据集下的文档，最新的在前。
func (s *Service) List(ctx context.Context, datasetID uint64) ([]*ent.Document, error) {
	docs, err := s.client.Document.Query().
		Where(document.DatasetIDEQ(datasetID)).
		Order(ent.Desc(document.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list documents: %w", err)
	}
	return docs, nil
}

// ChunkStats 批量取分块计数。
//
// 两次分组查询覆盖一页文档，往返次数不随页大小增长：一次取总数，一次只取
// vector_status=indexed 的数。缺省的文档计数为 0（没有分块，或者还没切）。
func (s *Service) ChunkStats(ctx context.Context, documentIDs []uint64) (map[uint64]ChunkStat, error) {
	stats := make(map[uint64]ChunkStat, len(documentIDs))
	if len(documentIDs) == 0 {
		return stats, nil
	}

	totals, err := s.countChunks(ctx, documentIDs, "")
	if err != nil {
		return nil, err
	}
	indexed, err := s.countChunks(ctx, documentIDs, documentchunk.VectorStatusIndexed)
	if err != nil {
		return nil, err
	}

	for _, id := range documentIDs {
		stats[id] = ChunkStat{Total: totals[id], Indexed: indexed[id]}
	}
	return stats, nil
}

// chunkCountRow 直接对齐 SQL 返回的列名：分组列是外键列 document_chunks
// （schema 没把 document_id 声明成独立字段），聚合列是 COUNT(*) 的默认名 count。
type chunkCountRow struct {
	DocumentID uint64 `sql:"document_chunks"`
	Count      int    `sql:"count"`
}

func (s *Service) countChunks(
	ctx context.Context,
	documentIDs []uint64,
	status documentchunk.VectorStatus,
) (map[uint64]int, error) {
	query := s.client.DocumentChunk.Query().
		Where(documentchunk.HasDocumentWith(document.IDIn(documentIDs...)))
	if status != "" {
		query = query.Where(documentchunk.VectorStatusEQ(status))
	}

	var rows []chunkCountRow
	if err := query.
		GroupBy(documentchunk.DocumentColumn).
		Aggregate(ent.Count()).
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("knowledge: count chunks: %w", err)
	}

	counts := make(map[uint64]int, len(rows))
	for _, row := range rows {
		counts[row.DocumentID] = row.Count
	}
	return counts, nil
}

// enqueue 投递索引任务，把「投不出去」变成请求错误。
//
// 失败时把文档标成 failed 再返回错误，而不是回滚删除：正文文件可能本来就在
// 托管目录里（更新场景），删掉等于替调用方决定文件去留。标 failed 是更保守
// 的选择 —— 文档在列表里可见、状态诚实，客户端拿到 5xx，之后可以显式 reindex。
func (s *Service) enqueue(ctx context.Context, doc *ent.Document, datasetID uint64) error {
	if err := s.queue.EnqueueIndex(ctx, datasetID, doc.ID); err != nil {
		observability.LogWithTrace(ctx, s.logger).Error("knowledge: enqueue index task failed",
			slog.Uint64("dataset_id", datasetID),
			slog.Uint64("document_id", doc.ID),
			slog.String("error", err.Error()),
		)
		if markErr := setDocumentStatus(ctx, s.client, doc.ID, document.StatusFailed); markErr != nil {
			observability.LogWithTrace(ctx, s.logger).Error("knowledge: mark document failed after enqueue error",
				slog.Uint64("document_id", doc.ID),
				slog.String("error", markErr.Error()),
			)
		} else {
			doc.Status = document.StatusFailed
		}
		return fmt.Errorf("%w: %v", ErrQueueUnavailable, err)
	}
	return nil
}

// setDocumentStatus 改状态时绕开已删除的文档，避免把一个已经被删掉的行
// 「复活」成 failed。用条件更新而不是 UpdateOneID：文档可能在索引途中被删，
// 那时这里应该安静地什么都不做。
func setDocumentStatus(ctx context.Context, client *ent.Client, id uint64, status document.Status) error {
	_, err := client.Document.Update().
		Where(document.IDEQ(id), document.StatusNEQ(document.StatusDeleted)).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("knowledge: mark document %d as %s: %w", id, status, err)
	}
	return nil
}

func (s *Service) dataset(ctx context.Context, datasetID uint64) (*ent.Dataset, error) {
	base, err := s.client.Dataset.Query().
		Where(dataset.IDEQ(datasetID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDatasetNotFound
		}
		return nil, fmt.Errorf("knowledge: load dataset: %w", err)
	}
	return base, nil
}

// document 取文档并校验归属：路径里的 :id 与 :docId 必须指向同一份资源，
// 免得每个用法各自记得比较 dataset_id。
func (s *Service) document(ctx context.Context, datasetID, documentID uint64) (*ent.Document, error) {
	doc, err := s.client.Document.Query().
		Where(
			document.IDEQ(documentID),
			document.DatasetIDEQ(datasetID),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("knowledge: load document: %w", err)
	}
	return doc, nil
}

func (s *Service) cleanupVectors(ctx context.Context, documentID uint64, chunkIDs []uint64) {
	if len(chunkIDs) == 0 {
		return
	}
	if s.vectors == nil {
		observability.LogWithTrace(ctx, s.logger).Warn("knowledge: vector cleanup skipped, no vector index configured",
			slog.Uint64("document_id", documentID),
			slog.Int("chunks", len(chunkIDs)),
		)
		return
	}
	ids := make([]int64, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		ids = append(ids, int64(id))
	}
	if err := s.vectors.Delete(ctx, ids); err != nil {
		observability.LogWithTrace(ctx, s.logger).Warn("knowledge: vector cleanup failed, orphan vectors remain until a full rebuild",
			slog.Uint64("document_id", documentID),
			slog.Int("chunks", len(ids)),
			slog.String("error", err.Error()),
		)
	}
}

// cleanupKeywordIndex 尽力而为地清掉这篇文档在检索索引里的分块。
//
// 和向量清理同一套取舍：索引删不掉不影响正确性（命中的分块回到 PostgreSQL
// 查不到就被跳过），只是白占空间。所以失败只记警告，不把删文档本身变成失败。
func (s *Service) cleanupKeywordIndex(ctx context.Context, documentID uint64) {
	if s.keywords == nil {
		observability.LogWithTrace(ctx, s.logger).Warn("knowledge: keyword index cleanup skipped, no search index configured",
			slog.Uint64("document_id", documentID),
		)
		return
	}
	deleted, err := s.keywords.DeleteByDocument(ctx, documentID)
	if err != nil {
		observability.LogWithTrace(ctx, s.logger).Warn("knowledge: keyword index cleanup failed, stale documents remain until a full rebuild",
			slog.Uint64("document_id", documentID),
			slog.String("error", err.Error()),
		)
		return
	}
	observability.LogWithTrace(ctx, s.logger).Info("knowledge: keyword index cleaned up",
		slog.Uint64("document_id", documentID),
		slog.Int64("deleted", deleted),
	)
}

func parseVisibility(raw string) (document.Visibility, error) {
	visibility := strings.ToLower(strings.TrimSpace(raw))
	if visibility == "" {
		return document.DefaultVisibility, nil
	}
	switch document.Visibility(visibility) {
	case document.VisibilitySystem, document.VisibilityPrivate:
		return document.Visibility(visibility), nil
	default:
		return "", invalid("visibility must be system or private")
	}
}

// mergeMetadata 把调用方给的元信息与正文溯源信息合并。source 与 title 以行内
// 字段为准，调用方同名键会被覆盖 —— 否则元数据里的 source 会和 documents.source
// 分叉，检索侧回指原文时会指向不存在的位置。
func mergeMetadata(extra map[string]string, source, title string) map[string]any {
	metadata := make(map[string]any, len(extra)+2)
	for key, value := range extra {
		metadata[key] = value
	}
	metadata[constant.MetaSource] = source
	metadata[constant.MetaTitle] = title
	return metadata
}

func mapContentError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, rag.ErrContentTooLarge):
		return ErrContentTooLarge
	case errors.Is(err, rag.ErrContentOutsideRoot):
		return ErrContentOutsideRoot
	case errors.Is(err, rag.ErrContentNotFound):
		return ErrContentUnavailable
	case errors.Is(err, rag.ErrContentNotManaged):
		return ErrContentNotManaged
	default:
		return fmt.Errorf("knowledge: content store: %w", err)
	}
}

func titleFromSource(source string) string {
	base := filepath.Base(source)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}
