package eval

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Runner 逐条跑用例并汇总。
type Runner struct {
	Searcher Searcher
	TopK     int
	Logger   *slog.Logger
}

// Run 执行一轮评测。
//
// 一条用例检索失败不会中断整轮：外部依赖抖动时，中断只能告诉人"有一处坏了"，
// 而跑完能得到"多少条坏了、坏在哪一类场景" —— 后者才是修复需要的输入。
// 失败的用例会以 Error 落进结果并计入 Failed，指标不会被静静地跳过。
func (r Runner) Run(ctx context.Context, cases []Case) Report {
	results := make([]CaseResult, 0, len(cases))

	for _, item := range cases {
		topK := item.TopK
		if topK <= 0 {
			topK = r.TopK
		}

		startedAt := time.Now()
		hits, err := r.Searcher.Search(ctx, item.Query, topK)
		latencyMS := time.Since(startedAt).Milliseconds()

		if err != nil {
			results = append(results, CaseResult{
				ID:              item.ID,
				Scene:           item.Scene,
				Query:           item.Query,
				Note:            item.Note,
				ExpectedCount:   len(item.Expected) + len(item.ExpectedKeywords),
				Missing:         append([]string(nil), item.Expected...),
				MissingKeywords: append([]string(nil), item.ExpectedKeywords...),
				DurationMS:      latencyMS,
				Error:           fmt.Sprintf("search failed: %v", err),
			})
			r.log().Warn("eval: 用例检索失败",
				slog.String("case", item.ID),
				slog.String("query", item.Query),
				slog.String("error", err.Error()),
			)
			continue
		}

		results = append(results, evaluateCase(item, hits, latencyMS))
	}

	report := Report{
		GeneratedAt: time.Now(),
		TopK:        r.TopK,
		Cases:       results,
		Summary:     summarize(results),
	}
	report.Scenes = sceneStats(results)
	return report
}

func (r Runner) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
