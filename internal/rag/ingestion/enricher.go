package ingestion

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Enricher interface {
	Name() string

	Enrich(ctx context.Context, document *domain.Document, chunks []*domain.Chunk) error
}
