package retriever

import "sort"

type WeightedCandidates struct {
	Items  []Candidate
	Weight float64
}

func FuseRRF(smoothing int, inputs ...WeightedCandidates) []Candidate {
	scores := make(map[int64]float64)

	for _, input := range inputs {
		for index, item := range input.Items {
			rank := index + 1

			scores[item.ChunkID] += input.Weight /
				float64(smoothing+rank)
		}
	}

	result := make([]Candidate, 0, len(scores))

	for chunkID, score := range scores {
		result = append(result, Candidate{
			ChunkID: chunkID,
			Score:   score,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].ChunkID < result[j].ChunkID
		}

		return result[i].Score > result[j].Score
	})
	return result
}
