package ingestion

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Transformer interface {
	Transform(ctx context.Context, documents []*domain.Document) ([]*domain.Document, error)
}
