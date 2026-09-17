package eval

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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

// 用例级 TopK 覆盖默认值，用来单独考察"放宽 K 能不能把漏召捞回来"。
func TestRunnerHonoursCaseLevelTopK(t *testing.T) {
	captured := make(chan int, 1)
	searcher := topKCapturingSearcher{captured: captured}

	report := Runner{Searcher: searcher, TopK: 5}.
		Run(context.Background(), []Case{{ID: "k", Query: "q", TopK: 20}})

	if got := <-captured; got != 20 {
		t.Fatalf("用例级 TopK 未生效，实际 %d", got)
	}
	if report.Summary.Cases != 1 {
		t.Fatalf("用例数错误: %+v", report.Summary)
	}
}

type topKCapturingSearcher struct {
	captured chan int
}

func (s topKCapturingSearcher) Search(_ context.Context, _ string, topK int) ([]Hit, error) {
	s.captured <- topK
	return nil, nil
}
