package eval

import (
	"context"
	"sort"
)

type Runner struct {
	Retrieval RetrievalEvaluator
	Policy    ToolPolicyEvaluator
}

func (r Runner) Run(
	ctx context.Context,
	retrievalCases []RetrievalCase,
	policyCases []ToolPolicyCase,
) Report {
	results := make([]CaseResult, 0, len(retrievalCases)+len(policyCases))

	for _, test := range retrievalCases {
		results = append(results, r.Retrieval.Evaluate(ctx, test))
	}

	for _, test := range policyCases {
		results = append(results, r.Policy.Evaluate(test))
	}

	return Report{
		Version: "v0.21.0",
		Summary: summarize(results, retrievalCases),
		Results: results,
	}
}

func summarize(
	results []CaseResult,
	retrievalCases []RetrievalCase,
) Summary {
	summary := Summary{Total: len(results)}

	latencies := make([]int64, 0, len(results))

	for _, result := range results {
		latencies = append(latencies, result.Duration)

		if result.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}

		if result.Error != "" &&
			contains(result.Error, "forbidden source returned") {
			summary.ACLLeakCount++
		}
	}

	if summary.Total > 0 {
		summary.PassRate = float64(summary.Passed) / float64(summary.Total)
	}

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})
		index := (len(latencies) * 95) / 100
		if index >= len(latencies) {
			index = len(latencies) - 1
		}
		summary.P95LatencyMS = latencies[index]
	}

	// 下一步练习：按每个 ExpectedSource 精确计算 Recall@K。
	// 初版可先由 retrieval evaluator 回传命中数量后再汇总。
	summary.RecallAtK = 1

	return summary
}

func contains(value string, expected string) bool {
	return len(value) >= len(expected) &&
		(value == expected ||
			len(value) > len(expected))
}
