package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"eino-quickstart/internal/rag/grouping"
)

// Render 把报告写成人能读的文本。
//
// 顺序按"从概览到细节"排：先汇总指标，再按场景分组（定位是哪一类查询退化），
// 最后列失败用例的明细（定位到具体 query 和召回了什么）。评测报告的用途就是
// 让人在几十秒内决定"这次改动能不能合"，所以失败明细必须带召回了什么 ——
// 只说"missing"没法判断是切块问题、embedding 问题还是 ACL 过滤掉了。
func Render(w io.Writer, report Report) error {
	summary := report.Summary

	// 归并粒度打在表头，和 TopK 并列：同一份用例集在两种粒度下的 Hits 不是同一个
	// 单位，报告一旦离开当时的上下文，「召回 3 条」是 3 篇文档还是 3 个分块就没人
	// 知道了。缺省时补上默认值，让老报告与新报告长得一样。
	granularity := report.Granularity
	if granularity == "" {
		granularity = grouping.Default
	}
	fmt.Fprintf(w, "检索召回评测  %s  TopK=%d  归并粒度=%s\n",
		report.GeneratedAt.Format("2006-01-02 15:04:05"), report.TopK, granularity)
	fmt.Fprintln(w, strings.Repeat("─", 68))

	fmt.Fprintf(w, "用例 %d    通过 %d (%.1f%%)    失败 %d\n",
		summary.Cases, summary.Passed, summary.PassRate*100, summary.Failed)
	fmt.Fprintf(w, "Recall@K %.3f    MRR %.3f    Hit@1 %.3f\n",
		summary.RecallAtK, summary.MRR, summary.HitRateAt1)
	fmt.Fprintf(w, "ACL 泄漏 %d    空结果 %d\n", summary.ACLLeakCount, summary.EmptyResultCases)
	fmt.Fprintf(w, "延迟 P50 %dms    P95 %dms\n", summary.P50LatencyMS, summary.P95LatencyMS)

	if len(report.Scenes) > 0 {
		fmt.Fprintln(w, strings.Repeat("─", 68))
		writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(writer, "场景\t用例\t通过\t通过率\tRecall@K\tMRR")
		for _, scene := range report.Scenes {
			fmt.Fprintf(writer, "%s\t%d\t%d\t%.1f%%\t%.3f\t%.3f\n",
				scene.Scene, scene.Cases, scene.Passed, scene.PassRate*100, scene.RecallAtK, scene.MRR)
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}

	failed := make([]CaseResult, 0, summary.Failed)
	for _, result := range report.Cases {
		if !result.Passed {
			failed = append(failed, result)
		}
	}
	if len(failed) > 0 {
		fmt.Fprintln(w, strings.Repeat("─", 68))
		fmt.Fprintf(w, "失败用例（%d）\n", len(failed))
		for _, result := range failed {
			fmt.Fprintf(w, "\n  ✗ %s  [%s]  query=%q\n", result.ID, result.Scene, result.Query)
			if result.Error != "" {
				fmt.Fprintf(w, "      原因: %s\n", result.Error)
			}
			if len(result.MissingKeywords) > 0 {
				fmt.Fprintf(w, "      未命中的关键词: %s\n", strings.Join(result.MissingKeywords, ", "))
			}
			if result.Note != "" {
				fmt.Fprintf(w, "      说明: %s\n", result.Note)
			}
			// 条数的单位跟着粒度走，所以这里不说「篇」也不说「块」——
			// 说哪一边，另一边就是错的。
			fmt.Fprintf(w, "      召回 %d 条: %s\n",
				len(result.Results), joinOrDash(resultSources(result.Results)))
			// 命中块数明显多于结果条数，说明同一篇文档占了名次里的好几个位置 ——
			// 那是「条数看着不少、覆盖面却很窄」，只看条数看不见。
			if result.Chunks > len(result.Results) {
				fmt.Fprintf(w, "      （共命中 %d 块，按文档归并）\n", result.Chunks)
			}
		}
	}

	if len(report.Violations) > 0 {
		fmt.Fprintln(w, strings.Repeat("─", 68))
		fmt.Fprintln(w, "未达门槛：")
		for _, violation := range report.Violations {
			fmt.Fprintf(w, "  ! %s\n", violation)
		}
	} else {
		fmt.Fprintln(w, strings.Repeat("─", 68))
		fmt.Fprintln(w, "门槛检查：通过")
	}
	return nil
}

// WriteJSON 落盘完整报告。
//
// 这是给机器读的那一份：CI 归档它、两次运行 diff 它。参数或切块策略的隐性
// 质量回归就是靠"这一次的 json 和上一次差在哪几条用例"发现的。
func WriteJSON(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: 序列化报告: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: 写入报告 %s: %w", path, err)
	}
	return nil
}

func joinOrDash(values []string) string {
	if len(values) == 0 {
		return "（无）"
	}
	return strings.Join(values, ", ")
}

// resultSources 抽出结果明细里的 source，保持名次顺序。
func resultSources(results []ResultHit) []string {
	sources := make([]string, 0, len(results))
	for _, result := range results {
		sources = append(sources, result.Source)
	}
	return sources
}
