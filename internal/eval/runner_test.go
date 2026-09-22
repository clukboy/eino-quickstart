package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"eino-quickstart/internal/rag/grouping"
)

type fakeSearcher struct {
	byQuery map[string][]Hit
	err     error
}

func (f fakeSearcher) Search(_ context.Context, query string, _ int) ([]Hit, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byQuery[query], nil
}

func TestRunnerAggregatesAndRenders(t *testing.T) {
	cases := []Case{
		{
			ID:               "hit",
			Scene:            "型号精确",
			Query:            "H105P",
			ExpectedKeywords: []string{"h11-二段力"},
		},
		{
			ID:               "miss",
			Scene:            "公司信息",
			Query:            "TUTTI 成立时间",
			ExpectedKeywords: []string{"阶段一-初步接触"},
			Note:             "这条带说明，失败明细里应该显示出来",
		},
	}
	searcher := fakeSearcher{byQuery: map[string][]Hit{
		"H105P":      {hit("documents/2/h11-二段力.md", 0.9)},
		"TUTTI 成立时间": {hit("documents/2/其他.md", 0.5)},
	}}

	report := Runner{Searcher: searcher, TopK: 5}.Run(context.Background(), cases)

	if report.Summary.Cases != 2 || report.Summary.Passed != 1 || report.Summary.Failed != 1 {
		t.Fatalf("汇总错误: %+v", report.Summary)
	}
	if report.Summary.RecallAtK != 0.5 {
		t.Fatalf("Recall@K 期望 0.5，实际 %v", report.Summary.RecallAtK)
	}
	if report.TopK != 5 {
		t.Fatalf("TopK 未透传，实际 %d", report.TopK)
	}
	if len(report.Scenes) != 2 {
		t.Fatalf("场景数期望 2，实际 %d", len(report.Scenes))
	}

	var out bytes.Buffer
	if err := Render(&out, report); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Recall@K", "型号精确", "公司信息", "✗ miss", "这条带说明"} {
		if !strings.Contains(text, want) {
			t.Fatalf("报告里缺少 %q:\n%s", want, text)
		}
	}
}

// 一条用例的检索失败不能中断整轮，也不能被静静跳过：它要以 Error 落进结果
// 并计入 Failed，否则"少跑了几条"会伪装成"指标还可以"。
func TestRunnerRecordsSearchFailureWithoutStopping(t *testing.T) {
	cases := []Case{{ID: "boom", Query: "x", ExpectedKeywords: []string{"a"}}}

	report := Runner{Searcher: fakeSearcher{err: errors.New("milvus down")}, TopK: 5}.
		Run(context.Background(), cases)

	if report.Summary.Failed != 1 {
		t.Fatalf("检索失败应计入 Failed，实际 %+v", report.Summary)
	}
	if got := report.Cases[0].Error; !strings.Contains(got, "search failed") || !strings.Contains(got, "milvus down") {
		t.Fatalf("失败用例应带上游错误原文，实际 %q", got)
	}
}

func TestRenderShowsViolations(t *testing.T) {
	report := Report{
		TopK:       5,
		Cases:      []CaseResult{{ID: "x", Passed: true}},
		Summary:    Summary{Cases: 1, Passed: 1, PassRate: 1},
		Violations: []string{"Recall@K 0.100 低于门槛 0.800"},
	}

	var out bytes.Buffer
	if err := Render(&out, report); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out.String(), "未达门槛") {
		t.Fatalf("报告未提示未达门槛:\n%s", out.String())
	}
}

// 用例级 TopK 覆盖默认值，且它的单位是**条数**：向检索侧要多少分块要按粒度折算。
//
// 这条同时守两件事：用例能单独放宽 K（用来考察"放宽 K 能不能把漏召捞回来"），
// 以及「放宽到 20 条」在 document 粒度下不等于「只要 20 块」—— 后者正是
// 「请求 20 篇、返回 4 篇」的成因，评测与线上会因此各说各话。
func TestRunnerHonoursCaseLevelTopKInResultUnits(t *testing.T) {
	captured := make(chan int, 1)
	searcher := topKCapturingSearcher{captured: captured}

	report := Runner{Searcher: searcher, TopK: 5}.
		Run(context.Background(), []Case{{ID: "k", Query: "q", TopK: 20}})

	if got := <-captured; got != 20*initialFetchFactorForTest {
		t.Fatalf("document 粒度下要 20 篇应折算成 %d 块预算，实际 %d", 20*initialFetchFactorForTest, got)
	}
	if report.Summary.Cases != 1 {
		t.Fatalf("用例数错误: %+v", report.Summary)
	}
}

// chunk 粒度下不折算：一条结果就是一个分块。
func TestRunnerDoesNotWidenChunkGranularityBudget(t *testing.T) {
	captured := make(chan int, 1)
	searcher := topKCapturingSearcher{captured: captured}

	Runner{Searcher: searcher, TopK: 5, Granularity: grouping.Chunk}.
		Run(context.Background(), []Case{{ID: "k", Query: "q", TopK: 20}})

	if got := <-captured; got != 20 {
		t.Fatalf("chunk 粒度下应原样要 20 块，实际 %d", got)
	}
}

// document 粒度下取不满就加码 —— 评测口径必须与线上同源。
//
// 不跟着走的话，评测只取 K 块、线上取满 K 篇，线上永远比评测宽松：用例集里显示
// 「只召回 4 篇」的问题，在真实调用里已经是被修好的样子，报告就失去了预警能力。
func TestRunnerFillsDocumentsAcrossRounds(t *testing.T) {
	searcher := &pagedSearcher{pool: documentPool(20, 5)} // 100 块 / 20 篇

	report := Runner{Searcher: searcher, TopK: 20, Granularity: grouping.Document}.
		Run(context.Background(), []Case{{ID: "c", Query: "q"}})

	if got := len(report.Cases[0].Results); got != 20 {
		t.Fatalf("应取满 20 篇，实际 %d 篇（预算序列 %v）", got, searcher.budgets)
	}
	if len(searcher.budgets) != 2 || searcher.budgets[0] != 80 || searcher.budgets[1] != 160 {
		t.Fatalf("应先按 80 块取、再翻到 160 块，实际预算序列 %v", searcher.budgets)
	}
	// 多取一轮不改变指标口径：Chunks 是原始分块命中数，Hits 是归并后的条数。
	if report.Cases[0].Chunks != 100 {
		t.Fatalf("原始命中块数应为 100，实际 %d", report.Cases[0].Chunks)
	}
	if report.Cases[0].Hits != 20 {
		t.Fatalf("归并后应为 20 篇，实际 %d", report.Cases[0].Hits)
	}
}

// 库里就这么多内容时，评测也只取一轮 —— 加码是为了取满，不是为了反复确认。
func TestRunnerStopsWhenRetrieverIsExhausted(t *testing.T) {
	searcher := &pagedSearcher{pool: documentPool(4, 5)} // 20 块 / 4 篇

	report := Runner{Searcher: searcher, TopK: 20, Granularity: grouping.Document}.
		Run(context.Background(), []Case{{ID: "c", Query: "q"}})

	if len(searcher.budgets) != 1 {
		t.Fatalf("见底之后不该再加码，实际预算序列 %v", searcher.budgets)
	}
	if got := len(report.Cases[0].Results); got != 4 {
		t.Fatalf("库里只有 4 篇就该返回 4 篇，实际 %d", got)
	}
}

// initialFetchFactorForTest 与 grouping 包内的放大倍数保持一致。
//
// 不直接引用那个常量（未导出），但这个数字是对外可见的行为契约的一部分：它决定
// 「要 20 篇」会向检索侧要多少块，改了它评测与线上的延迟都会变。
const initialFetchFactorForTest = 4

// pagedSearcher 是按预算截断的检索桩：池子有限，给不满预算就是「库里就这么多」。
type pagedSearcher struct {
	pool    []Hit
	budgets []int
}

func (s *pagedSearcher) Search(_ context.Context, _ string, topK int) ([]Hit, error) {
	s.budgets = append(s.budgets, topK)
	pool := append([]Hit(nil), s.pool...)
	if topK > 0 && len(pool) > topK {
		pool = pool[:topK]
	}
	return pool, nil
}

// documentPool 造一个「每篇文档切成 chunksPerDoc 块」的候选池。
func documentPool(documents, chunksPerDoc int) []Hit {
	hits := make([]Hit, 0, documents*chunksPerDoc)
	for doc := 1; doc <= documents; doc++ {
		for chunk := 0; chunk < chunksPerDoc; chunk++ {
			hits = append(hits, Hit{
				ChunkID: int64(doc*100 + chunk),
				Source:  fmt.Sprintf("documents/9/p%03d.md", doc),
				Content: fmt.Sprintf("块 %d", chunk),
				Score:   float64(chunksPerDoc-chunk) / 10,
			})
		}
	}
	return hits
}

type topKCapturingSearcher struct {
	captured chan int
}

func (s topKCapturingSearcher) Search(_ context.Context, _ string, topK int) ([]Hit, error) {
	s.captured <- topK
	return nil, nil
}

// 报告必须带上归并粒度：同一份用例集在两种粒度下「召回 3 条」不是一个单位，
// 报告一旦离开当时的上下文，就没人知道那 3 是 3 篇文档还是 3 个分块。
func TestReportRecordsGranularity(t *testing.T) {
	report := Runner{
		Searcher:    fakeSearcher{byQuery: map[string][]Hit{"q": {hit("documents/2/a.md", 1)}}},
		TopK:        5,
		Granularity: grouping.Chunk,
	}.Run(context.Background(), []Case{{ID: "c", Query: "q"}})

	if report.Granularity != grouping.Chunk {
		t.Fatalf("报告应记下粒度，实际 %q", report.Granularity)
	}
	var out bytes.Buffer
	if err := Render(&out, report); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(out.String(), "归并粒度=chunk") {
		t.Fatalf("报告表头应写明粒度:\n%s", out.String())
	}
}

// 零值粒度取默认（按文档归并）。「零值 = 不做这件事」是最容易被误读的一种默认,
// 而这里的零值含义是「按最保守的方式归并」，必须有一条测试钉住。
func TestRunnerZeroGranularityFallsBackToDefault(t *testing.T) {
	report := Runner{
		Searcher: fakeSearcher{byQuery: map[string][]Hit{"q": {
			hit("documents/2/a.md", 0.9),
			hit("documents/2/a.md", 0.8),
		}}},
		TopK: 5,
	}.Run(context.Background(), []Case{{ID: "c", Query: "q"}})

	if report.Granularity != grouping.Default {
		t.Fatalf("零值应取默认粒度 %q，实际 %q", grouping.Default, report.Granularity)
	}
	if got := len(report.Cases[0].Results); got != 1 {
		t.Fatalf("默认粒度下同一篇文档应归并成 1 条，实际 %d", got)
	}
}
