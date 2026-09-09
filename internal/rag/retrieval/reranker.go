package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type Reranker interface {
	Rerank(ctx context.Context, query domain.Query, candidates []*domain.Candidate) ([]*domain.Candidate, error)
}
