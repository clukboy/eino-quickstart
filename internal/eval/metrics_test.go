package eval

import (
	"testing"
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

	result := evaluateCase(testCase, hits, 12)

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

	result := evaluateCase(testCase, hits, 1)
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

	result := evaluateCase(testCase, hits, 1)

	if len(result.Retrieved) != 2 {
		t.Fatalf("召回应去重到 2 篇，实际 %d: %v", len(result.Retrieved), result.Retrieved)
	}
	if result.FirstHitRank != 1 {
		t.Fatalf("FirstHitRank 期望 1，实际 %d", result.FirstHitRank)
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

	result := evaluateCase(testCase, hits, 1)

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

	result := evaluateCase(testCase, hits, 1)

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
	result := evaluateCase(testCase, nil, 5)

	if !result.Passed {
		t.Fatalf("无期望用例不应因空结果失败: %s", result.Error)
	}
	if result.Recall != 1 {
		t.Fatalf("无期望用例 Recall 期望 1，实际 %v", result.Recall)
	}
}

func TestEvaluateCaseMinResults(t *testing.T) {
	testCase := Case{ID: "min", Query: "有没有结果", MinResults: 2}
	result := evaluateCase(testCase, []Hit{hit("documents/2/a.md", 1)}, 1)

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
