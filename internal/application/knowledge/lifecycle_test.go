package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/enttest"
	"eino-quickstart/internal/platform/queue"
	"eino-quickstart/internal/platform/queue/tasks"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"

	"entgo.io/ent/dialect"
	"github.com/cloudwego/eino-ext/components/embedding/openai"
	_ "github.com/mattn/go-sqlite3"
)

// These tests run the real Service, Indexer, Ent schema/transactions and Markdown
// pipeline. Queue, vector/keyword storage and model responses are controlled
// adapters: this is NOT proof that Redis/Milvus/PostgreSQL are reachable.
type lifecycleQueue struct {
	payloads    [][]byte
	unavailable bool
}

func (q *lifecycleQueue) EnqueueIndex(_ context.Context, datasetID, documentID uint64, mode tasks.IndexMode) error {
	if q.unavailable {
		return errors.New("injected queue outage")
	}
	msg, err := tasks.KnowledgeIndexMessage(tasks.KnowledgeIndexPayload{
		DatasetID: strconv.FormatUint(datasetID, 10), DocumentID: strconv.FormatUint(documentID, 10), Mode: mode,
	})
	if err == nil {
		q.payloads = append(q.payloads, msg.Payload)
	}
	return err
}

type lifecycleIndex struct {
	client           *ent.Client
	vectors          map[int64][]float32
	keywords         map[string]es.ChunkDoc
	keywordFailure   bool
	keywordFailureAt int
	keywordCalls     int
	vectorFailure    bool
	upsertCounts     map[int64]int
}

func (x *lifecycleIndex) Upsert(_ context.Context, ids []int64, vectors [][]float32) error {
	if x.vectorFailure {
		return errors.New("injected vector failure")
	}
	for n, id := range ids {
		x.upsertCounts[id]++
		x.vectors[id] = vectors[n]
	}
	return nil
}
func (x *lifecycleIndex) Delete(_ context.Context, ids []int64) error {
	for _, id := range ids {
		delete(x.vectors, id)
	}
	return nil
}
func (x *lifecycleIndex) IndexChunks(_ context.Context, docs []es.ChunkDoc) error {
	x.keywordCalls++
	if x.keywordFailure || (x.keywordFailureAt > 0 && x.keywordCalls == x.keywordFailureAt) {
		return errors.New("injected keyword write failure after vector upsert")
	}
	for _, doc := range docs {
		x.keywords[doc.ChunkID] = doc
	}
	return nil
}
func (x *lifecycleIndex) DeleteByDocument(_ context.Context, id uint64) (int64, error) {
	var count int64
	for key, doc := range x.keywords {
		if doc.DocumentID == strconv.FormatUint(id, 10) {
			delete(x.keywords, key)
			count++
		}
	}
	return count, nil
}

// Search simulates a keyword channel but hydrates/filter candidates from actual
// chunk/document rows, as production retrieval does. It deliberately does not
// simulate semantic ranking or claim to test HybridRetriever quality.
func (x *lifecycleIndex) Search(ctx context.Context, query string, scope SearchScope) (SearchOutcome, error) {
	chunks, err := x.client.DocumentChunk.Query().Where(documentchunk.VectorStatusEQ(documentchunk.VectorStatusIndexed)).WithDocument().Order(ent.Asc(documentchunk.FieldID)).All(ctx)
	if err != nil {
		return SearchOutcome{}, err
	}
	out := SearchOutcome{Channels: []string{"keyword"}}
	for _, chunk := range chunks {
		doc := chunk.Edges.Document
		indexed, ok := x.keywords[strconv.FormatUint(chunk.ID, 10)]
		if !ok || doc.DatasetID != scope.DatasetID || !doc.Enabled || doc.Status != document.StatusReady || !strings.Contains(indexed.Content, query) {
			continue
		}
		out.Hits = append(out.Hits, SearchHit{ChunkID: chunk.ID, DocumentID: doc.ID, Source: doc.Source, Title: doc.Title, Content: chunk.Content, Score: 1})
		if len(out.Hits) >= scope.ChunkBudget {
			break
		}
	}
	return out, nil
}

type lifecycleFixture struct {
	ctx       context.Context
	client    *ent.Client
	service   *Service
	indexer   *Indexer
	queue     *lifecycleQueue
	index     *lifecycleIndex
	datasetID uint64
}

func newLifecycleFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	ctx := context.Background()
	client := enttest.Open(t, dialect.SQLite, fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	t.Cleanup(func() { _ = client.Close() })
	// A local deterministic embedding endpoint exercises the real batching/client
	// adapter without paid requests or provider credentials.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data := make([]map[string]any, len(request.Input))
		for n := range data {
			data[n] = map[string]any{"object": "embedding", "index": n, "embedding": []float64{1, 0}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data, "model": "test", "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
	}))
	t.Cleanup(server.Close)
	dim := 2
	embedder, err := rag.NewEmbedder(ctx, &openai.EmbeddingConfig{BaseURL: server.URL + "/v1", APIKey: "local-test-only", Model: "test", Dimensions: &dim})
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := rag.NewPipeline(ctx, rag.Config{ChunkMaxChars: 80}, client)
	if err != nil {
		t.Fatal(err)
	}
	q := &lifecycleQueue{}
	index := &lifecycleIndex{client: client, vectors: map[int64][]float32{}, keywords: map[string]es.ChunkDoc{}, upsertCounts: map[int64]int{}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(ServiceDeps{Client: client, Queue: q, Vectors: index, Keywords: index, Searcher: index, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	indexer, err := NewIndexer(IndexerConfig{Client: client, Pipeline: pipeline, Embedder: embedder, Vectors: index, Keyword: index, BatchSize: 1, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := client.Dataset.Create().SetName(t.Name()).SetType("document").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return &lifecycleFixture{ctx: ctx, client: client, service: service, indexer: indexer, queue: q, index: index, datasetID: ds.ID}
}
func (f *lifecycleFixture) create(t *testing.T, content string) uint64 {
	t.Helper()
	results, err := f.service.Create(f.ctx, CreateInput{DatasetID: f.datasetID, Title: "生命周期验收", Content: content, OwnerSubject: "acceptance"})
	if err != nil || len(results) != 1 {
		t.Fatalf("create: results=%v err=%v", results, err)
	}
	return results[0].Document.ID
}
func (f *lifecycleFixture) drain(t *testing.T) {
	t.Helper()
	for len(f.queue.payloads) > 0 {
		payload := f.queue.payloads[0]
		f.queue.payloads = f.queue.payloads[1:]
		if err := f.indexer.HandleTask(f.ctx, payload); err != nil {
			t.Fatal(err)
		}
	}
}
func (f *lifecycleFixture) assertState(t *testing.T, id uint64, status document.Status) []uint64 {
	t.Helper()
	doc, err := f.service.Get(f.ctx, f.datasetID, id)
	if err != nil || doc.Status != status {
		t.Fatalf("state: doc=%v err=%v want=%s", doc, err, status)
	}
	chunks, err := f.client.DocumentChunk.Query().Where(documentchunk.HasDocumentWith(document.IDEQ(id))).Order(ent.Asc(documentchunk.FieldID)).All(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := f.service.ChunkStats(f.ctx, []uint64{id})
	if err != nil || stats[id].Total != len(chunks) {
		t.Fatalf("counts: %v %v", stats, err)
	}
	if status == document.StatusReady && (len(chunks) == 0 || stats[id].Indexed != len(chunks)) {
		t.Fatalf("ready without all chunks indexed: %v", stats)
	}
	ids := make([]uint64, len(chunks))
	indexes := map[int]bool{}
	for n, c := range chunks {
		ids[n] = c.ID
		if indexes[c.ChunkIndex] {
			t.Fatalf("duplicated chunk index %d", c.ChunkIndex)
		}
		indexes[c.ChunkIndex] = true
	}
	return ids
}
func (f *lifecycleFixture) assertSearch(t *testing.T, id uint64, query string, found bool) {
	t.Helper()
	out, err := f.service.Search(f.ctx, SearchInput{DatasetID: f.datasetID, Query: query, TopK: 5})
	if err != nil {
		t.Fatal(err)
	}
	actual := false
	for _, hit := range out.Hits {
		if hit.DocumentID == id {
			actual = true
		}
	}
	if actual != found {
		t.Fatalf("search %q found=%v want=%v hits=%v", query, actual, found, out.Hits)
	}
}
func equalIDs(a, b []uint64) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func TestDocumentLifecycle(t *testing.T) {
	f := newLifecycleFixture(t)
	bodyA := "# 验收\n\n验收蓝鲸词A，用于证明第一次索引。\n\n" + strings.Repeat("独立分块的补充文字。", 30)
	id := f.create(t, bodyA)
	ids := f.assertState(t, id, document.StatusIndexing)
	if len(ids) != 0 || len(f.queue.payloads) != 1 {
		t.Fatal("paused worker must leave a queued, unchunked document")
	}
	f.assertSearch(t, id, "验收蓝鲸词A", false)
	f.drain(t)
	before := f.assertState(t, id, document.StatusReady)
	f.assertSearch(t, id, "验收蓝鲸词A", true)
	contents, err := f.service.contents.Contents(f.ctx, []uint64{id})
	if err != nil || contents[id] != bodyA {
		t.Fatalf("body changed: %v %v", contents, err)
	}
	// A delivered duplicate catch-up task must preserve both IDs and store counts.
	if err := f.indexer.IndexDocument(f.ctx, id, tasks.IndexModeCatchUp); err != nil {
		t.Fatal(err)
	}
	if after := f.assertState(t, id, document.StatusReady); !equalIDs(before, after) {
		t.Fatalf("duplicate task rechunked: %v -> %v", before, after)
	}
	bodyB := "# 更新\n\n验收蓝鲸词B，仅保留更新后的内容。"
	if _, err := f.service.Update(f.ctx, UpdateInput{DatasetID: f.datasetID, DocumentID: id, Content: bodyB}); err != nil {
		t.Fatal(err)
	}
	f.assertState(t, id, document.StatusIndexing)
	f.drain(t)
	updated := f.assertState(t, id, document.StatusReady)
	contents, err = f.service.contents.Contents(f.ctx, []uint64{id})
	if err != nil || contents[id] != bodyB {
		t.Fatal("updated body is not persisted")
	}
	f.assertSearch(t, id, "验收蓝鲸词B", true)
	// Unlike semantic queries, checking actual stored chunks proves A is gone.
	chunks, err := f.client.DocumentChunk.Query().Where(documentchunk.HasDocumentWith(document.IDEQ(id))).All(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range chunks {
		if strings.Contains(chunk.Content, "验收蓝鲸词A") {
			t.Fatal("old chunk content remains")
		}
	}
	for _, chunk := range f.index.keywords {
		if strings.Contains(chunk.Content, "验收蓝鲸词A") {
			t.Fatal("old keyword entry remains")
		}
	}
	if _, err := f.service.Reindex(f.ctx, f.datasetID, id); err != nil {
		t.Fatal(err)
	}
	f.assertState(t, id, document.StatusIndexing)
	f.drain(t)
	rebuilt := f.assertState(t, id, document.StatusReady)
	if equalIDs(updated, rebuilt) {
		t.Fatal("explicit rebuild did not replace chunks")
	}
	second := f.create(t, "第二篇验收文档，同样包含验收蓝鲸词B。")
	f.drain(t)
	result, err := f.service.ReindexDataset(f.ctx, f.datasetID)
	if err != nil || result.Documents != 2 || result.Failed != 0 {
		t.Fatalf("dataset rebuild: %v %v", result, err)
	}
	f.drain(t)
	f.assertState(t, id, document.StatusReady)
	f.assertState(t, second, document.StatusReady)
	// Deleting while a queued rebuild exists must not resurrect a document.
	if _, err := f.service.Reindex(f.ctx, f.datasetID, id); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Delete(f.ctx, f.datasetID, id); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	if _, err := f.service.Get(f.ctx, f.datasetID, id); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("deleted document visible: %v", err)
	}
	f.assertSearch(t, id, "验收蓝鲸词B", false)
	assertLifecycleStores(t, f)
}

func assertLifecycleStores(t *testing.T, f *lifecycleFixture) {
	t.Helper()
	ids, err := f.client.DocumentChunk.Query().IDs(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(f.index.vectors) != len(ids) || len(f.index.keywords) != len(ids) {
		t.Fatalf("stale/duplicate external entries: chunks=%v vectors=%d keywords=%d", ids, len(f.index.vectors), len(f.index.keywords))
	}
	for _, id := range ids {
		if _, ok := f.index.vectors[int64(id)]; !ok {
			t.Fatalf("missing vector %d", id)
		}
		if _, ok := f.index.keywords[strconv.FormatUint(id, 10)]; !ok {
			t.Fatalf("missing keyword %d", id)
		}
	}
}

func TestDocumentLifecycleQueueFailureRecovery(t *testing.T) {
	f := newLifecycleFixture(t)
	f.queue.unavailable = true
	_, err := f.service.Create(f.ctx, CreateInput{DatasetID: f.datasetID, Title: "队列故障", Content: "验收蓝鲸词A"})
	if !errors.Is(err, ErrQueueUnavailable) {
		t.Fatalf("queue failure hidden: %v", err)
	}
	doc, err := f.client.Document.Query().Only(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	f.assertState(t, doc.ID, document.StatusFailed)
	if len(f.queue.payloads) != 0 {
		t.Fatal("failed enqueue left a task")
	}
	result, err := f.service.ReindexDataset(f.ctx, f.datasetID)
	if err != nil || result.Documents != 0 || result.Failed != 1 {
		t.Fatalf("dataset queue failure hidden: %v %v", result, err)
	}
	f.queue.unavailable = false
	if _, err := f.service.Reindex(f.ctx, f.datasetID, doc.ID); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	f.assertState(t, doc.ID, document.StatusReady)
	f.assertSearch(t, doc.ID, "验收蓝鲸词A", true)
	assertLifecycleStores(t, f)
}

func TestDocumentLifecycleRetryAndExhaustion(t *testing.T) {
	f := newLifecycleFixture(t)
	id := f.create(t, "# 重试\n\n验收蓝鲸词A\n\n"+strings.Repeat("重试分块内容。", 30))
	payload := f.queue.payloads[0]
	f.queue.payloads = nil
	f.index.keywordFailure = true
	attempt := queue.WithRetryState(f.ctx, queue.RetryState{Known: true, Attempt: 0, Max: 1})
	if err := f.indexer.HandleTask(attempt, payload); err == nil {
		t.Fatal("keyword outage was hidden")
	}
	before := f.assertState(t, id, document.StatusIndexing)
	if len(f.index.vectors) != 1 {
		t.Fatal("failure must occur after vector upsert")
	}
	f.index.keywordFailure = false
	if err := f.indexer.HandleTask(attempt, payload); err != nil {
		t.Fatal(err)
	}
	if after := f.assertState(t, id, document.StatusReady); !equalIDs(before, after) {
		t.Fatal("retry duplicated/replaced chunks")
	}
	assertLifecycleStores(t, f)
	if _, err := f.service.Reindex(f.ctx, f.datasetID, id); err != nil {
		t.Fatal(err)
	}
	payload = f.queue.payloads[0]
	f.queue.payloads = nil
	f.index.keywordFailure = true
	last := queue.WithRetryState(f.ctx, queue.RetryState{Known: true, Attempt: 1, Max: 1})
	if err := f.indexer.HandleTask(last, payload); err == nil {
		t.Fatal("last failure was hidden")
	}
	f.assertState(t, id, document.StatusFailed)
	f.index.keywordFailure = false
	if _, err := f.service.Reindex(f.ctx, f.datasetID, id); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	f.assertState(t, id, document.StatusReady)
	assertLifecycleStores(t, f)
}

func TestDocumentLifecycleInvalidInput(t *testing.T) {
	f := newLifecycleFixture(t)
	_, err := f.service.Create(f.ctx, CreateInput{DatasetID: f.datasetID, Title: "空正文", Content: " "})
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("empty content: %v", err)
	}
	if len(f.queue.payloads) != 0 {
		t.Fatal("invalid request published work")
	}
	_, err = f.service.Search(f.ctx, SearchInput{DatasetID: f.datasetID, Query: " "})
	if !errors.As(err, &invalid) {
		t.Fatalf("empty query: %v", err)
	}
	_, err = f.service.Get(f.ctx, f.datasetID, 999999)
	if !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("missing document: %v", err)
	}
}

// Errors outside embedBatch (e.g. rechunk transaction failures) also need a
// terminal state when the queue exhausts retries, not permanent indexing.
func TestDocumentLifecycleRechunkFailureExhaustion(t *testing.T) {
	f := newLifecycleFixture(t)
	id := f.create(t, "验收蓝鲸词A，事务失败后应该可以修复。")
	failWrites := true
	f.client.DocumentChunk.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if failWrites && m.Op().Is(ent.OpCreate) {
				return nil, errors.New("injected chunk transaction failure")
			}
			return next.Mutate(ctx, m)
		})
	})
	last := queue.WithRetryState(f.ctx, queue.RetryState{Known: true, Attempt: 0, Max: 0})
	if err := f.indexer.HandleTask(last, f.queue.payloads[0]); err == nil {
		t.Fatal("transaction failure hidden")
	}
	f.queue.payloads = nil
	f.assertState(t, id, document.StatusFailed)
	failWrites = false
	if _, err := f.service.Reindex(f.ctx, f.datasetID, id); err != nil {
		t.Fatal(err)
	}
	f.drain(t)
	f.assertState(t, id, document.StatusReady)
	assertLifecycleStores(t, f)
}

func TestDocumentLifecycleCancellationIsNotTerminal(t *testing.T) {
	f := newLifecycleFixture(t)
	id := f.create(t, "验收蓝鲸词A")
	ctx, cancel := context.WithCancel(queue.WithRetryState(f.ctx, queue.RetryState{Known: true, Attempt: 0, Max: 0}))
	cancel()
	if err := f.indexer.HandleTask(ctx, f.queue.payloads[0]); err == nil {
		t.Fatal("cancellation hidden")
	}
	f.assertState(t, id, document.StatusIndexing)
	f.drain(t)
	f.assertState(t, id, document.StatusReady)
}

func TestDocumentLifecyclePartialBatchRetry(t *testing.T) {
	f := newLifecycleFixture(t)
	id := f.create(t, "# 部分批次成功\n\n验收蓝鲸词A\n\n"+strings.Repeat("足够长的正文用于生成多个可独立处理的分块。", 40))
	f.index.keywordFailureAt = 2
	payload := f.queue.payloads[0]
	f.queue.payloads = nil
	if err := f.indexer.HandleTask(f.ctx, payload); err == nil {
		t.Fatal("second batch outage hidden")
	}
	before := f.assertState(t, id, document.StatusIndexing)
	if len(before) < 3 {
		t.Fatalf("fixture must have multiple batches: %v", before)
	}
	stats, err := f.service.ChunkStats(f.ctx, []uint64{id})
	if err != nil {
		t.Fatal(err)
	}
	if stats[id].Indexed != 1 {
		t.Fatalf("expected one committed batch before failure: %+v", stats[id])
	}
	if err := f.indexer.HandleTask(f.ctx, payload); err != nil {
		t.Fatal(err)
	}
	if after := f.assertState(t, id, document.StatusReady); !equalIDs(before, after) {
		t.Fatal("partial retry replaced chunks")
	}
	if f.index.upsertCounts[int64(before[0])] != 1 {
		t.Fatal("retry re-embedded already indexed first batch")
	}
	if f.index.upsertCounts[int64(before[1])] != 2 {
		t.Fatal("retry did not replay partially written batch")
	}
	assertLifecycleStores(t, f)
}

func TestDocumentLifecycleVectorFailureRecovery(t *testing.T) {
	f := newLifecycleFixture(t)
	id := f.create(t, "验收蓝鲸词A，模拟向量库故障。")
	payload := f.queue.payloads[0]
	f.queue.payloads = nil
	f.index.vectorFailure = true
	if err := f.indexer.HandleTask(f.ctx, payload); err == nil {
		t.Fatal("vector outage hidden")
	}
	before := f.assertState(t, id, document.StatusIndexing)
	f.index.vectorFailure = false
	if err := f.indexer.HandleTask(f.ctx, payload); err != nil {
		t.Fatal(err)
	}
	if after := f.assertState(t, id, document.StatusReady); !equalIDs(before, after) {
		t.Fatal("vector failure retry duplicated chunks")
	}
	assertLifecycleStores(t, f)
}
