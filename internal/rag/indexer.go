package rag

import (
	"context"
	"eino-quickstart/internal/rag/constant"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/schema"
)

// Indexer 负责 chunk → 向量 → Store。
// [优化] 1) 按 source 先删后插，重复 ingest 幂等；
// [优化] 2) embedding 分批调用，避免超长请求被服务端拒绝；
// [优化] 3) 结构化日志与错误包装。
type Indexer struct {
	embedder  *Embedder
	store     *Store
	batchSize int
	logger    *slog.Logger
}

func NewIndexer(embedder *Embedder, store *Store) *Indexer {
	return &Indexer{embedder: embedder, store: store, batchSize: 32, logger: slog.Default()}
}

func (i *Indexer) WithBatchSize(n int) *Indexer {
	if n > 0 {
		i.batchSize = n
	}
	return i
}

func (i *Indexer) Index(ctx context.Context, docs []*schema.Document) ([]*schema.Document, error) {
	if len(docs) == 0 {
		return docs, nil
	}

	// [优化] 幂等：同一 source 重新入库前先清理旧向量
	source := MetaString(docs[0], constant.MetaSource)
	if source != "" {
		if err := i.store.DeleteBySource(ctx, source); err != nil {
			i.logger.Warn("delete old chunks failed", "source", source, "err", err)
			// 不中断：宁可多插也不失败整个流程（可按需改成硬错误）
		}
	}

	texts := make([]string, len(docs))
	for k, d := range docs {
		texts[k] = d.Content
	}

	allVecs := make([][]float64, 0, len(docs))
	for start := 0; start < len(texts); start += i.batchSize {
		end := min(start+i.batchSize, len(texts))
		vecs, err := i.embedder.EmbedStrings(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("rag: embed batch [%d:%d]: %w", start, end, err)
		}
		if len(vecs) != end-start {
			return nil, fmt.Errorf("rag: embed batch size mismatch: %d != %d", len(vecs), end-start)
		}
		allVecs = append(allVecs, vecs...)
	}

	if err := i.store.Add(ctx, docs, allVecs); err != nil {
		return nil, fmt.Errorf("rag: store add: %w", err)
	}
	i.logger.Info("indexed", "source", source, "chunks", len(docs))
	return docs, nil
}
