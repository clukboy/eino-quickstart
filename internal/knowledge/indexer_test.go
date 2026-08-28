package knowledge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"eino-quickstart/ent/vectoroutbox"
	"eino-quickstart/internal/knowledge/vectorstore"
)

func TestIndexerProcessesUpsertsAndDeletes(t *testing.T) {
	repository := &recordingIndexerRepository{
		items: []claimedOutbox{
			{
				ID:        1,
				ChunkID:   101,
				Operation: vectoroutbox.OperationUpsert,
				Attempts:  1,
				Chunk: &indexChunk{
					ID:             101,
					Content:        "first document",
					EmbeddingModel: "test-model",
				},
			},
			{
				ID:        2,
				ChunkID:   102,
				Operation: vectoroutbox.OperationDelete,
				Attempts:  1,
			},
		},
	}
	embedder := &recordingEmbedder{
		dimensions: 2,
		vectors:    [][]float32{{0.1, 0.2}},
	}
	store := &recordingVectorStore{}
	indexer := newTestIndexer(t, repository, embedder, store, 3)

	result, err := indexer.ProcessPending(context.Background())
	if err != nil {
		t.Fatalf("ProcessPending() error = %v", err)
	}
	if want := (IndexResult{Claimed: 2, Completed: 2}); result != want {
		t.Errorf("ProcessPending() result = %#v, want %#v", result, want)
	}
	if store.ensureCalls != 1 {
		t.Errorf("EnsureCollection calls = %d, want 1", store.ensureCalls)
	}
	if want := [][]string{{"first document"}}; !reflect.DeepEqual(embedder.inputs, want) {
		t.Errorf("Embed() inputs = %#v, want %#v", embedder.inputs, want)
	}
	wantVectors := []vectorstore.Vector{{
		ChunkID:        101,
		Embedding:      []float32{0.1, 0.2},
		EmbeddingModel: "test-model",
	}}
	if !reflect.DeepEqual(store.upserts, wantVectors) {
		t.Errorf("Upsert() vectors = %#v, want %#v", store.upserts, wantVectors)
	}
	if want := []int64{102}; !reflect.DeepEqual(store.deletes, want) {
		t.Errorf("Delete() IDs = %#v, want %#v", store.deletes, want)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(repository.completed, want) {
		t.Errorf("completed IDs = %v, want %v", repository.completed, want)
	}
}

func TestIndexerRetriesThenFailsVectorErrors(t *testing.T) {
	for name, attempts := range map[string]int{
		"retry": 1,
		"fail":  3,
	} {
		t.Run(name, func(t *testing.T) {
			repository := &recordingIndexerRepository{
				items: []claimedOutbox{testUpsertOutbox(attempts)},
			}
			embedder := &recordingEmbedder{
				dimensions: 2,
				vectors:    [][]float32{{0.1, 0.2}},
			}
			store := &recordingVectorStore{upsertErr: errors.New("vector unavailable")}
			indexer := newTestIndexer(t, repository, embedder, store, 3)

			result, err := indexer.ProcessPending(context.Background())
			if err != nil {
				t.Fatalf("ProcessPending() error = %v", err)
			}
			if attempts < 3 {
				if want := (IndexResult{Claimed: 1, Retried: 1}); result != want {
					t.Errorf("ProcessPending() result = %#v, want %#v", result, want)
				}
				if len(repository.retried) != 1 || len(repository.failed) != 0 {
					t.Errorf("retry calls = %#v, failed calls = %#v", repository.retried, repository.failed)
				}
				if got, want := repository.retried[0].availableAt, testIndexerNow.Add(time.Second); !got.Equal(want) {
					t.Errorf("retry available at = %v, want %v", got, want)
				}
			} else {
				if want := (IndexResult{Claimed: 1, Failed: 1}); result != want {
					t.Errorf("ProcessPending() result = %#v, want %#v", result, want)
				}
				if len(repository.retried) != 0 || len(repository.failed) != 1 {
					t.Errorf("retry calls = %#v, failed calls = %#v", repository.retried, repository.failed)
				}
			}
		})
	}
}

func TestIndexerDeletesMissingUpsertWithoutEmbedding(t *testing.T) {
	repository := &recordingIndexerRepository{
		items: []claimedOutbox{{
			ID:        7,
			ChunkID:   107,
			Operation: vectoroutbox.OperationUpsert,
			Attempts:  1,
		}},
	}
	embedder := &recordingEmbedder{dimensions: 2}
	store := &recordingVectorStore{}
	indexer := newTestIndexer(t, repository, embedder, store, 3)

	result, err := indexer.ProcessPending(context.Background())
	if err != nil {
		t.Fatalf("ProcessPending() error = %v", err)
	}
	if want := (IndexResult{Claimed: 1, Completed: 1}); result != want {
		t.Errorf("ProcessPending() result = %#v, want %#v", result, want)
	}
	if len(embedder.inputs) != 0 {
		t.Errorf("Embed() calls = %d, want 0", len(embedder.inputs))
	}
	if want := []int64{107}; !reflect.DeepEqual(store.deletes, want) {
		t.Errorf("Delete() IDs = %#v, want %#v", store.deletes, want)
	}
}

func TestIndexerDoesNotClaimWhenCollectionSetupFails(t *testing.T) {
	repository := &recordingIndexerRepository{}
	indexer := newTestIndexer(
		t,
		repository,
		&recordingEmbedder{dimensions: 2},
		&recordingVectorStore{ensureErr: errors.New("unavailable")},
		3,
	)

	_, err := indexer.ProcessPending(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ensure vector collection") {
		t.Errorf("ProcessPending() error = %v, want collection setup error", err)
	}
	if repository.claimCalls != 0 {
		t.Errorf("Claim() calls = %d, want 0", repository.claimCalls)
	}
}

func TestIndexerRetryAndErrorBounds(t *testing.T) {
	indexer := newTestIndexer(
		t,
		&recordingIndexerRepository{},
		&recordingEmbedder{dimensions: 2},
		&recordingVectorStore{},
		5,
	)
	if got, want := indexer.retryDelay(1), time.Second; got != want {
		t.Errorf("retryDelay(1) = %v, want %v", got, want)
	}
	if got, want := indexer.retryDelay(2), 2*time.Second; got != want {
		t.Errorf("retryDelay(2) = %v, want %v", got, want)
	}
	if got, want := indexer.retryDelay(10), 4*time.Second; got != want {
		t.Errorf("retryDelay(10) = %v, want %v", got, want)
	}

	message := boundedError(errors.New(strings.Repeat("界", 2000)))
	if len(message) > maxOutboxErrorBytes || !utf8.ValidString(message) {
		t.Errorf("boundedError() returned invalid bounded string of %d bytes", len(message))
	}
}

var testIndexerNow = time.Date(2026, 8, 28, 3, 9, 25, 0, time.UTC)

func newTestIndexer(
	t *testing.T,
	repository indexerRepository,
	embedder *recordingEmbedder,
	store *recordingVectorStore,
	maxAttempts int,
) *Indexer {
	t.Helper()
	indexer, err := newIndexer(IndexerConfig{
		Embedder:          embedder,
		VectorStore:       store,
		BatchSize:         10,
		LeaseDuration:     time.Minute,
		MaxAttempts:       maxAttempts,
		InitialRetryDelay: time.Second,
		MaxRetryDelay:     4 * time.Second,
	}, repository)
	if err != nil {
		t.Fatalf("newIndexer() error = %v", err)
	}
	indexer.now = func() time.Time { return testIndexerNow }
	return indexer
}

func testUpsertOutbox(attempts int) claimedOutbox {
	return claimedOutbox{
		ID:        1,
		ChunkID:   101,
		Operation: vectoroutbox.OperationUpsert,
		Attempts:  attempts,
		Chunk: &indexChunk{
			ID:             101,
			Content:        "first document",
			EmbeddingModel: "test-model",
		},
	}
}

type recordingIndexerRepository struct {
	items      []claimedOutbox
	claimCalls int
	completed  []int
	retried    []retryCall
	failed     []failureCall
}

type retryCall struct {
	id          int
	availableAt time.Time
	lastError   string
}

type failureCall struct {
	id        int
	lastError string
}

func (r *recordingIndexerRepository) Claim(
	_ context.Context,
	_ int,
	_ time.Time,
	_ time.Duration,
) ([]claimedOutbox, error) {
	r.claimCalls++
	return append([]claimedOutbox(nil), r.items...), nil
}

func (r *recordingIndexerRepository) Complete(
	_ context.Context,
	item claimedOutbox,
	_ time.Time,
) (bool, error) {
	r.completed = append(r.completed, item.ID)
	return true, nil
}

func (r *recordingIndexerRepository) Retry(
	_ context.Context,
	item claimedOutbox,
	availableAt time.Time,
	lastError string,
) (bool, error) {
	r.retried = append(r.retried, retryCall{
		id:          item.ID,
		availableAt: availableAt,
		lastError:   lastError,
	})
	return true, nil
}

func (r *recordingIndexerRepository) Fail(
	_ context.Context,
	item claimedOutbox,
	lastError string,
) (bool, error) {
	r.failed = append(r.failed, failureCall{id: item.ID, lastError: lastError})
	return true, nil
}

type recordingEmbedder struct {
	dimensions int
	vectors    [][]float32
	err        error
	inputs     [][]string
}

func (e *recordingEmbedder) Dimensions() int {
	return e.dimensions
}

func (e *recordingEmbedder) Embed(
	_ context.Context,
	texts []string,
) ([][]float32, error) {
	e.inputs = append(e.inputs, append([]string(nil), texts...))
	if e.err != nil {
		return nil, e.err
	}
	return append([][]float32(nil), e.vectors...), nil
}

type recordingVectorStore struct {
	ensureCalls int
	ensureErr   error
	upserts     []vectorstore.Vector
	upsertErr   error
	deletes     []int64
	deleteErr   error
}

func (s *recordingVectorStore) EnsureCollection(_ context.Context) error {
	s.ensureCalls++
	return s.ensureErr
}

func (s *recordingVectorStore) Upsert(
	_ context.Context,
	vectors []vectorstore.Vector,
) error {
	s.upserts = append(s.upserts, vectors...)
	return s.upsertErr
}

func (s *recordingVectorStore) Delete(
	_ context.Context,
	chunkIDs []int64,
) error {
	s.deletes = append(s.deletes, chunkIDs...)
	return s.deleteErr
}

func (*recordingVectorStore) Search(
	_ context.Context,
	_ []float32,
	_ int,
) ([]vectorstore.SearchResult, error) {
	return nil, nil
}

func (*recordingVectorStore) Ready(context.Context) error {
	return nil
}

func (*recordingVectorStore) Close(context.Context) error {
	return nil
}
