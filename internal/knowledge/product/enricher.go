package product

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type Enricher struct {
	Catalog Catalog
}

func (e *Enricher) Name() string {
	return "product"
}

func (e *Enricher) Enrich(ctx context.Context, document *domain.Document, chunks []*domain.Chunk) error {

	// 根据文档内容解析 Product

	return nil
}
