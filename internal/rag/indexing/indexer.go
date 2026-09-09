package indexing

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Indexer interface {
	Name() string
	Index(ctx context.Context, chunks []*domain.Chunk) error
	Delete(ctx context.Context, ids []string) error
}
