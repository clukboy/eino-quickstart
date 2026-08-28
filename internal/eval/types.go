package eval

type RetrievalCase struct {
	ID               string   `json:"id"`
	ActorSubject     string   `json:"actor_subject"`
	Query            string   `json:"query"`
	ExpectedSources  []string `json:"expected_sources"`
	ForbiddenSources []string `json:"forbidden_sources"`
	MinResults       int      `json:"min_results"`
}

type ToolPolicyCase struct {
	ID               string `json:"id"`
	Tool             string `json:"tool"`
	Allowed          bool   `json:"allowed"`
	RequiresApproval bool   `json:"requires_approval"`
}

type CaseResult struct {
	ID       string         `json:"id"`
	Passed   bool           `json:"passed"`
	Duration int64          `json:"duration_ms"`
	Error    string         `json:"error,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
}

type Summary struct {
	Total        int     `json:"total"`
	Passed       int     `json:"passed"`
	Failed       int     `json:"failed"`
	PassRate     float64 `json:"pass_rate"`
	RecallAtK    float64 `json:"recall_at_k"`
	ACLLeakCount int     `json:"acl_leak_count"`
	P95LatencyMS int64   `json:"p95_latency_ms"`
}

type Report struct {
	Version string       `json:"version"`
	Summary Summary      `json:"summary"`
	Results []CaseResult `json:"results"`
}
