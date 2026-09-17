// Package eval 是检索召回的离线评测域。
//
// 它只做三件事：装载金标用例、驱动检索、把命中折算成指标。检索本身通过
// Searcher 接口注入，因此这里的指标计算可以脱离 Milvus / embedding / Redis
// 单独验证 —— 评测工具自己的正确性和被测系统的正确性必须能分开排查。
package eval

import (
	"context"
	"time"
)

// Hit 是评测视角下的一条召回结果。
//
// Source 是判定命中的口径：一篇文档可能被切成几十个块，评测关心的是
// 「这篇文档有没有被找到」，所以命中按 Source 去重后再判定。
type Hit struct {
	ChunkID     int64
	Source      string
	Title       string
	HeadingPath string
	Score       float64
	Content     string
}

// Searcher 是评测对检索侧的全部依赖。
//
// 只有一个方法：组合根用 rag.HybridRetriever 适配，单测用内存桩。
type Searcher interface {
	Search(ctx context.Context, query string, topK int) ([]Hit, error)
}

// Case 是一条金标用例。
//
// 期望命中用两种口径表达，按 source 是否可预测来选：
//
//   - Expected：精确的 document.source。语料是自己注册的、路径可预知时用它。
//   - ExpectedKeywords：召回的 source 里需**包含**该片段。托管上传的正文由
//     ContentStore.Create 命名，路径里带着纳秒时间戳后缀，重新上传一次就变，
//     写精确值等于让用例集活不过一次重传。这类语料用关键词口径。
//
// 两者可以同时用，同一份期望不要重复写。
type Case struct {
	ID               string   `json:"id"`
	Query            string   `json:"query"`
	TopK             int      `json:"top_k,omitempty"`
	Expected         []string `json:"expected_sources,omitempty"`
	ExpectedKeywords []string `json:"expected_source_keywords,omitempty"`
	Forbidden        []string `json:"forbidden_sources,omitempty"`

	// MinResults 是「至少要有这么多条结果」的下限，用来表达"这个问题应该有答案"。
	// 无答案用例把它留空或设 1 —— 项目当前没有生成式回答，暂时无法断言"正确地没答"。
	MinResults int `json:"min_results,omitempty"`

	// Scene 是场景标签（型号精确 / 同义表达 / 跨文档 / 无答案 / 越权），
	// 报告按它分组，这样能一眼看出是某一类查询整体退化还是个别用例抖动。
	Scene string `json:"scene,omitempty"`
	Note  string `json:"note,omitempty"`
}

// CaseResult 是一条用例的评测结果。
type CaseResult struct {
	ID     string `json:"id"`
	Scene  string `json:"scene,omitempty"`
	Query  string `json:"query"`
	Passed bool   `json:"passed"`

	// ExpectedCount 是期望命中的文档数（精确 source 与关键词合计）。汇总时用它
	// 区分「有期望」与「无答案」用例：后者没有 Recall 概念，混进宏平均会把均值
	// 稀释成没有区分度的数。
	ExpectedCount int `json:"expected_count"`

	Recall float64 `json:"recall"`

	// FirstHitRank 是首个期望文档的名次（1 起）。0 表示完全没有命中。
	FirstHitRank   int     `json:"first_hit_rank"`
	ReciprocalRank float64 `json:"reciprocal_rank"`

	Hits            int      `json:"hits"`
	DurationMS      int64    `json:"duration_ms"`
	Retrieved       []string `json:"retrieved_sources,omitempty"`
	Missing         []string `json:"missing_sources,omitempty"`
	MissingKeywords []string `json:"missing_keywords,omitempty"`
	Leaked          []string `json:"leaked_sources,omitempty"`
	Error           string   `json:"error,omitempty"`

	// Note 从用例带过来。失败明细里显示它，是为了让读到报告的人知道这条用例
	// 的意图 —— 尤其那些探针性质的用例（明知可能失败，用来观察趋势）。
	Note string `json:"note,omitempty"`
}

// Summary 是整轮评测的汇总指标。
type Summary struct {
	Cases    int     `json:"cases"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	PassRate float64 `json:"pass_rate"`

	// RecallAtK 是宏平均：先算每条用例的 recall，再对用例取平均。
	// 不用「命中总数/期望总数」的微平均 —— 那样期望多的用例会主导指标，
	// 一条 20 个期望的用例能盖掉十条单期望用例的退化。
	RecallAtK  float64 `json:"recall_at_k"`
	MRR        float64 `json:"mrr"`
	HitRateAt1 float64 `json:"hit_rate_at_1"`

	ACLLeakCount     int `json:"acl_leak_count"`
	EmptyResultCases int `json:"empty_result_cases"`

	P50LatencyMS int64 `json:"p50_latency_ms"`
	P95LatencyMS int64 `json:"p95_latency_ms"`
}

// Report 是落盘与展示的完整结果。
type Report struct {
	GeneratedAt time.Time    `json:"generated_at"`
	TopK        int          `json:"top_k"`
	Summary     Summary      `json:"summary"`
	Scenes      []SceneStat  `json:"scenes,omitempty"`
	Cases       []CaseResult `json:"cases"`

	// Violations 是未达标的阈值项；为空表示通过门禁。
	Violations []string `json:"violations,omitempty"`
}

// SceneStat 是单个场景的聚合，用来定位退化发生在哪一类查询上。
type SceneStat struct {
	Scene     string  `json:"scene"`
	Cases     int     `json:"cases"`
	Passed    int     `json:"passed"`
	PassRate  float64 `json:"pass_rate"`
	RecallAtK float64 `json:"recall_at_k"`
	MRR       float64 `json:"mrr"`
}
