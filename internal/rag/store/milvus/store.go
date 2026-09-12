package milvus

import "context"

type SearchResult struct {
	ChunkID int64
	Score   float64
}

type Store interface {
	EnsureCollection(ctx context.Context) error
	Upsert(ctx context.Context, chunkIDs []int64, vectors [][]float32) error
	Delete(ctx context.Context, chunkIDs []int64) error
	Search(ctx context.Context, embedding []float32, limit int) ([]SearchResult, error)
	Ready(ctx context.Context) error
	Close(ctx context.Context) error
}
