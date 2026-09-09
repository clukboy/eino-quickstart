package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
)

type Router interface {
	Route(ctx context.Context, query domain.Query) ([]string, error)
}
