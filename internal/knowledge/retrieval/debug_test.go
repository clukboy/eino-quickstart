package retrieval

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFuseRRF(t *testing.T) {
	vector := []Candidate{
		{
			ChunkID: 1,
			Score:   0.95,
		},
		{
			ChunkID: 2,
			Score:   0.90,
		},
		{
			ChunkID: 3,
			Score:   0.85,
		},
	}

	keyword := []Candidate{
		{
			ChunkID: 2,
			Score:   1.0,
		},
		{
			ChunkID: 3,
			Score:   0.9,
		},
		{
			ChunkID: 4,
			Score:   0.8,
		},
	}

	results := FuseRRF(
		60,
		WeightedCandidates{
			Items:  vector,
			Weight: 1,
		},
		WeightedCandidates{
			Items:  keyword,
			Weight: 1,
		},
	)

	require.Len(t, results, 4)

	// Chunk 2 同时出现在 Vector 和 Keyword，
	// 应该拥有最高 RRF 分数。
	require.Equal(t, int64(2), results[0].ChunkID)

	// Chunk 3 也是双路召回，但排名低于 Chunk 2。
	require.Equal(t, int64(3), results[1].ChunkID)
}

func TestFuseRRFWeight(t *testing.T) {
	vector := []Candidate{
		{
			ChunkID: 1,
			Score:   0.95,
		},
	}

	keyword := []Candidate{
		{
			ChunkID: 2,
			Score:   1.0,
		},
	}

	results := FuseRRF(60,
		WeightedCandidates{
			Items:  vector,
			Weight: 2,
		},
		WeightedCandidates{
			Items:  keyword,
			Weight: 1,
		},
	)

	require.Len(t, results, 2)

	require.Equal(
		t,
		int64(1),
		results[0].ChunkID,
	)
}
