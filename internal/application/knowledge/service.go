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
	"eino-quickstart/internal/rag/grouping"
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
	queue    IndexTaskQueue
	vectors  VectorIndex
	keywords KeywordIndex
	searcher Searcher
	grouping grouping.Policy
	limits   Limits
	logger   *slog.Logger

	// contents 是召回时取正文的端口（documents.content）。
	contents DocumentContent

	// maxDocumentBytes 是单篇正文的字节上限（knowledge.maxDocumentBytes）。
	//
	// 限在写入这一层而不是数据库层：Postgres 的 text 收得下 1GB，而「一次上传
	// 能塞多大」是服务的约束。越限的正文一律拒绝，不做截断 —— 截断一份正文
	// 等于把索引建在一份不完整的原文上，而调用方看不出少了什么。
	maxDocumentBytes int
}

// DocumentContent 是「按文档 id 批量取正文」的窄端口。
//
// 单独抽出来有两个理由：召回补正文是**检索链路里唯一碰数据库**的一步，把它
// 抽成端口之后这条链路可以脱离数据库测；而「正文从哪来」本身也变成一处可以
// 替换的决定（今天是 documents.content，将来若加一层正文缓存只改这里）。
type DocumentContent interface {
	Contents(ctx context.Context, documentIDs []uint64) (map[uint64]string, error)
}

// ServiceDeps 是知识库用例的全部外部依赖。
//
// 用结构体而不是继续加位置参数：这里面有五六个同类型的接口（队列、向量、检索
// 索引、召回），位置参数下把其中两个写反不会有编译错误，只会在运行期表现为
// 「任务投不进队列」或「删除没清干净」——而那种症状离根因很远。
type ServiceDeps struct {
	Client *ent.Client
	Queue  IndexTaskQueue

	// Vectors / Keywords 都可以是 nil：删除路径上的清理由它们承担，而清理是
	// 尽力而为的 —— 孤儿向量与孤儿文档取不回来（检索要回到 chunk 行做过滤），
	// 只是白占空间，靠后续全量重建收拾。所以外部存储没配或连不上都不该挡住
	// 「删文档」这件事本身。
	Vectors  VectorIndex
	Keywords KeywordIndex

	// Searcher 是召回能力。nil 表示这个进程不做召回（如 cmd/worker）——
	// 此时 Search 返回 ErrSearchUnavailable，而不是「什么都搜不到」。
	Searcher Searcher

	// Grouping 决定召回结果的归并粒度，按数据集类型解析。零值可用：
	// 没配的类型回落到 grouping.Default（按文档归并）。
	Grouping grouping.Policy

	// Limits 是召回入口的输入约束，零值会补成默认值。
	Limits Limits

	// MaxDocumentBytes 是单篇正文的字节上限，零值取 defaultDocumentMaxBytes。
	MaxDocumentBytes int

	// Logger 可选。
	Logger *slog.Logger
}

// NewService 组装用例层。
func NewService(deps ServiceDeps) (*Service, error) {
	if deps.Client == nil {
		return nil, errors.New("knowledge: ent client is required")
	}
	if deps.Queue == nil {
		return nil, errors.New("knowledge: index queue is required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxDocumentBytes := deps.MaxDocumentBytes
	if maxDocumentBytes <= 0 {
		maxDocumentBytes = defaultDocumentMaxBytes
	}
	return &Service{
		client:   deps.Client,
		queue:    deps.Queue,
		vectors:  deps.Vectors,
		keywords: deps.Keywords,
		searcher: deps.Searcher,
		grouping: deps.Grouping,
		limits:   deps.Limits.withDefaults(),
		logger:   logger,

		contents:         &entDocumentContent{client: deps.Client},
		maxDocumentBytes: maxDocumentBytes,
	}, nil
}

// defaultDocumentMaxBytes 是没配 knowledge.maxDocumentBytes 时的正文上限。
//
// 5MiB 与它作为文件时的旧上限一致：换了个存储介质，不该顺手把能收的文件
// 大小改一档。
const defaultDocumentMaxBytes = 5 << 20

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

	// 产品型录：按产品拆成一条一条文档。只处理「没有显式 source」这一种情况
	// —— 显式给了 source 表示调用方要求「这个标识就对应一条文档」，那时再把它
	// 拆成好几条，它会发现自己指定的那个标识一条都没落上。
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

// createSingleDocument 是「一条文档」的路径：非产品数据集，以及产品数据集里
// 没有产品块的正文，都走它。
func (s *Service) createSingleDocument(
	ctx context.Context,
	in CreateInput,
	title, source string,
	visibility document.Visibility,
	owner string,
) (*ent.Document, error) {
	content := in.Content
	if strings.TrimSpace(content) == "" {
		// 正文是必填：它从落库那一刻起就是唯一的真相，没有它这条文档什么都不是。
		// 从前允许「只给 source，接口去把文件读进来」，现在没有文件可读了。
		return nil, invalid("content is required")
	}
	if err := s.checkContentSize(content); err != nil {
		return nil, err
	}
	if title == "" {
		if source != "" {
			title = titleFromSource(source)
		} else {
			return nil, invalid("title is required when content is provided")
		}
	}
	if title == "" {
		title = "document"
	}
	if source == "" {
		// 标识由服务端派生，调用方不用管命名；派生规则是纯函数，见 rag.DocumentSource。
		source = rag.DocumentSource(in.DatasetID, rag.Slugify(title))
	}

	created, err := s.client.Document.Create().
		SetDatasetID(in.DatasetID).
		SetSource(source).
		SetTitle(title).
		SetContent(content).
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

// upsertProductDocument 按型号落一条产品文档：没有就建，有就更新，正文与产品头
// 一字不差就什么都不做。
func (s *Service) upsertProductDocument(ctx context.Context, in CreateInput, spec productDocument, visibility document.Visibility, owner string) (*CreateResult, error) {
	existing, err := s.findByExternalKey(ctx, in.DatasetID, spec.Key)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.refreshProductDocument(ctx, in, existing, spec, visibility)
	}

	if err := s.checkContentSize(spec.Content); err != nil {
		return nil, err
	}
	source := rag.DocumentSource(in.DatasetID, spec.Stem)

	create := s.client.Document.Create().
		SetDatasetID(in.DatasetID).
		SetSource(source).
		SetTitle(spec.Title).
		SetContent(spec.Content).
		SetMetadata(mergeProductMetadata(spec, in.Metadata, source, spec.Title)).
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
// 比对的是产品块的**指纹**（正文 + 解析出的头），不是文件的字节：
//
//   - 只比正文会漏掉「只改了 YAML 头」的重传（改个系列名、补一个规格），
//     那次的正文一个字没变，于是元数据永远停在旧值上，而这从外部完全看不出来
//     —— 症状是「按新系列名搜不到这个产品」。
//   - 直接拿解析出的元数据去和库里那份比也不行：它要经过 jsonb 往返，整数会
//     变成 float64、指针会变成值。类型对不上就会被判成「变了」，于是每次重传
//     都白重切一遍整库，而索引结果其实完全一样。
//
// 指纹是同一份代码算出来的字符串，两边不经过任何序列化，所以只有真正的内容
// 变化才会让它不同。
func (s *Service) refreshProductDocument(ctx context.Context, in CreateInput, doc *ent.Document, spec productDocument, visibility document.Visibility) (*CreateResult, error) {
	if storedSpecHash(doc.Metadata) == spec.Fingerprint {
		if doc.Status == document.StatusReady {
			// 内容一字不差、索引也是好的：这次上传对它没有任何影响。
			return &CreateResult{Document: doc, Operation: OperationUnchanged}, nil
		}
		// 内容没变但索引没到 ready（上次失败、或者还在排队）：补投一次，让
		// 「重传整份文件」顺带具备重试失败文档的能力。
		if err := s.enqueue(ctx, doc, in.DatasetID, tasks.IndexModeCatchUp); err != nil {
			return nil, err
		}
		return &CreateResult{Document: doc, Operation: OperationUpdated}, nil
	}

	if err := s.checkContentSize(spec.Content); err != nil {
		return nil, err
	}

	update := s.client.Document.UpdateOneID(doc.ID).
		SetTitle(spec.Title).
		SetContent(spec.Content).
		SetMetadata(mergeProductMetadata(spec, in.Metadata, doc.Source, spec.Title)).
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
// 正文就是 documents.content 那一列，改它和改标题一样是一次普通的行更新 ——
// 不需要判断「这份正文归不归接口管」（那个区分是文件时代的产物：调用方自己放进
// knowledge.root 的文件不该被接口覆盖；现在没有外部文件了，每一篇正文都在库里）。
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
		if err := s.checkContentSize(in.Content); err != nil {
			return nil, err
		}
		update.SetContent(in.Content)
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
//
// 这里**不检查正文有没有**：正文的解析与补齐（含存量数据的导入）是 worker 的
// 职责，它比这一层更清楚「这篇文档的正文还能不能找回来」——在请求期凭一个空串
// 就判死，会把一批本来能靠导入救回来的文档直接挡在门外。
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

// ReindexDataset 逐个文档重建，互不影响：某个文档排不进队列只计入 failed，
// 其余照常排队。
//
// 「这篇文档的正文还能不能拿到」不在这里判：worker 会在解析时给出诚实的结论
// （导入得到就续跑，确实没有就把文档落成 failed）。请求期唯一能看到的信号是
// content 是不是空，而空串既可能是「正文真没了」，也可能是「存量文档还没导入」，
// 拿它判死会错杀后一类。
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

// Delete 摘掉分块与文档行，然后尽力而为地清理向量与检索索引。
//
// 关系库的部分在一个事务里完成，正文（documents.content）随行一起消失 ——
// 这是它比文件好的地方之一：文件时代「删行」和「删文件」是两步，中间失败就会
// 留下一份没有主人的正文，或者一条指向已删文件的文档。
//
// Milvus 不参与这个事务，清理失败只记警告 —— 孤儿向量取不回来（检索要回到
// chunk 行做过滤），但会一直占着向量库空间，需要靠后续的全量重建收拾。
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

// mergeProductMetadata 合并产品文档的元数据：调用方给的键、产品块的元数据、行内
// 溯源信息、以及这次写入的产品块指纹。
//
// 顺序是有讲究的：调用方的键先铺，**产品块的键后盖** —— 产品身份（型号、系列、
// 品类）的真相在正文的 YAML 头里，调用方不能通过 metadata 把它改成另一个值，
// 否则「按型号查重」用的键与文档里记的型号会分叉，之后按型号就搜不到它了。
// source 与 title 最后盖，与单文档路径同口径（见 mergeMetadata）。
//
// 指纹写在最后：它是服务端算出来的比对依据，不是调用方可以设置的元信息。
func mergeProductMetadata(spec productDocument, extra map[string]string, source, title string) map[string]any {
	metadata := make(map[string]any, len(spec.Metadata)+len(extra)+3)
	for key, value := range extra {
		metadata[key] = value
	}
	for key, value := range spec.Metadata {
		metadata[key] = value
	}
	metadata[constant.MetaSource] = source
	metadata[constant.MetaTitle] = title
	metadata[constant.MetaSpecHash] = spec.Fingerprint
	return metadata
}

// storedSpecHash 读回上一次写入的产品块指纹。
//
// 读不到（不是字符串、或者这条文档根本不是产品块建的）时返回空串：空串与任何
// 指纹都不相等，于是这次上传按「内容变了」处理并重新写一遍。宁可多重切一次，
// 也不要因为读不出指纹就把一次真实的改动当成没变。
func storedSpecHash(metadata map[string]any) string {
	value, _ := metadata[constant.MetaSpecHash].(string)
	return value
}

// checkContentSize 拦住超过 knowledge.maxDocumentBytes 的正文。
//
// 按字节而不是字符：上限回答的是「一次上传能塞多大」，配它的人是按请求体与
// 存储成本算的。
func (s *Service) checkContentSize(content string) error {
	if s.maxDocumentBytes > 0 && len(content) > s.maxDocumentBytes {
		return fmt.Errorf("%w: %d bytes exceeds %d", ErrContentTooLarge, len(content), s.maxDocumentBytes)
	}
	return nil
}

func titleFromSource(source string) string {
	base := filepath.Base(source)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.TrimSpace(base)
}
