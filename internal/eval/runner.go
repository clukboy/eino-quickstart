package eval

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"eino-quickstart/internal/rag/grouping"
)

// Runner 逐条跑用例并汇总。
type Runner struct {
	Searcher Searcher

	// TopK 是每条用例的默认结果条数，单位由 Granularity 决定：document 粒度下
	// 是篇数（用例自己带 top_k 时以用例为准）。向检索侧要多少分块由它折算出来
	// （见 searchFilled），不是直接传下去的数字。
	TopK int

	// Granularity 是召回结果的归并粒度。零值取 grouping.Default，
	// 由组合根按知识库类型解析后传进来（见 grouping.Policy）。
	Granularity grouping.Granularity

	Logger *slog.Logger
}

// Run 执行一轮评测。
//
// 一条用例检索失败不会中断整轮：外部依赖抖动时，中断只能告诉人"有一处坏了"，
// 而跑完能得到"多少条坏了、坏在哪一类场景" —— 后者才是修复需要的输入。
// 失败的用例会以 Error 落进结果并计入 Failed，指标不会被静静地跳过。
func (r Runner) Run(ctx context.Context, cases []Case) Report {
	granularity := r.Granularity
	if granularity == "" {
		granularity = grouping.Default
	}

	results := make([]CaseResult, 0, len(cases))

	for _, item := range cases {
		topK := item.TopK
		if topK <= 0 {
			topK = r.TopK
		}

		startedAt := time.Now()
		hits, err := searchFilled(ctx, r.Searcher, item.Query, topK, granularity)
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

		results = append(results, evaluateCase(item, hits, latencyMS, granularity))
	}

	report := Report{
		GeneratedAt: time.Now(),
		TopK:        r.TopK,
		Granularity: granularity,
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

// searchFilled 按归并粒度取满结果。
//
// 评测的口径必须与线上同源：线上 document 粒度下 top_k=20 是「20 篇文档」，而
// 检索侧只认识分块 —— 折算规则是同一份（grouping.FetchPlan）。不跟着走的话，
// 评测只取 K 块、线上取满 K 篇，线上永远比评测宽松，报告就失去了预警能力：
// 用例集里显示「召回 4 篇」的问题，在真实调用里已经是被修好的样子。
//
// 返回最后一次检索的**分块级**命中：原始名次要按分块算（见 evaluateCase 的
// 判定口径），归并只影响明细与条数。
func searchFilled(
	ctx context.Context,
	searcher Searcher,
	query string,
	want int,
	granularity grouping.Granularity,
) ([]Hit, error) {
	plan := grouping.NewFetchPlan(granularity, want)
	for {
		hits, err := searcher.Search(ctx, query, plan.Budget())
		if err != nil {
			return nil, err
		}
		if plan.Settled(len(groupResults(hits, granularity)), len(hits)) {
			return hits, nil
		}
		next, ok := plan.Widen()
		if !ok {
			return hits, nil
		}
		plan = next
	}
}
