package metadata

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type MetadataSearcher interface {
	Search(ctx context.Context, filters []domain.Filter, topK int) ([]*domain.Candidate, error)
}

type MetadataRetriever struct {
	Searcher MetadataSearcher
}

func (r *MetadataRetriever) Name() string {
	return "metadata"
}
