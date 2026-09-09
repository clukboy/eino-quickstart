package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"eino-quickstart/internal/rag/retrieval"
)

type KeywordSearcher interface {
	Search(ctx context.Context, terms []string, topK int) ([]*domain.Candidate, error)
}

type KeywordRetriever struct {
	searcher KeywordSearcher
}

func NewKeywordRetriever(searcher KeywordSearcher) *KeywordRetriever {
	return &KeywordRetriever{
		searcher: searcher,
	}
}

func (r *KeywordRetriever) Name() string {
	return "keyword"
}

func (r *KeywordRetriever) Retrieve(ctx context.Context, query domain.Query, options retrieval.Options) ([]*domain.Candidate, error) {
	return r.searcher.Search(ctx, query.Signals.Terms, options.TopK)
}
