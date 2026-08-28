package retriever

import (
	context "context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/embedding"
	"eino-quickstart/internal/storage/vectorstore"
	"fmt"
	"strings"
)

type HybridRetriever struct {
	Client             *ent.Client
	Embedder           embedding.Embedder
	VectorStore        vectorstore.Store
	KeywordSearcher    KeywordSearcher
	DefaultTopK        int
	MaxTopK            int
	VectorCandidates   int
	KeywordCandidates  int
	MaxQueryCharacters int
	MaxResultBytes     int
	VectorWeight       float64
	KeywordWeight      float64
	RRFSmoothing       int
}

func (r *HybridRetriever) Search(ctx context.Context, actorSubject string, query string, topK int) ([]Result, error) {
	query = strings.TrimSpace(query)

	if query == "" {
		return nil, fmt.Errorf("knowledge query is required")
	}
	if len([]rune(query)) > r.MaxQueryCharacters {
		return nil, fmt.Errorf("knowledge query exceeds maximum length")
	}

	if topK == 0 {
		topK = r.DefaultTopK
	}
	if topK < 0 || topK > r.MaxTopK {
		return nil, fmt.Errorf("knowledge topK exceeds maximum")
	}

	vectors, err := r.Embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed knowledge query: %w", err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("query embedding response has invalid count")
	}

	vectorResults, err := r.VectorStore.Search(
		ctx,
		vectors[0],
		r.VectorCandidates,
	)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	vectorCandidates := make([]Candidate, 0, len(vectorResults))
	for _, item := range vectorResults {
		vectorCandidates = append(vectorCandidates, Candidate{
			ChunkID: item.ChunkID,
			Score:   item.Score,
		})
	}

	keywordCandidates, err := r.KeywordSearcher.Search(
		ctx,
		actorSubject,
		query,
		r.KeywordCandidates,
	)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}

	fused := FuseRRF(
		r.RRFSmoothing,
		WeightedCandidates{
			Items:  vectorCandidates,
			Weight: r.VectorWeight,
		},
		WeightedCandidates{
			Items:  keywordCandidates,
			Weight: r.KeywordWeight,
		},
	)

	return r.loadAuthorizedResults(
		ctx,
		actorSubject,
		fused,
		topK,
	)
}
