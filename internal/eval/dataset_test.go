package eval

import (
	"os"
	"path/filepath"
	"testing"
)

// 仓库里那份用例集必须始终可装载：它是评测的输入契约，写坏了整条流水线在
// 跑起来之前就该失败，而不是在 CI 上以"指标突然掉了"的面目出现。
func TestShippedDatasetLoads(t *testing.T) {
	cases, err := LoadCases(filepath.Join("datasets", "retrieval.jsonl"))
	if err != nil {
		t.Fatalf("装载随仓库分发的用例集失败: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("用例集为空")
	}

	withExpectation := 0
	for _, item := range cases {
		if len(item.Expected)+len(item.ExpectedKeywords) > 0 {
			withExpectation++
		}
		if item.Scene == "" {
			t.Errorf("用例 %s 缺少 scene，报告将无法按场景分组", item.ID)
		}
	}
	if withExpectation == 0 {
		t.Fatal("没有任何带期望的用例，评测跑不出召回指标")
	}
}

func TestLoadCasesRejectsDuplicateID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.jsonl")
	content := `{"id":"same","query":"a"}
{"id":"same","query":"b"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadCases(path); err == nil {
		t.Fatal("重复 id 必须报错：否则用例会被计入两次，指标悄悄失真")
	}
}

func TestLoadCasesRejectsAbsoluteSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abs.jsonl")
	content := `{"id":"abs","query":"a","expected_sources":["/abs/path.md"]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadCases(path); err == nil {
		t.Fatal("绝对路径的 source 必须报错：它永远匹配不上，且是静默地匹配不上")
	}
}

func TestLoadCasesSkipsCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.jsonl")
	content := `// 注释行
{"id":"one","query":"a","expected_source_keywords":["alpha"]}

// 又是注释
{"id":"two","query":"b"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cases, err := LoadCases(path)
	if err != nil {
		t.Fatalf("装载失败: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("期望 2 条用例，实际 %d", len(cases))
	}
}

func TestLoadCasesRejectsEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":"x","query":"   "}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadCases(path); err == nil {
		t.Fatal("空 query 必须报错")
	}
}

func TestLoadThresholdsRejectsEmptyGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	// 空配置不能当成"全部通过"：门禁最危险的失效方式是它以为自己在把关。
	if err := os.WriteFile(path, []byte("maxACLLeakCount: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadThresholds(path); err == nil {
		t.Fatal("未配置任何质量项时应报错")
	}
}

func TestLoadShippedThresholds(t *testing.T) {
	limits, err := LoadThresholds("thresholds.yaml")
	if err != nil {
		t.Fatalf("装载随仓库分发的门禁配置失败: %v", err)
	}
	if limits.MinPassRate <= 0 || limits.MinRecallAtK <= 0 {
		t.Fatalf("门禁配置缺少质量项: %+v", limits)
	}
	if limits.MaxACLLeakCount != 0 {
		t.Fatalf("ACL 泄漏上限必须是 0，实际 %d", limits.MaxACLLeakCount)
	}
}
