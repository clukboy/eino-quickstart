package rag

import (
	"context"
	"eino-quickstart/internal/rag/constant"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// Reranker 可选重排接口（cross-encoder / LLM rerank 均可实现）。
// [优化] 新增：召回后精排是提升 RAG 效果最显著的手段之一。
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []*schema.Document) ([]*schema.Document, error)
}

// HybridRetriever 实现 eino retriever.Retriever：
// 向量检索 + 词法检索，RRF 融合，可选重排。
type HybridRetriever struct {
	store     *Store
	embedder  *Embedder
	reranker  Reranker // 可为 nil
	rrfK      float64
	defTopK   int
	defThresh float64
}

type HybridConfig struct {
	Store          *Store
	Embedder       *Embedder
	Reranker       Reranker // 可选
	TopK           int      // 默认 5
	ScoreThreshold float64  // 默认 0（不过滤）
	RRFK           float64  // 默认 60
}

func NewHybridRetriever(cfg HybridConfig) (*HybridRetriever, error) {
	if cfg.Store == nil || cfg.Embedder == nil {
		return nil, fmt.Errorf("rag: store and embedder are required")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.RRFK <= 0 {
		cfg.RRFK = 60
	}
	return &HybridRetriever{
		store: cfg.Store, embedder: cfg.Embedder, reranker: cfg.Reranker,
		rrfK: cfg.RRFK, defTopK: cfg.TopK, defThresh: cfg.ScoreThreshold,
	}, nil
}

// Retrieve 实现 eino retriever.Retriever。
// 注意：query embedding 必须与入库时使用同一个模型！
func (h *HybridRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("rag: empty query")
	}

	// [优化] 复用 eino 通用 Option（TopK / ScoreThreshold 可被调用方动态覆盖）
	common := &retriever.Options{}
	retriever.GetCommonOptions(common, opts...)

	topK := h.defTopK
	if common.TopK != nil {
		topK = *common.TopK
	}
	thresh := h.defThresh
	if common.ScoreThreshold != nil {
		thresh = *common.ScoreThreshold
	}

	// [优化] ACL filter 也可通过 Options.Extra / 自定义 Option 传入，此处示例用零值
	filter := Filter{}
	candidateK := topK * 4 // 召回放大，给融合和重排留余量

	// 通道 1：向量
	vecs, err := h.embedder.EmbedStrings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("rag: embed query: %w", err)
	}
	vecHits, err := h.store.SearchByVector(ctx, vecs[0], candidateK, filter)
	if err != nil {
		return nil, fmt.Errorf("rag: vector search: %w", err)
	}

	// 通道 2：词法
	txtHits, err := h.store.SearchByText(ctx, query, candidateK, filter)
	if err != nil {
		return nil, fmt.Errorf("rag: text search: %w", err)
	}

	fused := h.rrfFuse(vecHits, txtHits)

	// 质量过滤
	out := make([]*schema.Document, 0, len(fused))
	for _, d := range fused {
		if strings.TrimSpace(d.Content) == "" {
			continue
		}
		if thresh > 0 && d.Score() < thresh {
			continue
		}
		out = append(out, d)
	}

	// [优化] 精排
	if h.reranker != nil {
		out, err = h.reranker.Rerank(ctx, query, out)
		if err != nil {
			return nil, fmt.Errorf("rag: rerank: %w", err)
		}
	}

	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

// rrfFuse: score = Σ 1/(k + rank)
func (h *HybridRetriever) rrfFuse(channels ...[]Hit) []*schema.Document {
	type acc struct {
		doc   *schema.Document
		score float64
	}
	byID := make(map[string]*acc)
	order := make([]string, 0)

	for _, hits := range channels {
		for rank, hit := range hits {
			id := hit.Doc.ID
			if id == "" {
				id = fmt.Sprintf("%s|%s", MetaString(hit.Doc, constant.MetaSource), hit.Doc.Content[:min(len(hit.Doc.Content), 64)])
			}
			a, ok := byID[id]
			if !ok {
				clone := *hit.Doc
				a = &acc{doc: &clone}
				byID[id] = a
				order = append(order, id)
			}
			a.score += 1.0 / (h.rrfK + float64(rank+1))
		}
	}

	out := make([]*schema.Document, 0, len(order))
	for _, id := range order {
		d := byID[id].doc
		d.WithScore(byID[id].score)
		out = append(out, d)
	}
	// 按融合分数降序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score() > out[j-1].Score(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
