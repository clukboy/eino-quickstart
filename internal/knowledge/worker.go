package knowledge

import (
	"context"
	"log"
	"time"
)

type IndexerWorker struct {
	indexer  *Indexer
	interval time.Duration
}

func NewIndexerWorker(
	indexer *Indexer,
	interval time.Duration,
) *IndexerWorker {
	return &IndexerWorker{
		indexer:  indexer,
		interval: interval,
	}
}

func (w *IndexerWorker) Run(ctx context.Context) {
	if w == nil || w.indexer == nil {
		return
	}

	// 启动时立即处理一次。
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

func (w *IndexerWorker) process(ctx context.Context) {
	result, err := w.indexer.ProcessPending(ctx)
	if err != nil {
		log.Printf(
			"knowledge indexer process failed: %v",
			err,
		)
		return
	}

	if result.Claimed == 0 {
		return
	}

	log.Printf(
		"knowledge indexer processed: claimed=%d completed=%d retried=%d failed=%d",
		result.Claimed,
		result.Completed,
		result.Retried,
		result.Failed,
	)
}
