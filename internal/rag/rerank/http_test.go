package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestHTTPRerankerSortsOriginalDocumentsAndSendsAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		var got request
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got.Model != "model-a" || got.Query != "hinge" || len(got.Documents) != 3 {
			t.Fatalf("request = %#v", got)
		}
		_ = json.NewEncoder(w).Encode(response{Results: []result{
			{Index: 2, RelevanceScore: 0.9},
			{Index: 0, RelevanceScore: 0.5},
			{Index: 1, RelevanceScore: 0.1},
		}})
	}))
	defer server.Close()

	ranker, err := NewHTTP(Config{Endpoint: server.URL, APIKey: "secret", Model: "model-a", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	docs := []*schema.Document{{ID: "a", Content: "A"}, {ID: "b", Content: "B"}, {ID: "c", Content: "C"}}
	got, err := ranker.Rerank(context.Background(), "hinge", docs)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != docs[2] || got[1] != docs[0] || got[2] != docs[1] {
		t.Fatalf("order = [%s %s %s]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestHTTPRerankerRejectsInvalidResultSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(response{Results: []result{{Index: 0}, {Index: 0}}})
	}))
	defer server.Close()
	ranker, err := NewHTTP(Config{Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ranker.Rerank(context.Background(), "q", []*schema.Document{{ID: "a"}, {ID: "b"}})
	if err == nil {
		t.Fatal("expected duplicate index error")
	}
}
