package eval

import (
	"context"
	"path/filepath"
	"testing"

	"eino-quickstart/internal/rag/grouping"
)

// P21 的金标必须有真实的非空正向与 forbidden 对照；空库只能测出“什么都没返回”，
// 不能证明授权隔离真的生效。
func TestPrivacyDatasetHasNonEmptyIsolationCases(t *testing.T) {
	cases, err := LoadCases(filepath.Join("datasets", "privacy.jsonl"))
	if err != nil {
		t.Fatalf("加载隐私隔离金标失败: %v", err)
	}
	if len(cases) < 2 {
		t.Fatalf("隐私金标至少需要两条主体对照，实际 %d", len(cases))
	}

	for _, item := range cases {
		if len(item.Expected) == 0 && len(item.ExpectedKeywords) == 0 {
			t.Errorf("用例 %q 缺少非空正向期望", item.ID)
		}
		if len(item.Forbidden) == 0 {
			t.Errorf("用例 %q 缺少 forbidden 期望", item.ID)
		}
		if item.MinResults == 0 {
			t.Errorf("用例 %q 必须要求至少一个正向结果", item.ID)
		}
	}
}

func TestPrivacyRunnerFailsOnCrossSubjectLeak(t *testing.T) {
	cases, err := LoadCases(filepath.Join("datasets", "privacy.jsonl"))
	if err != nil {
		t.Fatalf("加载隐私隔离金标失败: %v", err)
	}

	searcher := privacySearcher{byQuery: map[string][]Hit{
		"subject A unique hinge specification": {
			{ChunkID: 1, Source: "private/subject-a/hinge.md", Score: 1},
		},
		"subject B unique hinge specification": {
			{ChunkID: 2, Source: "private/subject-b/hinge.md", Score: 1},
			// 模拟服务端过滤失效：B 的查询混入了 A 的私有文档。
			{ChunkID: 3, Source: "private/subject-a/hinge.md", Score: 0.9},
		},
	}}

	report := Runner{Searcher: searcher, TopK: 5, Granularity: grouping.Document}.
		Run(context.Background(), cases)

	if report.Cases[0].Passed != true {
		t.Fatalf("主体 A 的正向隔离用例应通过: %+v", report.Cases[0])
	}
	if report.Cases[1].Passed {
		t.Fatalf("主体 B 混入 A 私有文档时必须失败: %+v", report.Cases[1])
	}
	if len(report.Cases[1].Leaked) != 1 || report.Cases[1].Leaked[0] != "private/subject-a/hinge.md" {
		t.Fatalf("应记录具体泄漏 source: %+v", report.Cases[1].Leaked)
	}
	if report.Summary.ACLLeakCount != 1 {
		t.Fatalf("ACL 泄漏计数应为 1，实际 %d", report.Summary.ACLLeakCount)
	}
}

type privacySearcher struct {
	byQuery map[string][]Hit
}

func (s privacySearcher) Search(_ context.Context, query string, _ int) ([]Hit, error) {
	return append([]Hit(nil), s.byQuery[query]...), nil
}
