package worker

import (
	"context"
	"errors"
	"log"
	"time"

	"eino-quickstart/internal/rag/indexing"
)

type IndexingWorker struct {
	indexer  indexing.Indexer
	interval time.Duration
}

func NewIndexingWorker(indexer indexing.Indexer, interval time.Duration) (*IndexingWorker, error) {
	if indexer == nil {
		return nil, errors.New("indexer is required")
	}

	if interval <= 0 {
		return nil, errors.New("index interval must be greater than zero")
	}

	return &IndexingWorker{
		indexer:  indexer,
		interval: interval,
	}, nil
}

func (w *IndexingWorker) Run(ctx context.Context) {
	if w == nil || w.indexer == nil {
		return
	}
	w.process(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *IndexingWorker) process(ctx context.Context) {
	if err := w.indexer.Index(ctx, nil); err != nil {
		log.Printf("rag indexing failed: %v", err)
	}
}
