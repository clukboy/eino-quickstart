package eval

import (
	"strings"
	"testing"

	"eino-quickstart/internal/rag/grouping"
)

func hit(source string, score float64) Hit {
	return Hit{Source: source, Score: score}
}

func TestEvaluateCaseMatchesPreciseSourceAndKeyword(t *testing.T) {
	testCase := Case{
		ID:               "mixed",
		Query:            "H105P",
		Expected:         []string{"documents/2/manual.md"},
		ExpectedKeywords: []string{"h11-二段力滑入式铰链"},
	}
	hits := []Hit{
		hit("documents/2/h11-二段力滑入式铰链-md-abc.md", 0.9),
		hit("documents/2/manual.md", 0.8),
	}

	result := evaluateCase(testCase, hits, 12, grouping.Document)

	if !result.Passed {
		t.Fatalf("期望通过，实际失败: %s", result.Error)
	}
	if result.Recall != 1 {
		t.Fatalf("Recall 期望 1，实际 %v", result.Recall)
	}
	// 关键词命中的是第 1 名，精确命中的是第 2 名，首个命中名次取最早的。
	if result.FirstHitRank != 1 {
		t.Fatalf("FirstHitRank 期望 1，实际 %d", result.FirstHitRank)
	}
}

func TestEvaluateCaseKeywordIsCaseInsensitive(t *testing.T) {
	testCase := Case{ID: "case", Query: "h105p", ExpectedKeywords: []string{"H105P"}}
	hits := []Hit{hit("documents/2/h105p-manual.md", 1)}

	result := evaluateCase(testCase, hits, 1, grouping.Document)
	if !result.Passed {
		t.Fatalf("关键词匹配应忽略大小写，实际失败: %s", result.Error)
	}
}

// 同一篇文档切成很多块时，命中只能算一次。不做去重的话，切得越碎指标越好看，
// 而 MRR 会被切块粒度污染 —— 这是评测最该避免的激励。
func TestEvaluateCaseDedupesBySource(t *testing.T) {
	testCase := Case{
		ID:               "dedupe",
		Query:            "二段力",
		ExpectedKeywords: []string{"h11-二段力"},
	}
	// 前 3 条都是同一篇文档的不同分块，第 4 条才是另一篇。
	hits := []Hit{
		hit("documents/2/h11-二段力.md", 0.9),
		hit("documents/2/h11-二段力.md", 0.85),
		hit("documents/2/h11-二段力.md", 0.8),
		hit("documents/2/other.md", 0.7),
	}

	result := evaluateCase(testCase, hits, 1, grouping.Document)

	if len(result.Results) != 2 {
		t.Fatalf("召回应去重到 2 条，实际 %d: %v", len(result.Results), result.Results)
	}
	if result.FirstHitRank != 1 {
		t.Fatalf("FirstHitRank 期望 1，实际 %d", result.FirstHitRank)
	}
	// 归并掉的分块数要留着：一篇文档占掉三个名次是切块过碎的信号。
	if result.Chunks != 4 || result.Hits != 2 {
		t.Fatalf("分块数 4 / 结果条数 2 才说明归并生效，实际 chunks=%d hits=%d",
			result.Chunks, result.Hits)
	}
}

// 按文档归并时，代表块取得分最高的那一块 —— 它的正文与标题路径最能解释
// 「为什么命中」，用错块会让排障时看到一段不相干的文字。
func TestEvaluateCaseDocumentKeepsBestChunkAsRepresentative(t *testing.T) {
	testCase := Case{ID: "best", Query: "二段力", ExpectedKeywords: []string{"h11"}}
	hits := []Hit{
		{Source: "documents/2/h11.md", Score: 0.4, HeadingPath: "低分块", Content: "低分内容"},
		{Source: "documents/2/h11.md", Score: 0.9, HeadingPath: "高分块", Content: "高分内容"},
		{Source: "documents/2/h11.md", Score: 0.6, HeadingPath: "中分块", Content: "中分内容"},
	}

	result := evaluateCase(testCase, hits, 1, grouping.Document)

	if len(result.Results) != 1 {
		t.Fatalf("应归并成 1 条，实际 %d", len(result.Results))
	}
	got := result.Results[0]
	if got.Score != 0.9 || got.HeadingPath != "高分块" || got.Content != "高分内容" {
		t.Fatalf("代表块应取最高分的那一块，实际 %+v", got)
	}
	if got.Chunks != 3 {
		t.Fatalf("命中块数期望 3，实际 %d", got.Chunks)
	}
}

// chunk 粒度下一条结果就是一个分块，不归并 —— 普通文档库里一篇长文切成几百块，
// 归并成一条等于什么都没返回。
func TestEvaluateCaseChunkGranularityKeepsEveryChunk(t *testing.T) {
	testCase := Case{ID: "chunks", Query: "二段力", ExpectedKeywords: []string{"h11"}}
	hits := []Hit{
		{ChunkID: 11, Source: "documents/2/h11.md", Score: 0.9, Content: "第一块"},
		{ChunkID: 12, Source: "documents/2/h11.md", Score: 0.8, Content: "第二块"},
		{ChunkID: 13, Source: "documents/2/other.md", Score: 0.7, Content: "另一篇"},
	}

	result := evaluateCase(testCase, hits, 1, grouping.Chunk)

	if len(result.Results) != 3 {
		t.Fatalf("chunk 粒度不该归并，期望 3 条，实际 %d", len(result.Results))
	}
	if result.Results[1].ChunkID != 12 || result.Results[1].Content != "第二块" {
		t.Fatalf("chunk 粒度下每条要保留自己的分块身份，实际 %+v", result.Results[1])
	}
	if result.Results[1].Chunks != 1 {
		t.Fatalf("chunk 粒度下每条的块数恒为 1，实际 %d", result.Results[1].Chunks)
	}
}

// 质量指标不能跟着归并粒度走：同一份命中在两种粒度下 Recall 与 MRR 必须一致，
// 否则「把 chunkSize 调小」就能把分数刷上去。
func TestMetricsIgnoreGranularity(t *testing.T) {
	testCase := Case{
		ID:               "same",
		Query:            "二段力",
		ExpectedKeywords: []string{"h11"},
	}
	hits := []Hit{
		{Source: "documents/2/other.md", Score: 0.9},
		{Source: "documents/2/h11.md", Score: 0.8},
		{Source: "documents/2/h11.md", Score: 0.7},
	}

	document := evaluateCase(testCase, hits, 1, grouping.Document)
	chunk := evaluateCase(testCase, hits, 1, grouping.Chunk)

	if document.Recall != chunk.Recall {
		t.Fatalf("Recall 不该随粒度变: document=%v chunk=%v", document.Recall, chunk.Recall)
	}
	if document.FirstHitRank != chunk.FirstHitRank {
		t.Fatalf("FirstHitRank 不该随粒度变: document=%d chunk=%d",
			document.FirstHitRank, chunk.FirstHitRank)
	}
	if document.ReciprocalRank != chunk.ReciprocalRank {
		t.Fatalf("MRR 不该随粒度变: document=%v chunk=%v",
			document.ReciprocalRank, chunk.ReciprocalRank)
	}
	// 而条数就该不同 —— 那正是粒度在起作用的地方。
	if document.Hits != 2 || chunk.Hits != 3 {
		t.Fatalf("条数应随粒度变化：document=%d chunk=%d", document.Hits, chunk.Hits)
	}
}

// MinResults 量的是「调用方拿到几条」，所以它跟粒度走；报错信息里要带单位,
// 否则看的人会按自己以为的粒度去理解。
func TestMinResultsFollowsGranularity(t *testing.T) {
	testCase := Case{ID: "min", Query: "有没有结果", MinResults: 2}
	// 一篇文档的两个块：chunk 粒度下是 2 条，document 粒度下只有 1 条。
	hits := []Hit{
		{Source: "documents/2/a.md", Score: 0.9},
		{Source: "documents/2/a.md", Score: 0.8},
	}

	if !evaluateCase(testCase, hits, 1, grouping.Chunk).Passed {
		t.Fatal("chunk 粒度下有 2 条，应满足 min_results=2")
	}
	document := evaluateCase(testCase, hits, 1, grouping.Document)
	if document.Passed {
		t.Fatal("document 粒度下只有 1 条，应判失败")
	}
	if !strings.Contains(document.Error, "document") {
		t.Fatalf("条数不足的提示要写清粒度，实际 %q", document.Error)
	}
}

func TestEvaluateCaseForbiddenFailsTheCase(t *testing.T) {
	testCase := Case{
		ID:               "acl",
		Query:            "团队路线图",
		ExpectedKeywords: []string{"roadmap"},
		Forbidden:        []string{"documents/2/private-roadmap.md"},
	}
	hits := []Hit{
		hit("documents/2/team-roadmap.md", 0.9),
		hit("documents/2/private-roadmap.md", 0.8),
	}

	result := evaluateCase(testCase, hits, 1, grouping.Document)

	if result.Passed {
		t.Fatal("命中 forbidden 必须判失败，即使期望文档也命中了")
	}
	if len(result.Leaked) != 1 || result.Leaked[0] != "documents/2/private-roadmap.md" {
		t.Fatalf("Leaked 期望一条，实际 %v", result.Leaked)
	}
}

func TestEvaluateCasePartialRecall(t *testing.T) {
	testCase := Case{
		ID:               "partial",
		Query:            "多文档",
		ExpectedKeywords: []string{"alpha", "beta"},
	}
	hits := []Hit{hit("documents/2/alpha.md", 0.9)}

	result := evaluateCase(testCase, hits, 1, grouping.Document)

	if result.Recall != 0.5 {
		t.Fatalf("Recall 期望 0.5，实际 %v", result.Recall)
	}
	if result.Passed {
		t.Fatal("漏召了一条就该判失败")
	}
	if len(result.MissingKeywords) != 1 || result.MissingKeywords[0] != "beta" {
		t.Fatalf("MissingKeywords 期望 [beta]，实际 %v", result.MissingKeywords)
	}
}

// 无期望用例没有「该召回到什么」可谈，Recall 记 1；只有 Forbidden 与 MinResults 能判它。
func TestEvaluateCaseWithoutExpectationPasses(t *testing.T) {
	testCase := Case{ID: "no-answer", Query: "Kubernetes Ingress"}
	result := evaluateCase(testCase, nil, 5, grouping.Document)

	if !result.Passed {
		t.Fatalf("无期望用例不应因空结果失败: %s", result.Error)
	}
	if result.Recall != 1 {
		t.Fatalf("无期望用例 Recall 期望 1，实际 %v", result.Recall)
	}
}

func TestEvaluateCaseMinResults(t *testing.T) {
	testCase := Case{ID: "min", Query: "有没有结果", MinResults: 2}
	result := evaluateCase(testCase, []Hit{hit("documents/2/a.md", 1)}, 1, grouping.Document)

	if result.Passed {
		t.Fatal("结果条数少于 MinResults 应判失败")
	}
}

// 无答案用例不参与 Recall 宏平均，否则多塞几条无答案用例就能把指标"抬"上去。
func TestSummarizeExcludesNoAnswerCasesFromRecall(t *testing.T) {
	results := []CaseResult{
		{ExpectedCount: 1, Recall: 1, ReciprocalRank: 1, FirstHitRank: 1, Passed: true, DurationMS: 10, Hits: 3},
		{ExpectedCount: 1, Recall: 0, ReciprocalRank: 0, Passed: false, DurationMS: 20, Hits: 3},
		{ExpectedCount: 0, Recall: 1, Passed: true, DurationMS: 30, Hits: 0},
	}

	summary := summarize(results)

	if summary.Cases != 3 || summary.Passed != 2 || summary.Failed != 1 {
		t.Fatalf("用例计数错误: %+v", summary)
	}
	if summary.RecallAtK != 0.5 {
		t.Fatalf("Recall@K 期望 0.5（只算有期望的两条），实际 %v", summary.RecallAtK)
	}
	if summary.MRR != 0.5 {
		t.Fatalf("MRR 期望 0.5，实际 %v", summary.MRR)
	}
	if summary.HitRateAt1 != 0.5 {
		t.Fatalf("HitRate@1 期望 0.5，实际 %v", summary.HitRateAt1)
	}
	if summary.EmptyResultCases != 1 {
		t.Fatalf("空结果用例数期望 1，实际 %d", summary.EmptyResultCases)
	}
}

func TestSceneStatsGroupsByScene(t *testing.T) {
	results := []CaseResult{
		{Scene: "型号精确", ExpectedCount: 1, Recall: 1, ReciprocalRank: 1, Passed: true},
		{Scene: "型号精确", ExpectedCount: 1, Recall: 0, Passed: false},
		{Scene: "公司信息", ExpectedCount: 1, Recall: 1, ReciprocalRank: 0.5, Passed: true},
	}

	stats := sceneStats(results)

	if len(stats) != 2 {
		t.Fatalf("场景数期望 2，实际 %d", len(stats))
	}
	// 顺序按首次出现，报告里才稳定。
	if stats[0].Scene != "型号精确" || stats[0].Passed != 1 || stats[0].RecallAtK != 0.5 {
		t.Fatalf("型号精确场景统计错误: %+v", stats[0])
	}
	if stats[1].Scene != "公司信息" || stats[1].RecallAtK != 1 {
		t.Fatalf("公司信息场景统计错误: %+v", stats[1])
	}
}

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []int64{40, 10, 30, 20}

	if got := Percentile(values, 50); got != 20 {
		t.Fatalf("P50 期望 20，实际 %d", got)
	}
	// ceil(0.95*4) = 4 -> 最大值。最近秩法保证 P95 对应某个真实样本。
	if got := Percentile(values, 95); got != 40 {
		t.Fatalf("P95 期望 40，实际 %d", got)
	}
	if got := Percentile(values, 0); got != 10 {
		t.Fatalf("P0 应回落最小值 10，实际 %d", got)
	}
	if got := Percentile(nil, 95); got != 0 {
		t.Fatalf("空切片期望 0，实际 %d", got)
	}
	if got := Percentile([]int64{7}, 95); got != 7 {
		t.Fatalf("单样本期望 7，实际 %d", got)
	}
}

func TestThresholdsCheckReportsEveryViolation(t *testing.T) {
	limits := Thresholds{
		MinPassRate:     0.9,
		MinRecallAtK:    0.8,
		MinMRR:          0.7,
		MaxACLLeakCount: 0,
		MaxP95LatencyMS: 100,
	}
	summary := Summary{
		PassRate:     0.5,
		RecallAtK:    0.4,
		MRR:          0.3,
		ACLLeakCount: 2,
		P95LatencyMS: 900,
	}

	violations := limits.Check(summary)
	if len(violations) != 5 {
		t.Fatalf("五项都该报出来，实际 %d 项: %v", len(violations), violations)
	}
}

func TestThresholdsCheckPassesWhenWithinLimits(t *testing.T) {
	limits := Thresholds{MinPassRate: 0.8, MinRecallAtK: 0.7, MinMRR: 0.6, MaxP95LatencyMS: 5000}
	summary := Summary{PassRate: 0.9, RecallAtK: 0.8, MRR: 0.7, P95LatencyMS: 120}

	if violations := limits.Check(summary); len(violations) != 0 {
		t.Fatalf("达标时不应有违规项，实际 %v", violations)
	}
}
