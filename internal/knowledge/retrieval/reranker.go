package retrieval

import "context"

type Reranker interface {
	Rerank(ctx context.Context, query string, results []Result) ([]Result, error)
}
