package ingestion

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Parser interface {
	Parse(ctx context.Context, documents []*domain.Document) ([]*domain.Document, error)
}
