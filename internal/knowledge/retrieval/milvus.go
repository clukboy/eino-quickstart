package retriever

import (
	context "context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/vectorstore"
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
	if r == nil {
		return nil, fmt.Errorf("knowledge retriever is nil")
	}
	if r.Client == nil {
		return nil, fmt.Errorf("knowledge ent client is required")
	}
	if r.Embedder == nil {
		return nil, fmt.Errorf("knowledge embedder is required")
	}
	if r.VectorStore == nil {
		return nil, fmt.Errorf("knowledge vector store is required")
	}
	if r.KeywordSearcher == nil {
		return nil, fmt.Errorf("knowledge keyword searcher is required")
	}

	query = strings.TrimSpace(query)

	if query == "" {
		return nil, fmt.Errorf("knowledge query is required")
	}
	if r.MaxQueryCharacters > 0 &&
		len([]rune(query)) > r.MaxQueryCharacters {
		return nil, fmt.Errorf("knowledge query exceeds maximum length")
	}

	if topK == 0 {
		topK = r.DefaultTopK
	}
	if topK <= 0 {
		return nil, fmt.Errorf("knowledge topK must be greater than zero")
	}
	if r.MaxTopK > 0 && topK > r.MaxTopK {
		return nil, fmt.Errorf("knowledge topK exceeds maximum")
	}
	if r.VectorCandidates <= 0 {
		return nil, fmt.Errorf("vector candidate limit must be greater than zero")
	}
	if r.KeywordCandidates <= 0 {
		return nil, fmt.Errorf("keyword candidate limit must be greater than zero")
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

func (r *HybridRetriever) loadAuthorizedResults(
	ctx context.Context,
	actorSubject string,
	candidates []Candidate,
	topK int,
) ([]Result, error) {
	if r == nil {
		return nil, fmt.Errorf("knowledge retriever is nil")
	}
	if r.Client == nil {
		return nil, fmt.Errorf("knowledge ent client is required")
	}
	if topK <= 0 {
		return nil, fmt.Errorf("knowledge topK must be greater than zero")
	}
	if len(candidates) == 0 {
		return []Result{}, nil
	}

	ids := make([]int, 0, len(candidates))
	seen := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.ChunkID <= 0 ||
			candidate.ChunkID > int64(^uint(0)>>1) {
			continue
		}
		if _, exists := seen[candidate.ChunkID]; exists {
			continue
		}

		seen[candidate.ChunkID] = struct{}{}
		ids = append(ids, int(candidate.ChunkID))
	}
	if len(ids) == 0 {
		return []Result{}, nil
	}

	chunks, err := r.Client.DocumentChunk.Query().
		Where(
			documentchunk.IDIn(ids...),
			documentchunk.VectorStatusEQ(documentchunk.VectorStatusIndexed),
			documentchunk.HasDocumentWith(
				document.StatusEQ(document.StatusReady),
				document.Or(
					document.VisibilityEQ(document.VisibilitySystem),
					document.And(
						document.VisibilityEQ(document.VisibilityPrivate),
						document.OwnerSubjectEQ(actorSubject),
					),
				),
			),
		).
		WithDocument().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load authorized knowledge results: %w", err)
	}

	byID := make(map[int64]*ent.DocumentChunk, len(chunks))
	for _, chunk := range chunks {
		if chunk.Edges.Document != nil {
			byID[int64(chunk.ID)] = chunk
		}
	}

	results := make([]Result, 0, min(topK, len(candidates)))
	remainingBytes := r.MaxResultBytes
	for _, candidate := range candidates {
		if len(results) == topK {
			break
		}
		chunk, found := byID[candidate.ChunkID]
		if !found {
			continue
		}

		content := chunk.Content
		if remainingBytes > 0 {
			content = truncateUTF8(content, remainingBytes)
			if content == "" && chunk.Content != "" {
				break
			}
			remainingBytes -= len(content)
		}

		headingPath := ""
		if chunk.HeadingPath != nil {
			headingPath = *chunk.HeadingPath
		}
		results = append(results, Result{
			ChunkID:     candidate.ChunkID,
			CitationID:  chunk.CitationID,
			Source:      chunk.Edges.Document.Source,
			Title:       chunk.Edges.Document.Title,
			HeadingPath: headingPath,
			StartLine:   chunk.StartLine,
			EndLine:     chunk.EndLine,
			Content:     content,
			Score:       candidate.Score,
		})
	}

	return results, nil
}

func truncateUTF8(value string, maximumBytes int) string {
	if maximumBytes <= 0 {
		return ""
	}
	if len(value) <= maximumBytes {
		return value
	}

	end := maximumBytes
	for end > 0 && (value[end]&0xc0) == 0x80 {
		end--
	}
	if end == 0 {
		return ""
	}

	return value[:end]
}
