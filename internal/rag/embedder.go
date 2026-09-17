package rag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
)

const defaultMaxTextsPerRequest = 10

// Embedder 把 StringEmbedder 适配为 eino embedding.Embedder。
type Embedder struct {
	inner  embedding.Embedder
	config *openai.EmbeddingConfig

	// maxTextsPerRequest 是切分请求的粒度，见 defaultMaxTextsPerRequest。
	maxTextsPerRequest int
}

// EmbedderOption 覆盖 Embedder 的默认行为。
type EmbedderOption func(*Embedder)

// WithMaxTextsPerRequest 设置单次请求最多带几段文本（<=0 时忽略）。
//
// 换 embedding 服务时用它：不同服务方的上限不一样，接入时确认一次即可，
// 比在调用方到处调 batchSize 可靠。
func WithMaxTextsPerRequest(n int) EmbedderOption {
	return func(e *Embedder) {
		if n > 0 {
			e.maxTextsPerRequest = n
		}
	}
}

func NewEmbedder(ctx context.Context, config *openai.EmbeddingConfig, opts ...EmbedderOption) (*Embedder, error) {
	inner, err := openai.NewEmbedder(ctx, config)
	if err != nil {
		return nil, err
	}
	embedder := &Embedder{
		inner:              inner,
		config:             config,
		maxTextsPerRequest: defaultMaxTextsPerRequest,
	}
	for _, apply := range opts {
		apply(embedder)
	}
	return embedder, nil
}

// EmbedStrings 按服务方允许的上限切分请求，并把结果按原顺序拼回来。
//
// 切分对调用方完全透明：入参与返回值仍然是一一对应的完整切片，调用方不需要
// 知道中间发了几次请求。这样「一次能带多少」就只是 Embedder 的内部约束，
// 上层怎么分批都不会踩到服务方的上限。
func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 维度只在第一次拿到向量后读出来做校验，配置里没写就跳过。
	var dim int
	if e.config.Dimensions != nil {
		dim = *e.config.Dimensions
	}

	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += e.maxTextsPerRequest {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + e.maxTextsPerRequest
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		vecs, err := e.inner.EmbedStrings(ctx, batch)
		if err != nil {
			// 带上这一段在整批里的位置：一整篇文档可能有几十个分块、分几次请求，
			// 只报 len(texts) 的话不知道是哪一段触发的（比如某一段超长）。
			return nil, fmt.Errorf("rag: embed texts [%d:%d) of %d: %w", start, end, len(texts), err)
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf(
				"rag: embed returned %d vectors for %d texts (batch [%d:%d) of %d)",
				len(vecs), len(batch), start, end, len(texts),
			)
		}

		for i, v := range vecs {
			if dim > 0 && len(v) != dim {
				return nil, fmt.Errorf(
					"rag: vector %d dim=%d, want %d", start+i, len(v), dim,
				)
			}
			f64 := make([]float64, len(v))
			for j, x := range v {
				f64[j] = float64(x)
			}
			out = append(out, f64)
		}
	}
	return out, nil
}
