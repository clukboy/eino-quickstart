package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"eino-quickstart/internal/rag/retrieval"
)

type ExactSearcher interface {
	Search(ctx context.Context, entities []domain.Entity, topK int) ([]*domain.Candidate, error)
}

type ExactRetriever struct {
	searcher ExactSearcher
}

func NewExactRetriever(searcher ExactSearcher) *ExactRetriever {
	return &ExactRetriever{
		searcher: searcher,
	}
}

func (r *ExactRetriever) Name() string {
	return "exact"
}

func (r *ExactRetriever) Retrieve(ctx context.Context, query domain.Query, options retrieval.Options) ([]*domain.Candidate, error) {
	return r.searcher.Search(ctx, query.Signals.Entities, options.TopK)
}
