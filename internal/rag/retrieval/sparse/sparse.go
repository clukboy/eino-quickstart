package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"eino-quickstart/internal/rag/retrieval"
)

type SparseSearcher interface {
	Search(ctx context.Context, query string, topK int) ([]*domain.Candidate, error)
}

type SparseRetriever struct {
	searcher SparseSearcher
}

func NewSparseRetriever(searcher SparseSearcher) *SparseRetriever {
	return &SparseRetriever{
		searcher: searcher,
	}
}

func (r *SparseRetriever) Name() string {
	return "sparse"
}

func (r *SparseRetriever) Retrieve(ctx context.Context, query domain.Query, options retrieval.Options) ([]*domain.Candidate, error) {
	return r.searcher.Search(
		ctx,
		query.Text,
		options.TopK,
	)
}
