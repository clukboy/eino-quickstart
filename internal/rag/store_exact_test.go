package rag

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/enttest"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag/store/postgres"

	"entgo.io/ent/dialect"
	"github.com/cloudwego/eino/components/retriever"
	_ "github.com/mattn/go-sqlite3"
)

func TestNoESStructuredExactSearchAndFusion(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, dialect.SQLite, "file:structured-exact?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	datasetA, err := client.Dataset.Create().SetName("exact-a").SetType("document").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	datasetB, err := client.Dataset.Create().SetName("exact-b").SetType("document").Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	chunkID := func(ds uint64, model string, enabled bool) uint64 {
		t.Helper()
		doc, err := client.Document.Create().
			SetSource(model + ".md").
			SetTitle(model).
			SetContent("product content").
			SetDatasetID(ds).
			SetOwnerSubject("tester").
			SetVisibility(document.VisibilitySystem).
			SetStatus(document.StatusReady).
			SetEnabled(enabled).
			Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		chunk, err := client.DocumentChunk.Create().
			SetChunkIndex(0).
			SetContent("product content").
			SetHeadingPath(model).
			SetMetadata(map[string]interface{}{"model": model, "product_id": model, "variants": []string{model + "-ALT"}}).
			SetVectorStatus(documentchunk.VectorStatusIndexed).
			SetDocumentID(doc.ID).
			Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return chunk.ID
	}

	matchingID := chunkID(datasetA.ID, " ACC-H105P ", true)
	chunkID(datasetA.ID, "ACC-H105PE", true)
	chunkID(datasetA.ID, "ACC-H105P-DISABLED", false)
	chunkID(datasetB.ID, "ACC-H105P", true)

	mapping := &es.Mapping{Fields: []es.FieldSpec{
		{Name: "model", From: "model", Type: es.FieldText, Boost: 5, Keyword: true},
		{Name: "product_id", From: "product_id", Type: es.FieldText, Boost: 5, Keyword: true},
		{Name: "variants", From: "variants", Type: es.FieldText, Boost: 2, Keyword: true, Multi: true},
	}}
	store := &Store{PgStore: postgres.NewPostgres(client), Mapping: mapping}

	hits, err := store.SearchByExact(ctx, "  acc-h105p  ", 10, Filter{DatasetID: datasetA.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Doc == nil || hits[0].Doc.ID == "" {
		t.Fatalf("full normalized model should return one enabled exact hit: %+v", hits)
	}
	if hits[0].Doc.ID == "" || hits[0].Score <= 0 {
		t.Fatalf("exact hit should carry score and document: %+v", hits[0])
	}
	if hits[0].Doc.ID != fmt.Sprint(matchingID) {
		t.Fatalf("unexpected exact chunk id: got=%s want=%d", hits[0].Doc.ID, matchingID)
	}

	for _, query := range []string{"ACC-H105", "ACC-H999"} {
		hits, err := store.SearchByExact(ctx, query, 10, Filter{DatasetID: datasetA.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 0 {
			t.Fatalf("query %q must not be mislabeled exact: %+v", query, hits)
		}
	}

	all, err := store.SearchByExact(ctx, "ACC-H105P", 10, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("dataset/disabled filtering should leave two exact hits, got %d", len(all))
	}

	hybrid, err := NewHybridRetriever(HybridConfig{
		Store: store,
		TopK:  3,
		Retrieval: RetrievalPolicy{
			ExactWeight: 1, KeywordWeight: 0, VectorWeight: 0,
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	docs, report, err := hybrid.RetrieveDetailed(ctx, "ACC-H105P", retriever.WithTopK(1), WithFilter(Filter{DatasetID: datasetA.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || len(report.Used()) != 1 || report.Used()[0] != "exact" {
		t.Fatalf("no-ES exact channel should feed fusion/report: docs=%d used=%v report=%+v", len(docs), report.Used(), report)
	}
}
