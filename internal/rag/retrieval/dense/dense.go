package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"eino-quickstart/internal/rag/retrieval"
)

type DenseSearcher interface {
	Search(ctx context.Context, query string, topK int) ([]*domain.Candidate, error)
}

type DenseRetriever struct {
	searcher DenseSearcher
}

func NewDenseRetriever(searcher DenseSearcher) *DenseRetriever {
	return &DenseRetriever{
		searcher: searcher,
	}
}

func (r *DenseRetriever) Name() string {
	return "dense"
}

func (r *DenseRetriever) Retrieve(ctx context.Context, query domain.Query, options retrieval.Options) ([]*domain.Candidate, error) {
	return r.searcher.Search(ctx, query.Text, options.TopK)
}
