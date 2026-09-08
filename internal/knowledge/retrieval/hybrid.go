package retrieval

import (
	context "context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/ent/knowledgebase"
	"eino-quickstart/ent/predicate"
	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/vectorstore"
	"errors"
	"fmt"
	"strings"
)

type HybridRetrieverConfig struct {
	Client          *ent.Client
	Embedder        embedding.Embedder
	VectorStore     vectorstore.Store
	KeywordSearcher KeywordSearcher
	ProductSearcher *ProductSearcher

	DefaultTopK        int
	MaxTopK            int
	VectorCandidates   int
	KeywordCandidates  int
	ExactCandidates    int
	MaxQueryCharacters int
	MaxResultBytes     int
	VectorWeight       float64
	KeywordWeight      float64
	ExactWeight        float64
	RRFSmoothing       int
}

type HybridRetriever struct {
	client      *ent.Client
	embedder    embedding.Embedder
	vectorStore vectorstore.Store

	keywordSearcher KeywordSearcher
	productSearcher *ProductSearcher

	defaultTopK        int
	maxTopK            int
	vectorCandidates   int
	keywordCandidates  int
	exactCandidates    int
	maxQueryCharacters int
	maxResultBytes     int

	vectorWeight  float64
	keywordWeight float64
	exactWeight   float64

	rrfSmoothing int
}

func NewHybridRetriever(config HybridRetrieverConfig) (*HybridRetriever, error) {
	if config.Client == nil {
		return nil, errors.New("knowledge ent client is required")
	}
	if config.Embedder == nil {
		return nil, errors.New("knowledge embedder is required")
	}
	if config.VectorStore == nil {
		return nil, errors.New("knowledge vector store is required")
	}
	if config.KeywordSearcher == nil {
		return nil, errors.New("knowledge keyword searcher is required")
	}
	if config.DefaultTopK <= 0 || config.MaxTopK < config.DefaultTopK {
		return nil, errors.New("knowledge topK limits are invalid")
	}
	if config.VectorCandidates <= 0 || config.KeywordCandidates <= 0 ||
		config.ExactCandidates <= 0 {
		return nil, errors.New("retrieval candidate limits must be greater than zero")
	}
	if config.RRFSmoothing <= 0 {
		return nil, errors.New("retrieval RRF smoothing must be greater than zero")
	}
	return &HybridRetriever{
		client: config.Client, embedder: config.Embedder, vectorStore: config.VectorStore,
		keywordSearcher: config.KeywordSearcher, productSearcher: config.ProductSearcher,
		defaultTopK: config.DefaultTopK, maxTopK: config.MaxTopK,
		vectorCandidates: config.VectorCandidates, keywordCandidates: config.KeywordCandidates,
		exactCandidates: config.ExactCandidates, maxQueryCharacters: config.MaxQueryCharacters,
		maxResultBytes: config.MaxResultBytes, vectorWeight: config.VectorWeight,
		keywordWeight: config.KeywordWeight, exactWeight: config.ExactWeight,
		rrfSmoothing: config.RRFSmoothing,
	}, nil
}

func (r *HybridRetriever) Search(ctx context.Context, request SearchRequest) ([]Result, error) {
	debugResult, err := r.DebugSearch(ctx, request)
	if err != nil {
		return nil, err
	}

	return debugResult.FinalResults, nil
}

func (r *HybridRetriever) DebugSearch(ctx context.Context, request SearchRequest) (*DebugResult, error) {
	if r == nil {
		return nil, fmt.Errorf("knowledge retriever is nil")
	}
	if r.client == nil {
		return nil, fmt.Errorf("knowledge ent client is required")
	}
	if r.embedder == nil {
		return nil, fmt.Errorf("knowledge embedder is required")
	}
	if r.vectorStore == nil {
		return nil, fmt.Errorf("knowledge vector store is required")
	}
	if r.keywordSearcher == nil {
		return nil, fmt.Errorf("knowledge keyword searcher is required")
	}

	request.Query = strings.TrimSpace(request.Query)

	if request.Query == "" {
		return nil, fmt.Errorf("knowledge query is required")
	}

	if r.maxQueryCharacters > 0 &&
		len([]rune(request.Query)) > r.maxQueryCharacters {
		return nil, fmt.Errorf("knowledge query exceeds maximum length")
	}

	if request.TopK == 0 {
		request.TopK = r.defaultTopK
	}

	if request.TopK <= 0 {
		return nil, fmt.Errorf("knowledge topK must be greater than zero")
	}

	if r.maxTopK > 0 && request.TopK > r.maxTopK {
		return nil, fmt.Errorf("knowledge topK exceeds maximum")
	}

	scope := request.Scope()

	debugResult := &DebugResult{Query: request.Query}
	if !scope.HasKnowledgeBases() {
		return debugResult, nil
	}

	queryInfo := ParseQuery(request.Query)
	keywordQuery := BuildKeywordQuery(queryInfo)
	if keywordQuery == "" {
		keywordQuery = request.Query
	}
	var vectorCandidates []Candidate

	vectors, err := r.embedder.Embed(ctx, []string{request.Query})
	if err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}
	if len(vectors) != 1 {
		err = fmt.Errorf("query embedding response has invalid count")
	}

	vectorResults, err := r.vectorStore.Search(ctx, vectors[0], r.vectorCandidates)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	vectorCandidates = make([]Candidate, 0, len(vectorResults))

	for _, item := range vectorResults {
		vectorCandidates = append(vectorCandidates,
			Candidate{
				ChunkID: item.ChunkID,
				Score:   item.Score,
			},
		)
	}

	vectorCandidates, err = r.filterAuthorizedCandidates(ctx, scope, vectorCandidates)
	if err != nil {
		return nil, fmt.Errorf("filter vector candidates: %w", err)
	}
	debugResult.VectorResults = vectorCandidates

	var exactCandidates []Candidate
	if queryInfo.HasModel && r.productSearcher != nil {
		exactCandidates, err = r.productSearcher.SearchModel(ctx, scope, queryInfo.Model, r.exactCandidates)
		if err != nil {
			return nil, fmt.Errorf("exact product search: %w", err)
		}
	}
	debugResult.ExactResults = exactCandidates

	keywordCandidates, err := r.keywordSearcher.Search(ctx, scope, keywordQuery, r.keywordCandidates)
	if err != nil {
		return nil, fmt.Errorf("keyword search: %w", err)
	}

	debugResult.KeywordResults = keywordCandidates

	fused := FuseRRF(r.rrfSmoothing,
		WeightedCandidates{
			Items:  exactCandidates,
			Weight: r.exactWeight,
		},
		WeightedCandidates{
			Items:  vectorCandidates,
			Weight: r.vectorWeight,
		},
		WeightedCandidates{
			Items:  keywordCandidates,
			Weight: r.keywordWeight,
		},
	)

	debugResult.FusedResults = fused

	finalResults, err := r.loadAuthorizedResults(ctx, scope, fused, request.TopK)
	if err != nil {
		return nil, err
	}
	debugResult.FinalResults = finalResults

	return debugResult, nil
}

func (r *HybridRetriever) filterAuthorizedCandidates(ctx context.Context, scope SearchScope, candidates []Candidate) ([]Candidate, error) {
	if !scope.HasKnowledgeBases() || len(candidates) == 0 {
		return []Candidate{}, nil
	}

	scope = scope.Normalized()

	ids := make([]uint64, 0, len(candidates))
	seen := make(map[uint64]struct{}, len(candidates))

	for _, candidate := range candidates {
		if candidate.ChunkID <= 0 {
			continue
		}
		if _, exists := seen[candidate.ChunkID]; exists {
			continue
		}
		seen[candidate.ChunkID] = struct{}{}

		ids = append(ids, candidate.ChunkID)
	}
	if len(ids) == 0 {
		return []Candidate{}, nil
	}
	chunks, err := r.client.DocumentChunk.Query().
		Where(
			documentchunk.IDIn(ids...),
			documentchunk.HasDocumentWith(authorizedDocumentPredicates(scope)...),
		).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load authorized candidates: %w", err)
	}
	allowed := make(map[uint64]struct{}, len(chunks))

	for _, chunk := range chunks {
		allowed[chunk.ID] = struct{}{}
	}
	result := make([]Candidate, 0, len(candidates))

	for _, candidate := range candidates {
		if _, ok := allowed[candidate.ChunkID]; !ok {
			continue
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (r *HybridRetriever) loadAuthorizedResults(ctx context.Context, scope SearchScope, candidates []Candidate, topK int) ([]Result, error) {
	if r == nil {
		return nil, fmt.Errorf("knowledge retriever is nil")
	}
	if r.client == nil {
		return nil, fmt.Errorf("knowledge ent client is required")
	}
	if topK <= 0 {
		return nil, fmt.Errorf("knowledge topK must be greater than zero")
	}
	if !scope.HasKnowledgeBases() || len(candidates) == 0 {
		return []Result{}, nil
	}

	scope = scope.Normalized()

	ids := make([]uint64, 0, len(candidates))
	seen := make(map[uint64]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.ChunkID <= 0 ||
			candidate.ChunkID > uint64(^uint(0)>>1) {
			continue
		}
		if _, exists := seen[candidate.ChunkID]; exists {
			continue
		}

		seen[candidate.ChunkID] = struct{}{}
		ids = append(ids, candidate.ChunkID)
	}
	if len(ids) == 0 {
		return []Result{}, nil
	}

	chunks, err := r.client.DocumentChunk.Query().
		Where(
			documentchunk.IDIn(ids...),
			documentchunk.VectorStatusEQ(documentchunk.VectorStatusIndexed),
			documentchunk.HasDocumentWith(authorizedDocumentPredicates(scope)...),
		).
		WithDocument().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load authorized knowledge results: %w", err)
	}

	byID := make(map[uint64]*ent.DocumentChunk, len(chunks))
	for _, chunk := range chunks {
		if chunk.Edges.Document != nil {
			byID[chunk.ID] = chunk
		}
	}

	results := make([]Result, 0, min(topK, len(candidates)))
	remainingBytes := r.maxResultBytes
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

func authorizedDocumentPredicates(scope SearchScope) []predicate.Document {
	scope = scope.Normalized()
	predicates := []predicate.Document{
		document.StatusEQ(document.StatusReady),
		document.HasKnowledgeBaseWith(
			knowledgebase.StatusEQ(knowledgebase.StatusACTIVE),
		),
	}
	predicates = append(
		predicates,
		document.KnowledgeBaseIDIn(scope.KnowledgeBaseIDs...),
	)
	return predicates
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
