package knowledge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/vectoroutbox"
	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/vectorstore"
)

const maxOutboxErrorBytes = 4096

// IndexerConfig configures transactional vector-outbox processing.
type IndexerConfig struct {
	Client            *ent.Client
	Embedder          embedding.Embedder
	VectorStore       vectorstore.Store
	BatchSize         int
	LeaseDuration     time.Duration
	MaxAttempts       int
	InitialRetryDelay time.Duration
	MaxRetryDelay     time.Duration
}

// IndexResult reports the outcomes from one ProcessPending call.
type IndexResult struct {
	Claimed   int
	Completed int
	Retried   int
	Failed    int
}

// Indexer moves durable vector-outbox records into a vector store. Vector
// mutations are at-least-once: an expired lease or failed completion may
// repeat an idempotent upsert or delete.
type Indexer struct {
	repository  indexerRepository
	embedder    embedding.Embedder
	vectorStore vectorstore.Store

	batchSize         int
	leaseDuration     time.Duration
	maxAttempts       int
	initialRetryDelay time.Duration
	maxRetryDelay     time.Duration
	now               func() time.Time

	ensureMu sync.Mutex
	ensured  bool
}

type indexerRepository interface {
	Claim(
		ctx context.Context,
		limit int,
		now time.Time,
		leaseDuration time.Duration,
	) ([]claimedOutbox, error)
	Complete(ctx context.Context, item claimedOutbox, at time.Time) (bool, error)
	Retry(
		ctx context.Context,
		item claimedOutbox,
		availableAt time.Time,
		lastError string,
	) (bool, error)
	Fail(
		ctx context.Context,
		item claimedOutbox,
		lastError string,
	) (bool, error)
}

type claimedOutbox struct {
	ID          int
	ChunkID     int64
	Operation   vectoroutbox.Operation
	Attempts    int
	LockedUntil time.Time
	Chunk       *indexChunk
}

type indexChunk struct {
	ID             int
	Content        string
	EmbeddingModel string
}

// NewIndexer creates an outbox indexer backed by an Ent client.
func NewIndexer(config IndexerConfig) (*Indexer, error) {
	if config.Client == nil {
		return nil, errors.New("knowledge ent client is required")
	}
	return newIndexer(config, &entIndexerRepository{client: config.Client})
}

func newIndexer(
	config IndexerConfig,
	repository indexerRepository,
) (*Indexer, error) {
	if repository == nil {
		return nil, errors.New("knowledge indexer repository is required")
	}
	if config.Embedder == nil {
		return nil, errors.New("knowledge embedder is required")
	}
	if config.Embedder.Dimensions() <= 0 {
		return nil, errors.New(
			"knowledge embedder dimensions must be greater than zero",
		)
	}
	if config.VectorStore == nil {
		return nil, errors.New("knowledge vector store is required")
	}
	if config.BatchSize <= 0 {
		return nil, errors.New("knowledge index batch size must be greater than zero")
	}
	if config.LeaseDuration <= 0 {
		return nil, errors.New("knowledge index lease duration must be greater than zero")
	}
	if config.MaxAttempts <= 0 {
		return nil, errors.New("knowledge index maximum attempts must be greater than zero")
	}
	if config.InitialRetryDelay <= 0 {
		return nil, errors.New("knowledge initial retry delay must be greater than zero")
	}
	if config.MaxRetryDelay < config.InitialRetryDelay {
		return nil, errors.New(
			"knowledge maximum retry delay must be at least the initial retry delay",
		)
	}

	return &Indexer{
		repository:        repository,
		embedder:          config.Embedder,
		vectorStore:       config.VectorStore,
		batchSize:         config.BatchSize,
		leaseDuration:     config.LeaseDuration,
		maxAttempts:       config.MaxAttempts,
		initialRetryDelay: config.InitialRetryDelay,
		maxRetryDelay:     config.MaxRetryDelay,
		now:               time.Now,
	}, nil
}

// ProcessPending ensures the vector collection exists and processes one batch
// of pending or expired-lease outbox records. Operational vector failures are
// persisted for retry and therefore do not stop a long-running caller.
func (i *Indexer) ProcessPending(ctx context.Context) (IndexResult, error) {
	if i == nil {
		return IndexResult{}, errors.New("knowledge indexer is nil")
	}
	if ctx == nil {
		return IndexResult{}, errors.New("knowledge indexing context is required")
	}
	if err := i.ensureCollection(ctx); err != nil {
		return IndexResult{}, err
	}

	now := indexerTime(i.now())
	items, err := i.repository.Claim(ctx, i.batchSize, now, i.leaseDuration)
	if err != nil {
		return IndexResult{}, fmt.Errorf("claim vector outbox records: %w", err)
	}
	result := IndexResult{Claimed: len(items)}

	upserts := make([]claimedOutbox, 0, len(items))
	deletes := make([]claimedOutbox, 0, len(items))
	for _, item := range items {
		switch item.Operation {
		case vectoroutbox.OperationUpsert:
			if item.Chunk == nil {
				deletes = append(deletes, item)
				continue
			}
			upserts = append(upserts, item)
		case vectoroutbox.OperationDelete:
			deletes = append(deletes, item)
		default:
			if err := i.handleFailure(
				ctx,
				item,
				fmt.Errorf("unsupported vector outbox operation %q", item.Operation),
				now,
				&result,
			); err != nil {
				return result, err
			}
		}
	}

	if err := i.processUpserts(ctx, upserts, now, &result); err != nil {
		return result, err
	}
	if err := i.processDeletes(ctx, deletes, now, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (i *Indexer) ensureCollection(ctx context.Context) error {
	i.ensureMu.Lock()
	defer i.ensureMu.Unlock()
	if i.ensured {
		return nil
	}
	if err := i.vectorStore.EnsureCollection(ctx); err != nil {
		return fmt.Errorf("ensure vector collection: %w", err)
	}
	i.ensured = true
	return nil
}

func (i *Indexer) processUpserts(
	ctx context.Context,
	items []claimedOutbox,
	now time.Time,
	result *IndexResult,
) error {
	if len(items) == 0 {
		return nil
	}

	texts := make([]string, len(items))
	for position, item := range items {
		texts[position] = item.Chunk.Content
	}
	embeddings, err := i.embedder.Embed(ctx, texts)
	if err == nil {
		err = i.validateEmbeddings(embeddings, len(items))
	}
	if err == nil {
		vectors := make([]vectorstore.Vector, len(items))
		for position, item := range items {
			vectors[position] = vectorstore.Vector{
				ChunkID:        item.ChunkID,
				Embedding:      append([]float32(nil), embeddings[position]...),
				EmbeddingModel: item.Chunk.EmbeddingModel,
			}
		}
		err = i.vectorStore.Upsert(ctx, vectors)
	}
	if err != nil {
		for _, item := range items {
			if failureErr := i.handleFailure(ctx, item, err, now, result); failureErr != nil {
				return failureErr
			}
		}
		return nil
	}

	for _, item := range items {
		if err := i.complete(ctx, item, now, result); err != nil {
			return err
		}
	}
	return nil
}

func (i *Indexer) processDeletes(
	ctx context.Context,
	items []claimedOutbox,
	now time.Time,
	result *IndexResult,
) error {
	if len(items) == 0 {
		return nil
	}

	chunkIDs := make([]int64, len(items))
	for position, item := range items {
		chunkIDs[position] = item.ChunkID
	}
	if err := i.vectorStore.Delete(ctx, chunkIDs); err != nil {
		for _, item := range items {
			if failureErr := i.handleFailure(ctx, item, err, now, result); failureErr != nil {
				return failureErr
			}
		}
		return nil
	}

	for _, item := range items {
		if err := i.complete(ctx, item, now, result); err != nil {
			return err
		}
	}
	return nil
}

func (i *Indexer) validateEmbeddings(
	embeddings [][]float32,
	count int,
) error {
	if len(embeddings) != count {
		return fmt.Errorf(
			"embedding response count %d does not match requested count %d",
			len(embeddings),
			count,
		)
	}
	for position, values := range embeddings {
		if len(values) != i.embedder.Dimensions() {
			return fmt.Errorf(
				"embedding %d dimensions %d do not match configured dimensions %d",
				position,
				len(values),
				i.embedder.Dimensions(),
			)
		}
		for _, value := range values {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return fmt.Errorf("embedding %d contains a non-finite value", position)
			}
		}
	}
	return nil
}

func (i *Indexer) complete(
	ctx context.Context,
	item claimedOutbox,
	now time.Time,
	result *IndexResult,
) error {
	completed, err := i.repository.Complete(ctx, item, now)
	if err != nil {
		return fmt.Errorf("complete vector outbox record %d: %w", item.ID, err)
	}
	if completed {
		result.Completed++
	}
	return nil
}

func (i *Indexer) handleFailure(
	ctx context.Context,
	item claimedOutbox,
	cause error,
	now time.Time,
	result *IndexResult,
) error {
	lastError := boundedError(cause)
	if item.Attempts >= i.maxAttempts {
		failed, err := i.repository.Fail(ctx, item, lastError)
		if err != nil {
			return fmt.Errorf("fail vector outbox record %d: %w", item.ID, err)
		}
		if failed {
			result.Failed++
		}
		return nil
	}

	retried, err := i.repository.Retry(
		ctx,
		item,
		now.Add(i.retryDelay(item.Attempts)),
		lastError,
	)
	if err != nil {
		return fmt.Errorf("retry vector outbox record %d: %w", item.ID, err)
	}
	if retried {
		result.Retried++
	}
	return nil
}

func (i *Indexer) retryDelay(attempts int) time.Duration {
	delay := i.initialRetryDelay
	for attempt := 1; attempt < attempts && delay < i.maxRetryDelay; attempt++ {
		if delay > i.maxRetryDelay/2 {
			return i.maxRetryDelay
		}
		delay *= 2
	}
	if delay > i.maxRetryDelay {
		return i.maxRetryDelay
	}
	return delay
}

func boundedError(err error) string {
	if err == nil {
		return "vector indexing failed"
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return "vector indexing failed"
	}
	if len(message) <= maxOutboxErrorBytes {
		return message
	}
	message = message[:maxOutboxErrorBytes]
	for !utf8.ValidString(message) {
		message = message[:len(message)-1]
	}
	return message
}

func indexerTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

type entIndexerRepository struct {
	client *ent.Client
}

func (r *entIndexerRepository) Claim(
	ctx context.Context,
	limit int,
	now time.Time,
	leaseDuration time.Duration,
) ([]claimedOutbox, error) {
	candidates, err := r.client.VectorOutbox.Query().
		Where(vectoroutbox.Or(
			vectoroutbox.And(
				vectoroutbox.StatusEQ(vectoroutbox.StatusPending),
				vectoroutbox.AvailableAtLTE(now),
			),
			vectoroutbox.And(
				vectoroutbox.StatusEQ(vectoroutbox.StatusProcessing),
				vectoroutbox.Or(
					vectoroutbox.LockedUntilIsNil(),
					vectoroutbox.LockedUntilLTE(now),
				),
			),
		)).
		Order(vectoroutbox.ByAvailableAt(), vectoroutbox.ByID()).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	claimed := make([]claimedOutbox, 0, len(candidates))
	leaseUntil := indexerTime(now.Add(leaseDuration))
	for _, candidate := range candidates {
		affected, err := r.client.VectorOutbox.Update().
			Where(
				vectoroutbox.IDEQ(candidate.ID),
				vectoroutbox.Or(
					vectoroutbox.And(
						vectoroutbox.StatusEQ(vectoroutbox.StatusPending),
						vectoroutbox.AvailableAtLTE(now),
					),
					vectoroutbox.And(
						vectoroutbox.StatusEQ(vectoroutbox.StatusProcessing),
						vectoroutbox.Or(
							vectoroutbox.LockedUntilIsNil(),
							vectoroutbox.LockedUntilLTE(now),
						),
					),
				),
			).
			SetStatus(vectoroutbox.StatusProcessing).
			AddAttempts(1).
			SetLockedUntil(leaseUntil).
			ClearLastError().
			Save(ctx)
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			continue
		}

		outbox, err := r.client.VectorOutbox.Query().
			Where(
				vectoroutbox.IDEQ(candidate.ID),
				vectoroutbox.StatusEQ(vectoroutbox.StatusProcessing),
				vectoroutbox.LockedUntilEQ(leaseUntil),
			).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		item := claimedOutbox{
			ID:          outbox.ID,
			ChunkID:     outbox.ChunkID,
			Operation:   outbox.Operation,
			Attempts:    outbox.Attempts,
			LockedUntil: leaseUntil,
		}
		if item.Operation == vectoroutbox.OperationUpsert {
			chunk, err := r.client.DocumentChunk.Query().
				Where(documentchunk.IDEQ(int(item.ChunkID))).
				Only(ctx)
			if err != nil {
				if ent.IsNotFound(err) {
					claimed = append(claimed, item)
					continue
				}
				return nil, err
			}
			item.Chunk = &indexChunk{
				ID:             chunk.ID,
				Content:        chunk.Content,
				EmbeddingModel: chunk.EmbeddingModel,
			}
		}
		claimed = append(claimed, item)
	}
	return claimed, nil
}

func (r *entIndexerRepository) Complete(
	ctx context.Context,
	item claimedOutbox,
	at time.Time,
) (completed bool, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	client := tx.Client()
	var chunk *ent.DocumentChunk
	var doc *ent.Document
	if item.Operation == vectoroutbox.OperationUpsert {
		chunk, doc, err = lockChunkDocument(ctx, client, item.ChunkID)
		if err != nil {
			return false, err
		}
	}
	affected, err := client.VectorOutbox.Update().
		Where(
			vectoroutbox.IDEQ(item.ID),
			vectoroutbox.StatusEQ(vectoroutbox.StatusProcessing),
			vectoroutbox.LockedUntilEQ(item.LockedUntil),
		).
		SetStatus(vectoroutbox.StatusDone).
		ClearLockedUntil().
		ClearLastError().
		Save(ctx)
	if err != nil {
		return false, err
	}
	if affected == 0 {
		if err := tx.Rollback(); err != nil {
			return false, fmt.Errorf("rollback unowned vector outbox record: %w", err)
		}
		return false, nil
	}

	if chunk != nil {
		if err := markChunkIndexed(ctx, client, chunk, doc, at); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *entIndexerRepository) Retry(
	ctx context.Context,
	item claimedOutbox,
	availableAt time.Time,
	lastError string,
) (retried bool, err error) {
	return r.updateFailureState(
		ctx,
		item,
		vectoroutbox.StatusPending,
		availableAt,
		lastError,
	)
}

func (r *entIndexerRepository) Fail(
	ctx context.Context,
	item claimedOutbox,
	lastError string,
) (failed bool, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	client := tx.Client()
	var chunk *ent.DocumentChunk
	var doc *ent.Document
	if item.Operation == vectoroutbox.OperationUpsert {
		chunk, doc, err = lockChunkDocument(ctx, client, item.ChunkID)
		if err != nil {
			return false, err
		}
	}
	affected, err := client.VectorOutbox.Update().
		Where(
			vectoroutbox.IDEQ(item.ID),
			vectoroutbox.StatusEQ(vectoroutbox.StatusProcessing),
			vectoroutbox.LockedUntilEQ(item.LockedUntil),
		).
		SetStatus(vectoroutbox.StatusFailed).
		ClearLockedUntil().
		SetLastError(lastError).
		Save(ctx)
	if err != nil {
		return false, err
	}
	if affected == 0 {
		if err := tx.Rollback(); err != nil {
			return false, fmt.Errorf("rollback unowned vector outbox record: %w", err)
		}
		return false, nil
	}

	if chunk != nil {
		if err := markChunkFailed(ctx, chunk, doc); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *entIndexerRepository) updateFailureState(
	ctx context.Context,
	item claimedOutbox,
	status vectoroutbox.Status,
	availableAt time.Time,
	lastError string,
) (updated bool, err error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	affected, err := tx.Client().VectorOutbox.Update().
		Where(
			vectoroutbox.IDEQ(item.ID),
			vectoroutbox.StatusEQ(vectoroutbox.StatusProcessing),
			vectoroutbox.LockedUntilEQ(item.LockedUntil),
		).
		SetStatus(status).
		SetAvailableAt(availableAt).
		ClearLockedUntil().
		SetLastError(lastError).
		Save(ctx)
	if err != nil {
		return false, err
	}
	if affected == 0 {
		if err := tx.Rollback(); err != nil {
			return false, fmt.Errorf("rollback unowned vector outbox record: %w", err)
		}
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func markChunkIndexed(
	ctx context.Context,
	client *ent.Client,
	chunk *ent.DocumentChunk,
	doc *ent.Document,
	at time.Time,
) error {
	if err := chunk.Update().
		SetVectorStatus(documentchunk.VectorStatusIndexed).
		SetIndexedAt(at).
		Exec(ctx); err != nil {
		return err
	}

	hasUnindexed, err := client.DocumentChunk.Query().
		Where(
			documentchunk.HasDocumentWith(document.IDEQ(doc.ID)),
			documentchunk.VectorStatusNEQ(documentchunk.VectorStatusIndexed),
		).
		Exist(ctx)
	if err != nil {
		return err
	}
	if !hasUnindexed {
		return doc.Update().SetStatus(document.StatusReady).Exec(ctx)
	}
	return nil
}

func markChunkFailed(
	ctx context.Context,
	chunk *ent.DocumentChunk,
	doc *ent.Document,
) error {
	if err := chunk.Update().
		SetVectorStatus(documentchunk.VectorStatusFailed).
		ClearIndexedAt().
		Exec(ctx); err != nil {
		return err
	}
	return doc.Update().SetStatus(document.StatusFailed).Exec(ctx)
}

func lockChunkDocument(
	ctx context.Context,
	client *ent.Client,
	chunkID int64,
) (*ent.DocumentChunk, *ent.Document, error) {
	chunkIDInt := int(chunkID)
	if chunkID <= 0 || int64(chunkIDInt) != chunkID {
		return nil, nil, fmt.Errorf("invalid document chunk ID %d", chunkID)
	}
	chunk, err := client.DocumentChunk.Query().
		Where(documentchunk.IDEQ(chunkIDInt)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	doc, err := chunk.QueryDocument().Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	// Ingestion changes the document before its outbox record. Locking in the
	// same order prevents a document/outbox lock-order deadlock.
	if err := doc.Update().SetStatus(doc.Status).Exec(ctx); err != nil {
		return nil, nil, err
	}
	return chunk, doc, nil
}
