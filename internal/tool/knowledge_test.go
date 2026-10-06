package tool

import (
	"context"
	"strings"
	"testing"

	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
)

type fakeKnowledgeSearcher struct {
	outcomes map[uint64]knowledge.SearchOutcome
	calls    []knowledge.SearchInput
}

func (s *fakeKnowledgeSearcher) Search(_ context.Context, input knowledge.SearchInput) (knowledge.SearchOutcome, error) {
	s.calls = append(s.calls, input)
	return s.outcomes[input.DatasetID], nil
}

type fakeDatasetBindings struct {
	subject string
	ids     []uint64
}

func (b fakeDatasetBindings) DatasetIDs(_ context.Context, subject string) ([]uint64, error) {
	b.subject = subject
	return b.ids, nil
}

func TestKnowledgeSearchUsesIdentityBoundDatasetsAndFormatsCitations(t *testing.T) {
	searcher := &fakeKnowledgeSearcher{outcomes: map[uint64]knowledge.SearchOutcome{
		11: {Hits: []knowledge.SearchHit{{
			ChunkID: 3, DocumentID: 101, Source: "catalog.md", Title: "Catalog", Content: "slower result", Score: 0.4,
		}}},
		22: {Hits: []knowledge.SearchHit{{
			ChunkID: 7, DocumentID: 202, Source: "guide.md", HeadingPath: "Setup", Content: "faster result", Score: 0.9,
		}}},
	}}
	toolValue, err := NewKnowledgeSearchWithBindings("ignored", fakeDatasetBindings{ids: []uint64{11, 22}}, searcher)
	if err != nil {
		t.Fatalf("construct tool: %v", err)
	}

	ctx := auth.WithIdentity(context.Background(), auth.Identity{Subject: "agent-7", Role: auth.RoleAgent})
	output, err := toolValue.InvokableRun(ctx, `{"query":"how to configure","topK":1}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if !strings.Contains(output, "[guide.md#chunk-7]") {
		t.Fatalf("output is missing the top citation: %s", output)
	}
	if strings.Contains(output, "catalog.md#chunk-3") {
		t.Fatalf("topK was not applied across authorized datasets: %s", output)
	}
	if len(searcher.calls) != 2 || searcher.calls[0].DatasetID != 11 || searcher.calls[1].DatasetID != 22 {
		t.Fatalf("unexpected search scopes: %+v", searcher.calls)
	}
	for _, call := range searcher.calls {
		if call.TopK != 1 {
			t.Fatalf("topK = %d, want 1", call.TopK)
		}
	}
}

func TestKnowledgeSearchUsesServiceTopKWhenInputOmitsIt(t *testing.T) {
	searcher := &fakeKnowledgeSearcher{outcomes: map[uint64]knowledge.SearchOutcome{
		11: {
			TopK: 2,
			Hits: []knowledge.SearchHit{
				{ChunkID: 1, DocumentID: 101, Source: "one.md", Content: "one", Score: 0.9},
				{ChunkID: 2, DocumentID: 101, Source: "one.md", Content: "two", Score: 0.8},
			},
		},
		22: {
			TopK: 2,
			Hits: []knowledge.SearchHit{
				{ChunkID: 3, DocumentID: 202, Source: "two.md", Content: "three", Score: 0.7},
			},
		},
	}}
	toolValue, err := NewKnowledgeSearchWithBindings("agent-7", fakeDatasetBindings{ids: []uint64{11, 22}}, searcher)
	if err != nil {
		t.Fatalf("construct tool: %v", err)
	}

	output, err := toolValue.InvokableRun(context.Background(), `{"query":"question"}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if len(searcher.calls) != 2 {
		t.Fatalf("search calls = %d, want 2", len(searcher.calls))
	}
	for _, call := range searcher.calls {
		if call.TopK != 0 {
			t.Fatalf("service topK input = %d, want omitted zero", call.TopK)
		}
	}
	if strings.Count(output, "\n[") != 2 {
		t.Fatalf("output contains more than the service-configured topK: %s", output)
	}
}

func TestKnowledgeSearchRequiresIdentityForDynamicBindings(t *testing.T) {
	searcher := &fakeKnowledgeSearcher{outcomes: map[uint64]knowledge.SearchOutcome{}}
	toolValue, err := NewKnowledgeSearchWithBindings("", fakeDatasetBindings{ids: []uint64{1}}, searcher)
	if err != nil {
		t.Fatalf("construct tool: %v", err)
	}

	if _, err := toolValue.InvokableRun(context.Background(), `{"query":"question"}`); err == nil || !strings.Contains(err.Error(), "actor subject") {
		t.Fatalf("run error = %v, want authenticated subject error", err)
	}
}

func TestKnowledgeSearchReturnsEmptyMessageWithoutAuthorizedDatasets(t *testing.T) {
	searcher := &fakeKnowledgeSearcher{outcomes: map[uint64]knowledge.SearchOutcome{}}
	toolValue, err := NewKnowledgeSearchWithBindings("", fakeDatasetBindings{}, searcher)
	if err != nil {
		t.Fatalf("construct tool: %v", err)
	}

	ctx := auth.WithIdentity(context.Background(), auth.Identity{Subject: "agent-7", Role: auth.RoleAgent})
	output, err := toolValue.InvokableRun(ctx, `{"query":"question"}`)
	if err != nil {
		t.Fatalf("run tool: %v", err)
	}
	if output != "No authorized knowledge-base results found." {
		t.Fatalf("output = %q", output)
	}
	if len(searcher.calls) != 0 {
		t.Fatalf("searcher was called without authorized datasets: %+v", searcher.calls)
	}
}
