package retrieval

import (
	"context"
	"eino-quickstart/internal/rag/domain"
	"time"
)

type QueryAnalyzer interface {
	Analyze(ctx context.Context, query domain.Query) (domain.Query, error)
}

type Pipeline struct {
	Analyzer QueryAnalyzer

	Retrievers map[string]Retriever

	Fusion FusionStrategy

	Reranker Reranker
}

func (p *Pipeline) Retrieve(ctx context.Context, query domain.Query) (*domain.RetrievalResult, error) {
	start := time.Now()

	if p.Analyzer != nil {
		var err error
		query, err = p.Analyzer.Analyze(ctx, query)
		if err != nil {
			return nil, err
		}
	}

	analyzeMS := time.Since(start).Milliseconds()
	if query.Options.TopK <= 0 {
		query.Options.TopK = 10
	}

	retrieveStart := time.Now()
	channels := make(map[string][]*domain.Candidate)

	for name, retriever := range p.Retrievers {
		enabled := true
		switch name {
		case "dense":
			enabled = query.Options.UseDense
		case "sparse":
			enabled = query.Options.UseSparse
		case "keyword":
			enabled = query.Options.UseKeyword
		case "exact":
			enabled = query.Options.UseExact
		}

		if !enabled {
			continue
		}

		candidates, err := retriever.Retrieve(
			ctx,
			query,
			Options{
				TopK:           query.Options.TopK,
				ScoreThreshold: query.Options.ScoreThreshold,
				Debug:          query.Options.IncludeDebug,
			},
		)

		if err != nil {
			return nil, err
		}
		channels[name] = candidates
	}

	retrieveMS := time.Since(retrieveStart).Milliseconds()
	fusionStart := time.Now()
	var fused []*domain.Candidate
	if p.Fusion != nil {
		fused = p.Fusion.Fuse(channels)
	}

	fusionMS := time.Since(fusionStart).Milliseconds()
	rerankStart := time.Now()
	reranked := fused
	if query.Options.UseRerank && p.Reranker != nil {
		var err error
		reranked, err = p.Reranker.Rerank(ctx, query, fused)
		if err != nil {
			return nil, err
		}
	}

	rerankMS := time.Since(rerankStart).Milliseconds()
	totalMS := time.Since(start).Milliseconds()
	result := &domain.RetrievalResult{
		Query:      query,
		Candidates: reranked,
	}

	if query.Options.IncludeDebug {
		result.Debug = &domain.RetrievalDebug{
			ParsedQuery: query,
			Channels:    channels,
			Fused:       fused,
			Reranked:    reranked,
			Latency: domain.RetrievalLatency{
				AnalyzeMS:  analyzeMS,
				RetrieveMS: retrieveMS,
				FusionMS:   fusionMS,
				RerankMS:   rerankMS,
				TotalMS:    totalMS,
			},
		}
	}

	return result, nil
}
