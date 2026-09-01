package retrieval

import (
	"testing"
)

func TestFuseRRFDeterministicOrder(t *testing.T) {
	got := FuseRRF(
		60,
		WeightedCandidates{
			Items: []Candidate{
				{
					ChunkID: 1,
					Score:   1,
				},
			},
			Weight: 1,
		},
		WeightedCandidates{
			Items: []Candidate{
				{
					ChunkID: 2,
					Score:   1,
				},
			},
			Weight: 1,
		},
	)

	if len(got) != 2 {
		t.Fatalf(
			"got %d candidates, want 2",
			len(got),
		)
	}

	if got[0].ChunkID >= got[1].ChunkID {
		t.Fatalf(
			"unexpected order: %#v",
			got,
		)
	}
}
