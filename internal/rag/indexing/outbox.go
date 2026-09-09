package indexing

import "time"

type OutboxIndexer struct {
	repository  OutboxRepository
	embedder    Embedder
	vectorStore VectorStore

	batchSize         int
	leaseDuration     time.Duration
	maxAttempts       int
	initialRetryDelay time.Duration
	maxRetryDelay     time.Duration
}
