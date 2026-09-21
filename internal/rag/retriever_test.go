package rag

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag/store/milvus"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// discardLogger 让降级路径的告警不淹掉测试输出。降级本身是这些用例的**预期**
// 行为，把警告打出来只会让无关的噪音盖住失败信息。
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---- 测试替身 ----
//
// 这一组测试关心的是「通道之间怎么配合」，不是「SQL 写没写对」，所以存储、
// 向量库与检索索引全部用桩：它们只回答「给什么候选」和「有没有报错」。

type fakeChunkStore struct {
	byID map[uint64]*ent.DocumentChunk
}

func (f *fakeChunkStore) CreateDocument(context.Context, []*schema.Document) ([]*schema.Document, error) {
	return nil, nil
}

func (f *fakeChunkStore) UpdateDocumentStatus(context.Context, []*schema.Document) ([]*schema.Document, error) {
	return nil, nil
}

func (f *fakeChunkStore) CreateChunk(context.Context, []*schema.Document) ([]*schema.Document, error) {
	return nil, nil
}

func (f *fakeChunkStore) ChunksByIDs(_ context.Context, ids []int64) ([]*ent.DocumentChunk, error) {
	out := make([]*ent.DocumentChunk, 0, len(ids))
	for _, id := range ids {
		if chunk, ok := f.byID[uint64(id)]; ok {
			out = append(out, chunk)
		}
	}
	return out, nil
}

func (f *fakeChunkStore) SearchChunks(context.Context, []string, int) ([]*ent.DocumentChunk, error) {
	return nil, nil
}

// fakeKeyword 是检索索引（ES）的桩。
//
// 两条通道各自记调用情况：用例要能分别核对「哪条通道被调了、传了多少候选」，
// 而「有没有被调用」正是「权重 0 是否真的关掉了通道」的唯一证据。
type fakeKeyword struct {
	bm25Hits  []es.ChunkHit
	bm25Err   error
	bm25Calls int
	bm25Seen  int // 最后一次请求的候选数

	exactHits  []es.ChunkHit
	exactErr   error
	exactCalls int
	exactSeen  int
}

func (f *fakeKeyword) SearchChunks(_ context.Context, _ string, topK int) ([]es.ChunkHit, error) {
	f.bm25Calls++
	f.bm25Seen = topK
	if f.bm25Err != nil {
		return nil, f.bm25Err
	}
	return f.bm25Hits, nil
}

func (f *fakeKeyword) SearchExact(_ context.Context, _ string, topK int) ([]es.ChunkHit, error) {
	f.exactCalls++
	f.exactSeen = topK
	if f.exactErr != nil {
		return nil, f.exactErr
	}
	return f.exactHits, nil
}

func (f *fakeKeyword) totalCalls() int { return f.bm25Calls + f.exactCalls }

type fakeVectorStore struct {
	results []milvus.SearchResult
	err     error
	calls   int
}

func (f *fakeVectorStore) EnsureCollection(context.Context) error { return nil }
func (f *fakeVectorStore) Upsert(context.Context, []int64, [][]float32) error {
	return nil
}
func (f *fakeVectorStore) Delete(context.Context, []int64) error { return nil }
func (f *fakeVectorStore) Ready(context.Context) error           { return nil }
func (f *fakeVectorStore) Close(context.Context) error           { return nil }

func (f *fakeVectorStore) Search(context.Context, []float32, int) ([]milvus.SearchResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

// fakeEmbedding 是底层 embedding 客户端的桩。它挂在 Embedder 的 inner 上，
// 这样切分逻辑仍然走真实实现。
type fakeEmbedding struct {
	err   error
	calls int
}

func (f *fakeEmbedding) EmbedStrings(_ context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, 0, len(texts))
	for range texts {
		out = append(out, []float64{0.1, 0.2, 0.3})
	}
	return out, nil
}

// newTestEmbedder 造一个内部走桩的 Embedder。
//
// config 必须给：EmbedStrings 会读它取期望维度，nil 会直接解引用。
func newTestEmbedder(inner embedding.Embedder) *Embedder {
	return &Embedder{inner: inner, config: &openai.EmbeddingConfig{}, maxTextsPerRequest: 10}
}

// newRetriever 组装一个全部依赖都是桩的检索器。
func newRetriever(
	t *testing.T,
	store *Store,
	inner embedding.Embedder,
	cfg HybridConfig,
) *HybridRetriever {
	t.Helper()
	cfg.Store = store
	cfg.Embedder = newTestEmbedder(inner)
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	hybrid, err := NewHybridRetriever(cfg)
	if err != nil {
		t.Fatalf("组装检索器: %v", err)
	}
	return hybrid
}

// chunkIn 造一个挂在某文档下的分块，文档默认可见性 system、状态 ready。
func chunkIn(id, datasetID uint64, enabled bool) *ent.DocumentChunk {
	return &ent.DocumentChunk{
		ID:         id,
		ChunkIndex: 0,
		Content:    "分块正文",
		Metadata:   map[string]any{},
		Edges: ent.DocumentChunkEdges{
			Document: &ent.Document{
				ID:           id * 10,
				DatasetID:    datasetID,
				Source:       "documents/x.md",
				Title:        "标题",
				OwnerSubject: "tester",
				Visibility:   document.VisibilitySystem,
				Status:       document.StatusReady,
				Enabled:      enabled,
			},
		},
	}
}

// ---- 用例 ----

// TestRetrieveKeepsKeywordResultsWhenEmbeddingFails 是这次改动的核心断言：
// 向量通道挂掉不能把关键词通道的结果一起吞掉。
//
// 反过来说，改之前这里返回的是错误 —— 于是 embedding 服务抖动、限流或模型下线
// 期间，连「H105P 是什么」这种纯型号查询都答不出来，而那正是关键词通道的主场。
func TestRetrieveKeepsKeywordResultsWhenEmbeddingFails(t *testing.T) {
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{1: chunkIn(1, 7, true)}},
		MilvusStore: &fakeVectorStore{results: []milvus.SearchResult{{ChunkID: 1, Score: 0.9}}},
		Keyword:     &fakeKeyword{bm25Hits: []es.ChunkHit{{ChunkID: 1, Score: 8.5}}},
	}
	embedding := &fakeEmbedding{err: errors.New("429 too many requests")}
	hybrid := newRetriever(t, store, embedding, HybridConfig{TopK: 5})

	docs, report, err := hybrid.RetrieveDetailed(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("向量通道失败不该让整次检索失败: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "1" {
		t.Fatalf("应当返回关键词通道的那条结果，实际 %v", docIDs(docs))
	}
	if embedding.calls == 0 {
		t.Error("向量通道必须真的尝试过，否则「降级」是假的")
	}
	// 降级必须出现在台账里：不然「结果变少了」会被归因到召回质量上。
	if degraded := report.Degraded(); len(degraded) != 1 || degraded[0] != string(ChannelVector) {
		t.Errorf("台账里应记下 vector 降级，实际 %v", report.Channels)
	}
}

// TestRetrieveDegradesToVectorWhenKeywordFails 是上一条的镜像：关键词通道失败时
// 也不该把向量通道的结果丢掉。
func TestRetrieveDegradesToVectorWhenKeywordFails(t *testing.T) {
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{2: chunkIn(2, 7, true)}},
		MilvusStore: &fakeVectorStore{results: []milvus.SearchResult{{ChunkID: 2, Score: 0.88}}},
		Keyword: &fakeKeyword{
			bm25Err:  errors.New("es: 集群不可达"),
			exactErr: errors.New("es: 集群不可达"),
		},
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{}, HybridConfig{TopK: 5})

	docs, _, err := hybrid.RetrieveDetailed(context.Background(), "铰链怎么调门缝")
	if err != nil {
		t.Fatalf("关键词通道失败不该让整次检索失败: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "2" {
		t.Fatalf("应当返回向量通道的那条结果，实际 %v", docIDs(docs))
	}
}

// TestRetrieveFailsOnlyWhenAllChannelsFail 守住另一头：三条都断了要报错，
// 而不是静默返回空结果 —— 空结果会被读成「没有相关内容」，那是错误结论。
func TestRetrieveFailsOnlyWhenAllChannelsFail(t *testing.T) {
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{}},
		MilvusStore: &fakeVectorStore{err: errors.New("milvus: 连接超时")},
		Keyword: &fakeKeyword{
			bm25Err:  errors.New("es: 集群不可达"),
			exactErr: errors.New("es: 集群不可达"),
		},
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{}, HybridConfig{TopK: 5})

	_, report, err := hybrid.RetrieveDetailed(context.Background(), "H105P")
	if err == nil {
		t.Fatal("所有通道都失败时必须报错")
	}
	// 报错要能看出是哪几条通道、各自坏在哪。
	for _, want := range []string{"exact", "keyword", "vector", "集群不可达", "连接超时"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息里缺少 %q: %v", want, err)
		}
	}
	if len(report.Degraded()) != 3 {
		t.Errorf("三条通道都该记为降级，实际 %v", report.Channels)
	}
}

// TestRetrieveAppliesDatasetScope 钉住数据集范围真的被用上。
//
// 这个过滤在改之前是死的（调用点永远传零值），后果是 /dataset/7/search 会把
// 别的数据集的段落一起返回。
func TestRetrieveAppliesDatasetScope(t *testing.T) {
	store := &Store{
		PgStore: &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{
			1: chunkIn(1, 7, true),
			2: chunkIn(2, 8, true),
		}},
		MilvusStore: &fakeVectorStore{},
		Keyword: &fakeKeyword{bm25Hits: []es.ChunkHit{
			{ChunkID: 2, Score: 9}, // 名次更靠前，但属于别的数据集
			{ChunkID: 1, Score: 8},
		}},
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{TopK: 5})

	docs, err := hybrid.Retrieve(context.Background(), "H105P", WithFilter(Filter{DatasetID: 7}))
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "1" {
		t.Fatalf("数据集 7 的检索只该看到分块 1，实际 %v", docIDs(docs))
	}
}

// TestRetrieveSkipsDisabledDocuments 钉住「停用」这个开关有效。
//
// document.enabled 是 PATCH /dataset/:id/documents/:docId/enabled 改的那个字段。
// 检索侧完全不看它时，那个接口就是个摆设：关掉的文档照样被搜出来。
func TestRetrieveSkipsDisabledDocuments(t *testing.T) {
	store := &Store{
		PgStore: &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{
			1: chunkIn(1, 7, false),
		}},
		MilvusStore: &fakeVectorStore{},
		Keyword:     &fakeKeyword{bm25Hits: []es.ChunkHit{{ChunkID: 1, Score: 9}}},
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{TopK: 5})

	docs, err := hybrid.Retrieve(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("停用的文档不该被召回，实际 %v", docIDs(docs))
	}
}

// TestRetrieveRejectsMismatchedFilterPayload 钉住「过滤条件类型不对就报错」。
//
// 回落成「不限」等于把一次带范围的检索悄悄放大成全库检索，那是越权而不是降级。
func TestRetrieveRejectsMismatchedFilterPayload(t *testing.T) {
	keyword := &fakeKeyword{bm25Hits: []es.ChunkHit{{ChunkID: 1, Score: 9}}}
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{1: chunkIn(1, 7, true)}},
		MilvusStore: &fakeVectorStore{},
		Keyword:     keyword,
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{}, HybridConfig{TopK: 5})

	_, err := hybrid.Retrieve(context.Background(), "H105P",
		retriever.WithDSLInfo(map[string]any{filterDSLKey: "dataset=7"}))
	if err == nil {
		t.Fatal("过滤条件类型不符时必须报错，而不是当成不过滤")
	}
	if !strings.Contains(err.Error(), "rag.Filter") {
		t.Errorf("错误信息里应说明期望的类型: %v", err)
	}
	if keyword.totalCalls() != 0 {
		t.Error("参数校验失败时不该已经发出检索请求")
	}
}

// TestScoreThresholdAppliesToChannelScore 钉住阈值的量纲。
//
// 阈值判的是通道原始分（余弦相似度 / BM25 分 / 词元命中率），不是融合分。
// 融合分的量级在 1/rrfSmoothing 上下（默认约 0.016），拿「0.5」这种相似度量级的
// 数字去比会一条不剩 —— 那种「配置看着合理、结果永远为空」的问题最难查，因为它
// 不报错。cmd/ragserver 里就写着 0.5。
func TestScoreThresholdAppliesToChannelScore(t *testing.T) {
	build := func(score float64) *HybridRetriever {
		return newRetriever(t, &Store{
			PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{1: chunkIn(1, 7, true)}},
			MilvusStore: &fakeVectorStore{},
			Keyword:     &fakeKeyword{bm25Hits: []es.ChunkHit{{ChunkID: 1, Score: score}}},
		}, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{TopK: 5, ScoreThreshold: 0.5})
	}

	kept, err := build(0.9).Retrieve(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(kept) != 1 {
		t.Fatalf("通道分 0.9 高于阈值 0.5，应当保留，实际 %d 条", len(kept))
	}

	dropped, err := build(0.1).Retrieve(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(dropped) != 0 {
		t.Fatalf("通道分 0.1 低于阈值 0.5，应当被过滤，实际 %d 条", len(dropped))
	}
}

// TestRetrieveRejectsEmptyQuery 守住空查询：空串在 ES 里等价于 match_all。
func TestRetrieveRejectsEmptyQuery(t *testing.T) {
	keyword := &fakeKeyword{}
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{}},
		MilvusStore: &fakeVectorStore{},
		Keyword:     keyword,
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{}, HybridConfig{TopK: 5})

	if _, err := hybrid.Retrieve(context.Background(), "   "); err == nil {
		t.Fatal("空查询必须被挡在检索之前")
	}
	if keyword.totalCalls() != 0 {
		t.Error("空查询不该发出检索请求")
	}
}

// TestExactChannelBringsBackStructuredMatch 是精确通道存在的理由。
//
// 型号这类值只出现在产品块的 YAML 头里、正文一次都不出现，所以「哪段文本提到了
// 这些词」这条通道抠不出来；逐字相等那条可以。台账里也要能看到是它出的力。
func TestExactChannelBringsBackStructuredMatch(t *testing.T) {
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{3: chunkIn(3, 7, true)}},
		MilvusStore: &fakeVectorStore{},
		Keyword:     &fakeKeyword{exactHits: []es.ChunkHit{{ChunkID: 3, Score: 7.2}}},
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{TopK: 5})

	docs, report, err := hybrid.RetrieveDetailed(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(docs) != 1 || docs[0].ID != "3" {
		t.Fatalf("精确通道的命中丢了，实际 %v", docIDs(docs))
	}
	if used := report.Used(); len(used) != 1 || used[0] != string(ChannelExact) {
		t.Errorf("台账应记下 exact 出力，实际 %v", report.Channels)
	}
}

// TestZeroWeightDisablesChannel 钉住「权重 0 = 关掉这条通道」。
//
// 这既是「纯关键词召回」的实现方式（把 vectorWeight 置 0），也是排查时排除某条
// 通道的手段 —— 所以它必须真的不发起请求，而不是「发起了但不计分」。
func TestZeroWeightDisablesChannel(t *testing.T) {
	keyword := &fakeKeyword{bm25Hits: []es.ChunkHit{{ChunkID: 1, Score: 5}}}
	embedding := &fakeEmbedding{}
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{1: chunkIn(1, 7, true)}},
		MilvusStore: &fakeVectorStore{results: []milvus.SearchResult{{ChunkID: 1, Score: 0.9}}},
		Keyword:     keyword,
	}
	hybrid := newRetriever(t, store, embedding, HybridConfig{
		TopK: 5,
		Retrieval: RetrievalPolicy{
			ExactWeight: 0, KeywordWeight: 1, VectorWeight: 0,
			RRFSmoothing:          60,
			ExactCandidateLimit:   20,
			KeywordCandidateLimit: 30,
			VectorCandidateLimit:  30,
		},
	})

	_, report, err := hybrid.RetrieveDetailed(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if keyword.exactCalls != 0 {
		t.Error("exactWeight=0 时不该发起精确检索")
	}
	if embedding.calls != 0 {
		t.Error("vectorWeight=0 时不该调用 embedding —— 连查询向量都不需要算")
	}
	if len(report.Channels) != 1 || report.Channels[0].Name != string(ChannelKeyword) {
		t.Errorf("关掉的通道不该出现在台账里，实际 %v", report.Channels)
	}
}

// TestFusionWeightsDecideOrder 钉住权重真的参与排序。
//
// 这正是「优先关键字召回」的落地方式：配置里 exact 3.0 > keyword 1.5 > vector 1.0，
// 那么名次相同的两条命中，精确通道捞回来的那条要排在前面。
func TestFusionWeightsDecideOrder(t *testing.T) {
	store := &Store{
		PgStore: &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{
			1: chunkIn(1, 7, true), // 只被关键词通道命中
			2: chunkIn(2, 7, true), // 只被精确通道命中
		}},
		MilvusStore: &fakeVectorStore{},
		Keyword: &fakeKeyword{
			bm25Hits:  []es.ChunkHit{{ChunkID: 1, Score: 5}},
			exactHits: []es.ChunkHit{{ChunkID: 2, Score: 9}},
		},
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{TopK: 5})

	docs, err := hybrid.Retrieve(context.Background(), "H105P")
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("期望两条结果，实际 %v", docIDs(docs))
	}
	if docs[0].ID != "2" {
		t.Fatalf("精确通道权重更高，命中 2 应排在前面，实际顺序 %v", docIDs(docs))
	}
}

// TestCandidatesScalesWithDatasetScope 钉住候选数会随范围过滤提高。
//
// 过滤发生在候选之后，跨数据集语料下按基础候选数取会被别的数据集占满名额，
// 表现是「明明有答案却少给了几条」。
func TestCandidatesScalesWithDatasetScope(t *testing.T) {
	if got := candidates(10, Filter{}); got != 10 {
		t.Errorf("不限范围的候选数是 %d，期望 10", got)
	}
	if got := candidates(10, Filter{DatasetID: 7}); got != 10*datasetScopeMultiplier {
		t.Errorf("限定数据集时的候选数是 %d，期望 %d", got, 10*datasetScopeMultiplier)
	}
}

// TestChannelCandidateLimitsComeFromPolicy 守住「候选上限由配置决定」。
//
// 早先这个倍数是代码里的常量，配置里那几个 *CandidateLimit 校验了却没人读 ——
// 改了配置不生效，而且不报错。
func TestChannelCandidateLimitsComeFromPolicy(t *testing.T) {
	keyword := &fakeKeyword{}
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{}},
		MilvusStore: &fakeVectorStore{},
		Keyword:     keyword,
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{
		TopK: 5,
		Retrieval: RetrievalPolicy{
			ExactWeight: 1, KeywordWeight: 1, VectorWeight: 0,
			RRFSmoothing:          60,
			ExactCandidateLimit:   7,
			KeywordCandidateLimit: 33,
			VectorCandidateLimit:  30,
		},
	})

	if _, err := hybrid.Retrieve(context.Background(), "H105P"); err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if keyword.bm25Seen != 33 {
		t.Errorf("关键词通道拿到的候选数是 %d，期望配置里的 33", keyword.bm25Seen)
	}
	if keyword.exactSeen != 7 {
		t.Errorf("精确通道拿到的候选数是 %d，期望配置里的 7", keyword.exactSeen)
	}

	// 限定数据集时再放大一档。
	if _, err := hybrid.Retrieve(context.Background(), "H105P", WithFilter(Filter{DatasetID: 7})); err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if keyword.bm25Seen != 33*datasetScopeMultiplier {
		t.Errorf("限定数据集时关键词候选数是 %d，期望 %d", keyword.bm25Seen, 33*datasetScopeMultiplier)
	}
}

// TestCandidateLimitNeverBelowTopK 守住「候选数不能小于最终条数」。
//
// 配置写小了（比如候选上限 3、topK 10）时，如果不兜底就会在过滤后必然凑不满，
// 表现是「结果总是比要的少」，而配置校验拦不住调用方在运行期传一个更大的 topK。
func TestCandidateLimitNeverBelowTopK(t *testing.T) {
	keyword := &fakeKeyword{}
	store := &Store{
		PgStore:     &fakeChunkStore{byID: map[uint64]*ent.DocumentChunk{}},
		MilvusStore: &fakeVectorStore{},
		Keyword:     keyword,
	}
	hybrid := newRetriever(t, store, &fakeEmbedding{err: errors.New("不参与本用例")}, HybridConfig{
		TopK: 10,
		Retrieval: RetrievalPolicy{
			ExactWeight: 0, KeywordWeight: 1, VectorWeight: 0,
			RRFSmoothing:          60,
			ExactCandidateLimit:   20,
			KeywordCandidateLimit: 3,
			VectorCandidateLimit:  30,
		},
	})

	if _, err := hybrid.Retrieve(context.Background(), "H105P"); err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if keyword.bm25Seen != 10 {
		t.Errorf("候选数应当被抬到 topK(10)，实际 %d", keyword.bm25Seen)
	}
}

func docIDs(docs []*schema.Document) []string {
	ids := make([]string, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	return ids
}
