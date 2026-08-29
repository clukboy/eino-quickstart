package retrieval

import (
	"context"
	"testing"
)

func TestTruncateUTF8(t *testing.T) {
	tests := map[string]struct {
		value string
		limit int
		want  string
	}{
		"within limit":    {"hello", 5, "hello"},
		"ASCII truncated": {"hello", 3, "hel"},
		"UTF8 truncated":  {"éclair", 1, ""},
		"UTF8 boundary":   {"éclair", 2, "é"},
		"zero limit":      {"hello", 0, ""},
		"negative limit":  {"hello", -1, ""},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := truncateUTF8(test.value, test.limit); got != test.want {
				t.Errorf(
					"truncateUTF8(%q, %d) = %q, want %q",
					test.value,
					test.limit,
					got,
					test.want,
				)
			}
		})
	}
}

func TestHybridRetrieverRequiresDependencies(t *testing.T) {
	var retriever *HybridRetriever
	if _, err := retriever.Search(nil, "actor", "query", 1); err == nil {
		t.Error("Search() error = nil, want missing dependency error")
	}
	if _, err := retriever.loadAuthorizedResults(
		context.Background(),
		"actor",
		[]Candidate{{ChunkID: 1}},
		1,
	); err == nil {
		t.Error("loadAuthorizedResults() error = nil, want missing client error")
	}
}
