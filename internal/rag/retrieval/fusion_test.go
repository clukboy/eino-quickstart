package retrieval

import (
	"testing"

	"eino-quickstart/internal/rag/domain"
)

func TestRRF_Fuse(t *testing.T) {
	c1 := &domain.Candidate{
		Chunk: &domain.Chunk{
			ID: "chunk-1",
		},
		Score: 0.9,
	}

	c2 := &domain.Candidate{
		Chunk: &domain.Chunk{
			ID: "chunk-2",
		},
		Score: 0.8,
	}

	c3 := &domain.Candidate{
		Chunk: &domain.Chunk{
			ID: "chunk-3",
		},
		Score: 0.7,
	}

	rrf := NewRRF(60, 10)

	result := rrf.Fuse(
		map[string][]*domain.Candidate{
			"dense": {
				c1,
				c2,
			},
			"sparse": {
				c2,
				c3,
			},
		},
	)

	if len(result) != 3 {
		t.Fatalf(
			"expected 3 candidates, got %d",
			len(result),
		)
	}

	if result[0].Chunk.ID != "chunk-2" {
		t.Fatalf(
			"expected chunk-2 to rank first, got %s",
			result[0].Chunk.ID,
		)
	}

	if result[0].Signals.DenseScore != 0.8 {
		t.Fatalf(
			"unexpected dense score: %f",
			result[0].Signals.DenseScore,
		)
	}

	if result[0].Signals.SparseScore != 0.8 {
		t.Fatalf(
			"unexpected sparse score: %f",
			result[0].Signals.SparseScore,
		)
	}
}

func TestRRF_TopK(t *testing.T) {
	rrf := NewRRF(60, 2)

	result := rrf.Fuse(
		map[string][]*domain.Candidate{
			"dense": {
				{
					Chunk: &domain.Chunk{ID: "1"},
				},
				{
					Chunk: &domain.Chunk{ID: "2"},
				},
				{
					Chunk: &domain.Chunk{ID: "3"},
				},
			},
		},
	)

	if len(result) != 2 {
		t.Fatalf(
			"expected top 2, got %d",
			len(result),
		)
	}
}

func TestRRF_SkipInvalidCandidate(t *testing.T) {
	rrf := NewRRF(60, 10)

	result := rrf.Fuse(
		map[string][]*domain.Candidate{
			"dense": {
				nil,
				{
					Chunk: nil,
				},
				{
					Chunk: &domain.Chunk{
						ID: "valid",
					},
				},
			},
		},
	)

	if len(result) != 1 {
		t.Fatalf(
			"expected 1 candidate, got %d",
			len(result),
		)
	}

	if result[0].Chunk.ID != "valid" {
		t.Fatalf(
			"unexpected candidate: %s",
			result[0].Chunk.ID,
		)
	}
}
