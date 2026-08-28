package vectorstore

import "context"

type Vector struct {
	ChunkID        int64
	Embedding      []float32
	EmbeddingModel string
}

type SearchResult struct {
	ChunkID int64
	Score   float64
}

type Store interface {
	EnsureCollection(ctx context.Context) error
	Upsert(ctx context.Context, vectors []Vector) error
	Delete(ctx context.Context, chunkIDs []int64) error
	Search(ctx context.Context, embedding []float32, limit int) ([]SearchResult, error)
	Ready(ctx context.Context) error
	Close(ctx context.Context) error
}
