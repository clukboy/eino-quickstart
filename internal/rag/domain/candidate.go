package domain

type Candidate struct {
	Chunk *Chunk

	Score float64

	Rank int

	Source string

	Signals CandidateSignals
}

type CandidateSignals struct {
	DenseScore    float64
	SparseScore   float64
	KeywordScore  float64
	MetadataScore float64
	ExactScore    float64

	FusionScore float64
	RerankScore float64
}
