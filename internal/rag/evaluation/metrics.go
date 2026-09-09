package evaluation

type Metrics struct {
	RecallAt1  float64
	RecallAt3  float64
	RecallAt5  float64
	RecallAt10 float64

	MRR  float64
	NDCG float64

	LatencyMS float64
}
