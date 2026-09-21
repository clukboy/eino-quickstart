package main

import (
	"bytes"
	"strings"
	"testing"

	"eino-quickstart/internal/eval"
	"eino-quickstart/internal/rag/grouping"
)

// 明细必须把召回到的**每一条**列出来，而不只是给个计数。
// 只写「结果 3 条」的报告回答不了「差多少」——那正是跑 -v 的理由。
func TestRenderDetailListsEveryResult(t *testing.T) {
	report := eval.Report{
		TopK:        5,
		Granularity: grouping.Document,
		Cases: []eval.CaseResult{{
			ID:           "model-h105p",
			Query:        "H105P",
			Recall:       1,
			FirstHitRank: 1,
			Chunks:       4,
			Results: []eval.ResultHit{
				{Rank: 1, Source: "documents/2/H105P.md", Score: 0.87, Chunks: 3, HeadingPath: "H105P / 规格参数"},
				{Rank: 2, Source: "documents/2/H105G.md", Score: 0.51, Chunks: 1, HeadingPath: "H105G / 规格参数"},
			},
		}},
	}

	text := renderDetailToString(t, report)

	for _, want := range []string{
		"documents/2/H105P.md",
		"documents/2/H105G.md",
		"0.870",
		"H105P / 规格参数",
		"按文档",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("明细里缺少 %q:\n%s", want, text)
		}
	}
	// 一条结果一行，行数要等于结果条数 —— 多一行少一行都说明在按别的粒度列。
	if got := strings.Count(text, "documents/2/"); got != 2 {
		t.Fatalf("期望两条结果行，实际 %d:\n%s", got, text)
	}
}

// 粒度换成分块时，表头也要跟着换：同一句「结果 3 条」在两种粒度下不是同一个
// 单位，明细的抬头必须说清这一份是按什么列的。
func TestRenderDetailNamesChunkGranularity(t *testing.T) {
	report := eval.Report{
		Granularity: grouping.Chunk,
		Cases: []eval.CaseResult{{
			ID:      "long-doc",
			Query:   "长文检索",
			Results: []eval.ResultHit{{Rank: 1, Source: "documents/2/长文.md", Score: 0.9, Chunks: 1}},
		}},
	}

	text := renderDetailToString(t, report)

	if !strings.Contains(text, "按分块") {
		t.Fatalf("chunk 粒度下抬头应说明按分块列出:\n%s", text)
	}
}

// 零值粒度取默认（按文档），与 eval.Runner 的口径一致 —— 两处显示不同的粒度
// 会让人对着同一份报告得出两个结论。
func TestRenderDetailZeroGranularityUsesDefault(t *testing.T) {
	report := eval.Report{Cases: []eval.CaseResult{{ID: "x", Query: "q"}}}

	text := renderDetailToString(t, report)

	if !strings.Contains(text, grouping.Default.Label()) {
		t.Fatalf("零值应显示默认粒度 %s:\n%s", grouping.Default.Label(), text)
	}
}

// 没有命中的用例要显式写出来，不能只留一个空的缩进块 —— 那看起来像渲染坏了,
// 而「一条都没召回」本身就是最需要看到的信息。
func TestRenderDetailMarksEmptyResults(t *testing.T) {
	report := eval.Report{Cases: []eval.CaseResult{{ID: "none", Query: "Kubernetes Ingress"}}}

	text := renderDetailToString(t, report)

	if !strings.Contains(text, "（无结果）") {
		t.Fatalf("空结果应显式标出:\n%s", text)
	}
}

// 截断按字符而不是字节：中文占 3 个字节，按字节切会把标题路径切成乱码，
// 而乱码比不截断更难读。
func TestTruncateRunesIsRuneSafe(t *testing.T) {
	if got := truncateRunes(strings.Repeat("铰", 40), 10); []rune(got)[9] != '铰' {
		t.Fatalf("截断后应仍是完整字符: %q", got)
	}
	if got := truncateRunes(strings.Repeat("铰", 40), 10); len([]rune(got)) != 11 {
		t.Fatalf("10 个字符加省略号应共 11 个字符，实际 %d", len([]rune(got)))
	}
	if got := truncateRunes("文档/规格参数", 36); got != "文档/规格参数" {
		t.Fatalf("未超长时不该改动: %q", got)
	}
}

func renderDetailToString(t *testing.T, report eval.Report) string {
	t.Helper()
	var out bytes.Buffer
	if err := renderDetail(&out, report); err != nil {
		t.Fatalf("渲染明细失败: %v", err)
	}
	return out.String()
}
