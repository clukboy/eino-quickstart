package domain

type RetrievalResult struct {
	Query Query

	Candidates []*Candidate

	Debug *RetrievalDebug
}

type RetrievalDebug struct {
	ParsedQuery Query

	Channels map[string][]*Candidate

	Fused []*Candidate

	Reranked []*Candidate

	Latency RetrievalLatency
}

type RetrievalLatency struct {
	AnalyzeMS  int64
	RetrieveMS int64
	FusionMS   int64
	RerankMS   int64
	TotalMS    int64
}
