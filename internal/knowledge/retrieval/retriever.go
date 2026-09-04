package retrieval

import "context"

type Result struct {
	ChunkID     int64
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
	ChunkID int64
	Score   float64
}

type SearchRequest struct {
	ActorSubject string

	Query string

	KnowledgeBaseIDs []int

	TopK int
}

type VectorSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]Candidate, error)
}

type KeywordSearcher interface {
	Search(ctx context.Context, actorSubject string, query string, limit int) ([]Candidate, error)
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
