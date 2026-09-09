package indexing

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"fmt"
)

type CompositeIndexer struct {
	indexers []Indexer
}

func NewCompositeIndexer(indexers ...Indexer) *CompositeIndexer {
	return &CompositeIndexer{
		indexers: indexers,
	}
}

func (i *CompositeIndexer) Index(ctx context.Context, chunks []*domain.Chunk) error {
	for _, indexer := range i.indexers {
		if err := indexer.Index(ctx, chunks); err != nil {
			return fmt.Errorf(
				"indexer %s failed: %w",
				indexer.Name(),
				err,
			)
		}
	}

	return nil
}

func (i *CompositeIndexer) Delete(ctx context.Context, ids []string) error {
	for _, indexer := range i.indexers {
		if err := indexer.Delete(ctx, ids); err != nil {
			return fmt.Errorf(
				"indexer %s delete failed: %w",
				indexer.Name(),
				err,
			)
		}
	}

	return nil
}
