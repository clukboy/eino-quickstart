package governance

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type Deduplicator interface {
	Deduplicate(ctx context.Context, chunks []*domain.Chunk) ([]*domain.Chunk, error)
}
