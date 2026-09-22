// Package eval 是检索召回的离线评测域。
//
// 它只做三件事：装载金标用例、驱动检索、把命中折算成指标。检索本身通过
// Searcher 接口注入，因此这里的指标计算可以脱离 Milvus / embedding / Redis
// 单独验证 —— 评测工具自己的正确性和被测系统的正确性必须能分开排查。
package eval

import (
	"context"
	"time"

	"eino-quickstart/internal/rag/grouping"
)

// Hit 是检索侧原始返回的一条结果 —— 一个分块，不是一篇文档。
//
// 保留分块粒度是必要的：Content 只有分块级才有（正文按块存），检索器也只按块
// 打分。但**判定与展示都不该停在分块上**，见 ResultHit。
type Hit struct {
	ChunkID     int64
	Source      string
	Title       string
	HeadingPath string
	Score       float64
	Content     string
}

// ResultHit 是召回结果里的一条明细，粒度由 grouping.Policy 决定（见 types 上的
// Report.Granularity）。它在两种粒度下含义不同，这是刻意的：
//
//   - document：一条 = 一篇文档，Chunks 是归并掉的分块数，ChunkID 取代表块。
//   - chunk：一条 = 一个分块，Chunks 恒为 1，ChunkID 就是它自己。
//
// 为什么不把粒度写死成文档：产品型录一篇文档就是一个产品，归并成文档才对；普通
// 文档库里一篇长文切成几百块，归并成一条等于什么都没返回。同一个评测工具要能
// 评这两类库，粒度就必须是可配的。
//
// 归并到文档时的代表块取**得分最高**的那一块（正文、标题路径、ChunkID 都取它），
// 另一并把命中块数带上：带上是为了不丢排障信息 ——「这篇文档进了 TopK」和
// 「这篇文档占掉了 TopK 里的 6 个位置」是两回事，后者说明切块过碎、正在挤掉
// 别的内容。
type ResultHit struct {
	// Rank 是按首次出现次序定的名次（1 起）。
	Rank        int     `json:"rank"`
	Source      string  `json:"source"`
	Title       string  `json:"title,omitempty"`
	ChunkID     int64   `json:"chunk_id,omitempty"`
	HeadingPath string  `json:"heading_path,omitempty"`
	Score       float64 `json:"score"`
	// Chunks 是这一条聚合掉的分块数：document 粒度下是这篇文档的命中块数，
	// chunk 粒度下恒为 1。
	Chunks int `json:"chunks"`
	// Content 是代表块的正文。document 粒度下是得分最高那一块。
	Content string `json:"content,omitempty"`
}

// Searcher 是评测对检索侧的全部依赖。
//
// 只有一个方法：组合根用 rag.HybridRetriever 适配，单测用内存桩。
//
// topK 是**分块预算**而不是结果条数：检索侧的单位是分块，条数的单位由归并粒度
// 决定，两者的折算由 Runner 完成（见 searchFilled）。把它当条数用，评测就与
// 线上不是同一个口径 —— document 粒度下会少给几倍的内容。
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
	// 单位是**文档**而不是分块：切块粒度是配置项（chunk_size），把它当条数会让
	// 同一个问题在调小分块后就"有答案"了。
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

	// Hits 是**结果条数**（= len(Results)），口径跟着归并粒度走：document 粒度下
	// 是文档数，chunk 粒度下是分块数。Chunks 恒为原始分块命中数 —— 两个都留,
	// 是为了在归并之后仍然看得出「切块有没有被同一篇文档挤满」。
	//
	// 注意 Recall / MRR / FirstHitRank 的口径**不跟粒度走**，恒按文档去重：
	// 那是质量指标，让它随切块粒度变化等于奖励切得更碎。
	Hits       int   `json:"hits"`
	Chunks     int   `json:"chunks"`
	DurationMS int64 `json:"duration_ms"`

	// Results 是召回结果明细，按名次排列，粒度见 Report.Granularity。
	Results         []ResultHit `json:"results,omitempty"`
	Missing         []string    `json:"missing_sources,omitempty"`
	MissingKeywords []string    `json:"missing_keywords,omitempty"`
	Leaked          []string    `json:"leaked_sources,omitempty"`
	Error           string      `json:"error,omitempty"`

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
	GeneratedAt time.Time `json:"generated_at"`
	TopK        int       `json:"top_k"`

	// Granularity 是这一轮召回结果的归并粒度。它必须落进报告：同一份用例集在
	// 两种粒度下的 Hits 不是同一个单位，报告离开当时的上下文之后，
	// 「hits=3」是 3 篇文档还是 3 个分块就没人知道了。
	Granularity grouping.Granularity `json:"granularity"`

	Summary Summary      `json:"summary"`
	Scenes  []SceneStat  `json:"scenes,omitempty"`
	Cases   []CaseResult `json:"cases"`

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
