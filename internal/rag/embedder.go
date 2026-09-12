package rag

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/embedding"
)

// Embedder 把 StringEmbedder 适配为 eino embedding.Embedder。
type Embedder struct {
	inner  embedding.Embedder
	config *openai.EmbeddingConfig
}

func NewEmbedder(ctx context.Context, config *openai.EmbeddingConfig) (*Embedder, error) {
	embedder, err := openai.NewEmbedder(ctx, config)
	if err != nil {
		return nil, err
	}
	return &Embedder{inner: embedder, config: config}, nil
}

func (e *Embedder) EmbedStrings(ctx context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {

	if len(texts) == 0 {
		return nil, nil
	}
	vecs, err := e.inner.EmbedStrings(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("rag: embed: %w", err)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("rag: embed returned %d vectors for %d texts", len(vecs), len(texts))
	}
	// [优化] 维度校验集中在这里，indexer/retriever 不再关心
	var dim int
	if e.config.Dimensions != nil {
		dim = *e.config.Dimensions
	}
	if dim > 0 {
		for i, v := range vecs {
			if len(v) != dim {
				return nil, fmt.Errorf("rag: vector %d dim=%d, want %d", i, len(v), dim)
			}
		}
	}
	out := make([][]float64, len(vecs))
	for i, v := range vecs {
		f64 := make([]float64, len(v))
		for j, x := range v {
			f64[j] = float64(x)
		}
		out[i] = f64
	}
	return out, nil
}
