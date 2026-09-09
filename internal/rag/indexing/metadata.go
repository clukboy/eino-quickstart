package indexing

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type MetadataStore interface {
	Index(ctx context.Context, chunks []*domain.Chunk) error
	Delete(ctx context.Context, ids []string) error
}

type MetadataIndexer struct {
	Store MetadataStore
}

func (i *MetadataIndexer) Name() string {
	return "metadata"
}
