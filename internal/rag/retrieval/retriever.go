package retrieval

import (
	"context"

	"eino-quickstart/internal/rag/domain"
)

type Retriever interface {
	Name() string
	Retrieve(ctx context.Context, query domain.Query, options Options) ([]*domain.Candidate, error)
}
