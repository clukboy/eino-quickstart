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
	"eino-quickstart/internal/platform/queue/tasks"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	ragparser "eino-quickstart/internal/rag/parser"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type IndexTaskQueue interface {
	EnqueueIndex(ctx context.Context, datasetID, documentID uint64, mode tasks.IndexMode) error
}

type VectorIndex interface {
	Upsert(ctx context.Context, chunkIDs []int64, vectors [][]float32) error
	Delete(ctx context.Context, chunkIDs []int64) error
}

type KeywordIndex interface {
	IndexChunks(ctx context.Context, docs []es.ChunkDoc) error
	DeleteByDocument(ctx context.Context, documentID uint64) (int64, error)
}

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
func NewService(client *ent.Client, content *rag.ContentStore, queue IndexTaskQueue, vectors VectorIndex, keywords KeywordIndex, logger *slog.Logger) (*Service, error) {
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

type CreateInput struct {
	DatasetID    uint64
	Title        string
	Content      string
	Source       string
	Visibility   string
	OwnerSubject string
	Metadata     map[string]string
}

// 一次写入对某条文档做了什么。
//
// 它回答的是「这次上传把我怎么了」，所以 unchanged 只说明正文一字不差，不代表
// 索引已经好了。正文没变但索引不在 ready 时仍会重新排队，那种情况按 updated
// 上报 —— 这次请求确实动了它。
const (
	OperationCreated   = "created"
	OperationUpdated   = "updated"
	OperationUnchanged = "unchanged"
)

// CreateResult 是一条文档在一次写入里的结果。
type CreateResult struct {
	Document  *ent.Document
	Operation string
}

// Create 写入正文并排队索引，返回这次写入涉及的全部文档。
//
// 返回值是列表而不是单条：产品型录里的一份文件通常有好几个产品块，这里会把
// 它们拆成「一个产品一条文档」（见 createProductDocuments）。其他类型的数据集
// 恒为一条。
//
// 逐条落库、逐条投递，中间失败时已经落下的那些保持现状，重传同一份文件会把
// 它们识别成 unchanged 再继续 —— 所以**失败后重传就是修复**，不需要回滚。
// 回滚反而是有害的：正文文件可能本来就在托管目录里，删掉等于替调用方决定文件
// 去留。代价是这一批不是原子的，客户端要按返回的每一条看状态。
func (s *Service) Create(ctx context.Context, in CreateInput) (_ []CreateResult, err error) {
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

	owner := strings.TrimSpace(in.OwnerSubject)
	if owner == "" {
		owner = base.OwnerSubject
	}

	title := strings.TrimSpace(in.Title)
	source := strings.TrimSpace(in.Source)

	// 产品型录：按产品拆成一条一条文档。只处理「正文由接口落盘」这一种情况
	// （没有显式 source）—— 注册一份已经在托管目录里的外部文件时，文件是调用
	// 方的资产，接口不该把它拆开再写回好几个新文件。
	if s.isProductDataset(base) && source == "" && strings.TrimSpace(in.Content) != "" {
		products, err := s.createProductDocuments(ctx, in, visibility, owner)
		if err != nil {
			return nil, err
		}
		if len(products) > 0 {
			span.SetAttributes(attribute.Int("knowledge.documents", len(products)))
			return products, nil
		}
		// len == 0：正文里一个产品块都没有，落到下面按整份文件建一条。
	}

	created, err := s.createSingleDocument(ctx, in, title, source, visibility, owner)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.Int64("knowledge.document_id", int64(created.ID)),
		attribute.String("knowledge.source", created.Source),
		attribute.String("knowledge.title", created.Title),
	)
	return []CreateResult{{Document: created, Operation: OperationCreated}}, nil
}

// createSingleDocument 是「一份文件一条文档」的路径：非产品数据集，以及产品
// 数据集里没有产品块的正文，都走它。
func (s *Service) createSingleDocument(
	ctx context.Context,
	in CreateInput,
	title, source string,
	visibility document.Visibility,
	owner string,
) (*ent.Document, error) {
	switch {
	case strings.TrimSpace(in.Content) != "":
		if title == "" {
			return nil, invalid("title is required when content is provided")
		}
		if source == "" {
			// 文件名从标题派生，托管目录按数据集分片。
			generated, err := s.content.Create(in.DatasetID, title, in.Content)
			if err != nil {
				return nil, mapContentError(err)
			}
			source = generated
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

	if err := s.enqueue(ctx, created, in.DatasetID, tasks.IndexModeCatchUp); err != nil {
		return nil, err
	}
	return created, nil
}

// isProductDataset 报告这个数据集是否按产品拆文档。
//
// 判据是 dataset.type 与解析器注册名是同一个常量（rag/parser.TypeProduct）。
// 这两处必须是同一个字符串：type 填错不会报错，只会静默退化成 TextParser，
// 正文里的产品块 YAML 头没人解析 —— 症状是「ES 里只有正文，按型号搜不到」。
func (s *Service) isProductDataset(base *ent.Dataset) bool {
	return strings.TrimSpace(base.Type) == ragparser.TypeProduct
}

// createProductDocuments 把一份多产品文件拆成「一个产品一条文档」。
//
// 返回空切片表示正文里没有产品块（普通 Markdown 传到了产品数据集），调用方
// 应当退回整份文件一条文档。这是正常情况，不是错误。
func (s *Service) createProductDocuments(ctx context.Context, in CreateInput, visibility document.Visibility, owner string) (_ []CreateResult, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.split_products")
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	specs, err := buildProductDocuments(in.Content, in.Title)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		observability.LogWithTrace(ctx, s.logger).Info(
			"knowledge: 产品数据集里的正文没有产品块，按整份文件建一条文档",
			slog.Uint64("dataset_id", in.DatasetID),
			slog.String("title", in.Title),
		)
		return nil, nil
	}
	span.SetAttributes(attribute.Int("knowledge.products", len(specs)))

	results := make([]CreateResult, 0, len(specs))
	for _, spec := range specs {
		result, err := s.upsertProductDocument(ctx, in, spec, visibility, owner)
		if err != nil {
			return results, err
		}
		results = append(results, *result)
	}

	// 一次上传的规模要有痕迹：「传了一份文件，只更新了其中一个产品」和
	// 「全都重建了一遍」在监控上必须能分开。
	observability.LogWithTrace(ctx, s.logger).Info("knowledge: 文件已按产品拆分",
		slog.Uint64("dataset_id", in.DatasetID),
		slog.String("title", in.Title),
		slog.Int("products", len(specs)),
		slog.Int(OperationCreated, countOperation(results, OperationCreated)),
		slog.Int(OperationUpdated, countOperation(results, OperationUpdated)),
		slog.Int(OperationUnchanged, countOperation(results, OperationUnchanged)),
	)
	return results, nil
}

// upsertProductDocument 按型号落一条产品文档：没有就建，有就更新，正文一字不差
// 就什么都不做。
func (s *Service) upsertProductDocument(ctx context.Context, in CreateInput, spec productDocument, visibility document.Visibility, owner string) (*CreateResult, error) {
	existing, err := s.findByExternalKey(ctx, in.DatasetID, spec.Key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.refreshProductDocument(ctx, in, existing, spec, visibility)
	}

	source, err := s.content.CreateNamed(in.DatasetID, spec.Stem, spec.Content)
	if err != nil {
		return nil, mapContentError(err)
	}

	create := s.client.Document.Create().
		SetDatasetID(in.DatasetID).
		SetSource(source).
		SetTitle(spec.Title).
		SetMetadata(mergeProductMetadata(spec.Metadata, in.Metadata, source, spec.Title)).
		SetVisibility(visibility).
		SetOwnerSubject(owner).
		SetStatus(document.StatusIndexing)
	if spec.Key != "" {
		// 空键留 NULL 而不是写空串：唯一索引把 NULL 视为互不相同，所以没有
		// 型号的产品可以有任意多条；写成空串则会互相撞车，第二个就建不出来。
		create.SetExternalKey(spec.Key)
	}

	created, err := create.Save(ctx)
	if err != nil {
		// 并发上传同一型号：先查后写漏掉的那条由唯一索引兜住。这里退化成
		// 更新，而不是把 500 丢给调用方 —— 两个请求上传同一个产品是正常操作。
		if spec.Key != "" && ent.IsConstraintError(err) {
			if again, lookupErr := s.findByExternalKey(ctx, in.DatasetID, spec.Key); lookupErr == nil && again != nil {
				return s.refreshProductDocument(ctx, in, again, spec, visibility)
			}
		}
		return nil, fmt.Errorf("knowledge: create product document: %w", err)
	}

	if err := s.enqueue(ctx, created, in.DatasetID, tasks.IndexModeCatchUp); err != nil {
		return nil, err
	}
	return &CreateResult{Document: created, Operation: OperationCreated}, nil
}

// refreshProductDocument 让一条已存在的产品文档追上这次上传的内容。
//
// 比对的是**文件字节**，不是 documents.metadata 里的 content_hash：那个哈希是
// worker 切块时才写的，刚建好、还没跑过索引的文档上根本没有值，拿它比对会把
// 「内容没变」一律误判成「内容变了」，于是每次重传都白重切一遍整库。
func (s *Service) refreshProductDocument(ctx context.Context, in CreateInput, doc *ent.Document, spec productDocument, visibility document.Visibility) (*CreateResult, error) {
	current, readErr := s.content.Read(doc.Source)
	if readErr == nil && current == spec.Content {
		if doc.Status == document.StatusReady {
			// 正文一字不差、索引也是好的：这次上传对它没有任何影响。
			return &CreateResult{Document: doc, Operation: OperationUnchanged}, nil
		}
		// 正文没变但索引没到 ready（上次失败、或者还在排队）：补投一次，让
		// 「重传整份文件」顺带具备重试失败文档的能力。
		if err := s.enqueue(ctx, doc, in.DatasetID, tasks.IndexModeCatchUp); err != nil {
			return nil, err
		}
		return &CreateResult{Document: doc, Operation: OperationUpdated}, nil
	}

	// 正文文件是注册进来的外部文件时不能覆盖：那是调用方的资产。产品文档都是
	// 接口自己写在托管目录里的，走到这里说明这条行的 external_key 与一份手工
	// 创建的文档撞了 —— 报出来，别无声改写别人的文件。
	if !s.content.Managed(doc.Source) {
		return nil, ErrContentNotManaged
	}
	if err := s.content.Write(doc.Source, spec.Content); err != nil {
		return nil, mapContentError(err)
	}

	update := s.client.Document.UpdateOneID(doc.ID).
		SetTitle(spec.Title).
		SetMetadata(mergeProductMetadata(spec.Metadata, in.Metadata, doc.Source, spec.Title)).
		SetStatus(document.StatusIndexing)
	if strings.TrimSpace(in.Visibility) != "" {
		// 没传可见性表示不动它，而不是重置回默认值。
		update.SetVisibility(visibility)
	}

	saved, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("knowledge: update product document: %w", err)
	}
	if err := s.enqueue(ctx, saved, in.DatasetID, tasks.IndexModeCatchUp); err != nil {
		return nil, err
	}
	return &CreateResult{Document: saved, Operation: OperationUpdated}, nil
}

// findByExternalKey 按数据集内的业务标识查一条文档。
//
// 空键直接返回 nil：没有型号的产品块不参与查重（它的文件名是随机后缀），用空
// 键去查会把「所有没型号的文档」当成同一个。
func (s *Service) findByExternalKey(ctx context.Context, datasetID uint64, key string) (*ent.Document, error) {
	if key == "" {
		return nil, nil
	}
	doc, err := s.client.Document.Query().
		Where(
			document.DatasetIDEQ(datasetID),
			document.ExternalKeyEQ(key),
			document.StatusNEQ(document.StatusDeleted),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("knowledge: look up document by external key %q: %w", key, err)
	}
	return doc, nil
}

// countOperation 数一数这批结果里有几条是某个动作。
func countOperation(results []CreateResult, operation string) int {
	count := 0
	for _, result := range results {
		if result.Operation == operation {
			count++
		}
	}
	return count
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
		if err := s.enqueue(ctx, saved, in.DatasetID, tasks.IndexModeCatchUp); err != nil {
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

	if err := s.enqueue(ctx, saved, datasetID, tasks.IndexModeRebuild); err != nil {
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
		if err := s.enqueue(ctx, saved, datasetID, tasks.IndexModeRebuild); err != nil {
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
//
// mode 由调用方决定：写入路径传 tasks.IndexModeCatchUp（内容变了自然会重切），
// 显式 reindex 传 tasks.IndexModeRebuild（正文没变也要按当前配置重切）。
func (s *Service) enqueue(ctx context.Context, doc *ent.Document, datasetID uint64, mode tasks.IndexMode) error {
	if err := s.queue.EnqueueIndex(ctx, datasetID, doc.ID, mode); err != nil {
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

// mergeProductMetadata 合并产品文档的元数据：调用方给的键、产品块元数据、行内
// 溯源信息。
//
// 顺序是有讲究的：调用方的键先铺，**产品块的键后盖** —— 产品身份（型号、系列、
// 品类）的真相在正文的 YAML 头里，调用方不能通过 metadata 把它改成另一个值，
// 否则「按型号查重」用的键与文档里记的型号会分叉，之后按型号就搜不到它了。
// source 与 title 最后盖，与单文档路径同口径（见 mergeMetadata）。
func mergeProductMetadata(block map[string]any, extra map[string]string, source, title string) map[string]any {
	metadata := make(map[string]any, len(block)+len(extra)+2)
	for key, value := range extra {
		metadata[key] = value
	}
	for key, value := range block {
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

func (s *Service) ContentFromSource(ctx context.Context, source string) (string, error) {
	return s.content.Read(source)
}
