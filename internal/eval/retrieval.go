package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"eino-quickstart/internal/knowledge/retrieval"
)

type RetrievalEvaluator struct {
	Retriever retrieval.Retriever
	TopK      int
}

func (e RetrievalEvaluator) Evaluate(ctx context.Context, test RetrievalCase) CaseResult {
	startedAt := time.Now()

	results, err := e.Retriever.Search(ctx, test.ActorSubject, test.Query, e.TopK)

	result := CaseResult{
		ID:       test.ID,
		Duration: time.Since(startedAt).Milliseconds(),
		Details:  make(map[string]any),
	}

	if err != nil {
		result.Error = err.Error()
		return result
	}

	sources := make(map[string]struct{}, len(results))
	citations := make([]string, 0, len(results))

	for _, item := range results {
		sources[item.Source] = struct{}{}
		citations = append(citations, item.CitationID)
	}

	result.Details["sources"] = sourceNames(sources)
	result.Details["citations"] = citations
	result.Details["result_count"] = len(results)

	if len(results) < test.MinResults {
		result.Error = fmt.Sprintf(
			"got %d results, expected at least %d",
			len(results),
			test.MinResults,
		)
		return result
	}

	for _, source := range test.ExpectedSources {
		if _, found := sources[source]; !found {
			result.Error = "expected source missing: " + source
			return result
		}
	}

	for _, source := range test.ForbiddenSources {
		if _, found := sources[source]; found {
			result.Error = "forbidden source returned: " + source
			return result
		}
	}

	for _, citation := range citations {
		if !strings.Contains(citation, "#chunk-") {
			result.Error = "invalid citation format: " + citation
			return result
		}
	}

	result.Passed = true
	return result
}

func sourceNames(items map[string]struct{}) []string {
	sources := make([]string, 0, len(items))
	for source := range items {
		sources = append(sources, source)
	}
	return sources
}
