package ingestion

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Chunker interface {
	Chunk(ctx context.Context, document *domain.Document) ([]*domain.Chunk, error)
}
