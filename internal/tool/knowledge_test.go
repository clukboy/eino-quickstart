package tool

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	retrieval "eino-quickstart/internal/knowledge/retrieval"
	"eino-quickstart/internal/platform/auth"
)

type fakeKnowledgeRetriever struct {
	actor   string
	query   string
	topK    int
	results []retrieval.Result
	err     error
}

func (r *fakeKnowledgeRetriever) Search(
	_ context.Context,
	actorSubject string,
	query string,
	topK int,
) ([]retrieval.Result, error) {
	r.actor = actorSubject
	r.query = query
	r.topK = topK
	return r.results, r.err
}

func TestKnowledgeSearchToolReturnsCitedResults(t *testing.T) {
	retriever := &fakeKnowledgeRetriever{results: []retrieval.Result{{
		ChunkID:     7,
		CitationID:  "docs/guide.md#chunk-2",
		Source:      "docs/guide.md",
		Title:       "Guide",
		HeadingPath: "Install > macOS",
		StartLine:   10,
		EndLine:     14,
		Content:     "Install the package with the documented command.",
	}}}
	search, err := NewKnowledgeSearch(retriever, " actor-1 ")
	if err != nil {
		t.Fatalf("NewKnowledgeSearch() error = %v", err)
	}

	info, err := search.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.Name != "search_knowledge" {
		t.Errorf("tool name = %q, want search_knowledge", info.Name)
	}

	output, err := search.InvokableRun(
		context.Background(),
		`{"query":"  install package  ","topK":2}`,
	)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	for _, expected := range []string{
		"[docs/guide.md#chunk-2]",
		"Source: docs/guide.md",
		"Title: Guide",
		"Section: Install > macOS",
		"Lines: 10-14",
		"Excerpt: Install the package",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output missing %q:\n%s", expected, output)
		}
	}
	if retriever.actor != "actor-1" || retriever.query != "install package" ||
		retriever.topK != 2 {
		t.Errorf("retriever arguments = %#v", retriever)
	}
}

func TestKnowledgeSearchToolValidatesInputAndErrors(t *testing.T) {
	retriever := &fakeKnowledgeRetriever{}
	search, err := NewKnowledgeSearch(retriever, "actor")
	if err != nil {
		t.Fatalf("NewKnowledgeSearch() error = %v", err)
	}

	for _, arguments := range []string{
		`{"query":""}`,
		`{"query":"question","topK":-1}`,
	} {
		if _, err := search.InvokableRun(context.Background(), arguments); err == nil {
			t.Errorf("InvokableRun(%s) error = nil, want validation error", arguments)
		}
	}

	retriever.err = errors.New("backend unavailable")
	if _, err := search.InvokableRun(
		context.Background(),
		`{"query":"question"}`,
	); err == nil || !strings.Contains(err.Error(), "search knowledge") {
		t.Errorf("backend error = %v", err)
	}

	retriever.err = nil
	output, err := search.InvokableRun(context.Background(), `{"query":"question"}`)
	if err != nil || output != "No authorized knowledge-base results found." {
		t.Errorf("empty result = (%q, %v)", output, err)
	}
}

func TestKnowledgeSearchUsesAuthenticatedActor(t *testing.T) {
	retriever := &fakeKnowledgeRetriever{}
	search := &KnowledgeSearch{
		Retriever:    retriever,
		ActorSubject: "fallback-actor",
	}
	authenticator, err := auth.New([]auth.APIKey{{
		Secret: "test-key",
		Identity: auth.Identity{
			Subject: "context-actor",
			Role:    auth.RoleAgent,
		},
	}})
	if err != nil {
		t.Fatalf("auth.New() error = %v", err)
	}

	handler := authenticator.Authenticate(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if _, err := search.run(
				request.Context(),
				knowledgeSearchInput{Query: "question"},
			); err != nil {
				t.Errorf("run() error = %v", err)
			}
		},
	))
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set("Authorization", "Bearer test-key")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if retriever.actor != "context-actor" {
		t.Errorf("actor = %q, want authenticated subject", retriever.actor)
	}
}
