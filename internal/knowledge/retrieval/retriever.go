package retrieval

import (
	"context"
	"strings"
)

type Result struct {
	ChunkID     uint64
	CitationID  string
	Source      string
	Title       string
	HeadingPath string
	StartLine   int
	EndLine     int
	Content     string
	Score       float64
}

type Candidate struct {
	ChunkID uint64
	Score   float64
}
type SearchScope struct {
	ActorSubject     string
	KnowledgeBaseIDs []uint64
}

func (s SearchScope) Normalized() SearchScope {
	result := SearchScope{
		ActorSubject: strings.TrimSpace(s.ActorSubject),
	}

	seen := make(map[uint64]struct{}, len(s.KnowledgeBaseIDs))

	for _, id := range s.KnowledgeBaseIDs {
		if id <= 0 {
			continue
		}

		if _, exists := seen[id]; exists {
			continue
		}

		seen[id] = struct{}{}
		result.KnowledgeBaseIDs =
			append(result.KnowledgeBaseIDs, id)
	}

	return result
}

func (s SearchScope) HasKnowledgeBases() bool {
	return len(s.Normalized().KnowledgeBaseIDs) > 0
}

type SearchRequest struct {
	ActorSubject     string
	Query            string
	KnowledgeBaseIDs []uint64
	TopK             int
}

func (r SearchRequest) Scope() SearchScope {
	return SearchScope{
		ActorSubject:     r.ActorSubject,
		KnowledgeBaseIDs: r.KnowledgeBaseIDs,
	}.Normalized()
}

type VectorSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]Candidate, error)
}

type KeywordSearcher interface {
	Search(ctx context.Context, scope SearchScope, query string, limit int) ([]Candidate, error)
}

type Retriever interface {
	Search(ctx context.Context, request SearchRequest) ([]Result, error)
}

type DebugResult struct {
	Query string

	VectorResults  []Candidate
	KeywordResults []Candidate
	FusedResults   []Candidate
	ExactResults   []Candidate
	FinalResults   []Result
}

type DebugRetriever interface {
	DebugSearch(ctx context.Context, request SearchRequest) (*DebugResult, error)
}
