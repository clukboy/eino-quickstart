package indexing

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

type VectorStore interface {
	Upsert(ctx context.Context, chunks []*domain.Chunk, vectors [][]float64) error
	Delete(ctx context.Context, ids []string) error
}

type DenseIndexer struct {
	embedder Embedder
	store    VectorStore
}

func NewDenseIndexer(embedder Embedder, store VectorStore) *DenseIndexer {
	return &DenseIndexer{
		embedder: embedder,
		store:    store,
	}
}

func (i *DenseIndexer) Name() string {
	return "dense"
}

func (i *DenseIndexer) Index(ctx context.Context, chunks []*domain.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	texts := make([]string, 0, len(chunks))

	for _, chunk := range chunks {
		texts = append(texts, chunk.Content)
	}

	vectors, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}

	return i.store.Upsert(ctx, chunks, vectors)
}

func (i *DenseIndexer) Delete(ctx context.Context, ids []string) error {
	return i.store.Delete(ctx, ids)
}
