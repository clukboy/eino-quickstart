package knowledge

import (
	"context"
	"testing"
	"time"
)

func TestIndexerWorkerStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cancel()

	worker := &IndexerWorker{
		indexer:  nil,
		interval: time.Millisecond,
	}

	done := make(chan struct{})

	go func() {
		worker.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after context cancellation")
	}
}
