package indexing

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type SparseStore interface {
	Index(ctx context.Context, chunks []*domain.Chunk) error
	Delete(ctx context.Context, ids []string) error
}

type SparseIndexer struct {
	store SparseStore
}

func NewSparseIndexer(store SparseStore) *SparseIndexer {
	return &SparseIndexer{
		store: store,
	}
}

func (i *SparseIndexer) Name() string {
	return "sparse"
}

func (i *SparseIndexer) Index(ctx context.Context, chunks []*domain.Chunk) error {
	return i.store.Index(ctx, chunks)
}

func (i *SparseIndexer) Delete(ctx context.Context, ids []string) error {
	return i.store.Delete(ctx, ids)
}
