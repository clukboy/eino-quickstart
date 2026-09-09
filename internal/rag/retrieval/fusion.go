package retrieval

import (
	"eino-quickstart/internal/rag/domain"
	"sort"
)

type FusionStrategy interface {
	Fuse(results map[string][]*domain.Candidate) []*domain.Candidate
}

type RRF struct {
	K    int
	TopK int
}

func NewRRF(k, topK int) *RRF {
	if k <= 0 {
		k = 60
	}

	return &RRF{
		K:    k,
		TopK: topK,
	}
}

func (r *RRF) Fuse(results map[string][]*domain.Candidate) []*domain.Candidate {
	type scoreEntry struct {
		candidate *domain.Candidate
		score     float64
	}
	scores := make(map[string]*scoreEntry)

	for source, candidates := range results {
		for rank, candidate := range candidates {
			if candidate == nil || candidate.Chunk == nil {
				continue
			}
			id := candidate.Chunk.ID
			entry, ok := scores[id]
			if !ok {
				entry = &scoreEntry{
					candidate: candidate,
				}
				scores[id] = entry
			}
			entry.score += 1.0 / float64(r.K+rank+1)
			switch source {
			case "dense":
				entry.candidate.Signals.DenseScore = candidate.Score
			case "sparse":
				entry.candidate.Signals.SparseScore = candidate.Score
			case "keyword":
				entry.candidate.Signals.KeywordScore = candidate.Score
			case "exact":
				entry.candidate.Signals.ExactScore = candidate.Score
			}
			entry.candidate.Signals.FusionScore = entry.score
		}
	}

	output := make([]*domain.Candidate, 0, len(scores))

	for _, entry := range scores {
		entry.candidate.Score = entry.score
		output = append(output, entry.candidate)
	}

	sort.Slice(output, func(i, j int) bool { return output[i].Score > output[j].Score })

	if r.TopK > 0 && len(output) > r.TopK {
		output = output[:r.TopK]
	}

	for i, candidate := range output {
		candidate.Rank = i + 1
	}
	return output
}
